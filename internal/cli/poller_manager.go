package cli

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/pglock"
)

// pollerLeaderLockKey identifies the Postgres advisory lock guarding
// poller leadership across replicas (ha-multi-replica HA-01..HA-07). Its
// value is deliberately in a distinct block from internal/dbtest's
// test-only keys (727100001-727100003) - see internal/pglock's own doc
// comment for the full namespace rationale - so a production poller lock
// can never collide with (and deadlock against) a test-only one sharing
// the same database.
const pollerLeaderLockKey int64 = 727200001

// defaultLeaderRetryInterval controls how often a non-leader replica
// retries acquiring the poller leadership lock.
const defaultLeaderRetryInterval = 10 * time.Second

// defaultLeaderHeartbeatInterval controls how often the current leader
// checks that its lock session is still alive. It doubles as the renewal
// interval spec.md's HA-03/HA-04 refer to: pg_advisory_lock is
// session-scoped, so there is nothing to actively "renew" - Healthy()
// simply confirms the session (and therefore the lock) hasn't died.
const defaultLeaderHeartbeatInterval = 10 * time.Second

// PollerManager owns the single running Poller's lifecycle, letting it be
// (re)started after boot - when an admin connects Datadog or rotates its
// key through the API - without a process restart (PLD-01, PLD-05).
// Restart always tears down whatever poller is currently running (if any)
// before building and starting a fresh one from the integration row
// currently stored, so a rotated key is picked up the same way a first
// connect is (PLD-05) and concurrent calls never leave two pollers running
// against the same services (Edge Cases).
//
// Across multiple replicas sharing one Postgres database, RunLeaderLoop
// additionally gates Restart behind a Postgres advisory lock (AD-013) so
// at most one replica ever runs a poller at a time (HA-01..HA-07).
type PollerManager struct {
	mu        sync.Mutex
	parentCtx context.Context
	pool      *db.Pool
	cfg       config.Config
	logger    *zap.Logger
	dsn       string
	cancel    context.CancelFunc
	done      chan struct{}

	// leaderRetryInterval/leaderHeartbeatInterval default to
	// defaultLeaderRetryInterval/defaultLeaderHeartbeatInterval in
	// NewPollerManager; tests override them directly (same package) to
	// avoid waiting a full 10s per assertion.
	leaderRetryInterval     time.Duration
	leaderHeartbeatInterval time.Duration
}

// NewPollerManager builds a PollerManager. parentCtx is the server's own
// lifetime context - canceling it (e.g. on SIGINT/SIGTERM) is what lets a
// poller started via Restart still exit on shutdown, the same as the
// boot-time poller does today. dsn is the raw Postgres connection string
// (not pool) - RunLeaderLoop's advisory lock needs a dedicated,
// non-pooled connection per internal/pglock's own contract.
func NewPollerManager(parentCtx context.Context, pool *db.Pool, cfg config.Config, logger *zap.Logger, dsn string) *PollerManager {
	return &PollerManager{
		parentCtx:               parentCtx,
		pool:                    pool,
		cfg:                     cfg,
		logger:                  logger,
		dsn:                     dsn,
		leaderRetryInterval:     defaultLeaderRetryInterval,
		leaderHeartbeatInterval: defaultLeaderHeartbeatInterval,
	}
}

// RunLeaderLoop blocks until ctx is done, alternating between two states:
// attempting to acquire the poller leadership lock (retrying every
// leaderRetryInterval while it's held elsewhere - HA-01/HA-02), and, once
// acquired, running the poller (via Restart) while heartbeating the lock's
// session every leaderHeartbeatInterval. The moment a heartbeat finds the
// session no longer alive (lock lost - crash, GC pause, network partition:
// HA-04), it stops the poller immediately without waiting for the
// in-flight cycle to finish (HA-05) and returns to the acquire loop.
//
// With a single replica (HA-07), TryAcquire always succeeds immediately
// and no contention ever occurs - behavior is identical to the unconditional
// boot-time Restart this replaces, just gated by one extra (near-instant)
// lock acquisition. No new environment variable or configuration is
// required (HA-06) - this activates automatically whenever more than one
// replica targets the same database.
//
// Callers should run this in its own goroutine; it does not return until
// ctx is canceled.
func (m *PollerManager) RunLeaderLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		handle, ok, err := pglock.TryAcquire(ctx, m.dsn, pollerLeaderLockKey)
		if err != nil {
			m.logger.Warn("poller leader election: failed to attempt lock acquisition, retrying", zap.Error(err))
			if !sleepOrDone(ctx, m.leaderRetryInterval) {
				return
			}
			continue
		}
		if !ok {
			// Another replica currently holds leadership.
			if !sleepOrDone(ctx, m.leaderRetryInterval) {
				return
			}
			continue
		}

		m.logger.Info("poller leader election: acquired leadership")
		if started, err := m.Restart(ctx); err != nil {
			m.logger.Error("poller leader election: failed to start poller after acquiring leadership", zap.Error(err))
		} else if !started {
			m.logger.Warn("poller leader election: acquired leadership but no datadog integration connected yet, poller not started")
		}

		m.heartbeatUntilLost(ctx, handle)

		// Lock lost or shutting down: abort whatever is in-flight (HA-05)
		// before releasing, so no partial poll cycle keeps running under a
		// lock we no longer safely hold.
		m.Stop()
		_ = handle.Release(context.Background())

		if ctx.Err() != nil {
			return
		}
		m.logger.Warn("poller leader election: lost leadership lock, stopped poller, retrying acquisition")
	}
}

// heartbeatUntilLost blocks until ctx is done or handle's session is found
// unhealthy, checking on leaderHeartbeatInterval.
func (m *PollerManager) heartbeatUntilLost(ctx context.Context, handle *pglock.Handle) {
	ticker := time.NewTicker(m.leaderHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !handle.Healthy(ctx) {
				return
			}
		}
	}
}

// sleepOrDone waits for either d to elapse (returns true) or ctx to be
// done (returns false), whichever comes first.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Restart stops whichever poller is currently running (waiting for its Run
// goroutine to actually return first, so two pollers never overlap - Edge
// Cases), then builds a new one from the Datadog integration currently
// stored and starts it. started reports whether a poller ended up running;
// it is false with a nil error if no integration is stored (PLD-02/PLD-03's
// "nothing to start" case), mirroring newPollerFromStoredIntegration's own
// contract - callers that don't need to distinguish the two (e.g.
// IntegrationsHandler, which just connected/rotated a real integration)
// can ignore it.
func (m *PollerManager) Restart(ctx context.Context) (started bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopLocked()

	p, started, err := newPollerFromStoredIntegration(ctx, m.pool, m.cfg, m.logger)
	if err != nil {
		return false, err
	}
	if !started {
		return false, nil
	}

	runCtx, cancel := context.WithCancel(m.parentCtx)
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done

	go func() {
		defer close(done)
		p.Run(runCtx)
	}()

	return true, nil
}

// Stop stops whichever poller is currently running and waits for it to
// exit, so callers (serve's shutdown path) can rely on no poller goroutine
// outliving the call - the same guarantee serve already made for the
// boot-time poller (PLD-04).
func (m *PollerManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

// stopLocked cancels and waits for the currently running poller, if any.
// Callers must hold m.mu.
func (m *PollerManager) stopLocked() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	<-m.done
	m.cancel = nil
	m.done = nil
}
