package cli

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
// the same database. Exported as db.PollerLeaderLockKey so
// PollerLeadershipRepository (poller-status-real-state, POLLST-01) can
// query pg_locks for the same key without an import cycle; kept as a local
// alias here so this file's existing references don't all need rewriting.
const pollerLeaderLockKey = db.PollerLeaderLockKey

// replicaApplicationName identifies this replica in
// pg_stat_activity.application_name (poller-status-real-state POLLST-01/03),
// sourced from HOSTNAME - which Kubernetes sets to the pod name - falling
// back to a fixed placeholder outside Kubernetes so the admin UI never
// renders a blank replica name.
func replicaApplicationName() string {
	if h := os.Getenv("HOSTNAME"); h != "" {
		return h
	}
	return "unknown"
}

// dsnWithApplicationName appends application_name=name to dsn, so the
// connection this DSN opens is identifiable in
// pg_stat_activity.application_name. Falls back to raw query-string
// concatenation when dsn doesn't parse as a URL (e.g. a keyword/value DSN) -
// Postgres accepts application_name as either a URL query parameter or a
// keyword/value pair, so appending it as "key=value" text works either way.
func dsnWithApplicationName(dsn, name string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		q := u.Query()
		q.Set("application_name", name)
		u.RawQuery = q.Encode()
		return u.String()
	}
	sep := " "
	if strings.HasSuffix(strings.TrimSpace(dsn), "=") || dsn == "" {
		sep = ""
	}
	return dsn + sep + "application_name='" + name + "'"
}

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

	// manualCancel/manualDone track the running poller.ManualScheduler - a
	// second, independent start/stop pair alongside cancel/done for the
	// Datadog poller (manual-polling-monitoring design.md Approach
	// Exploration). Deliberately not folded into cancel/done or into
	// Restart's own lifecycle: Restart is also called directly by
	// IntegrationsHandler.ConnectDatadog on every Datadog credential
	// connect/rotate, and the manual scheduler has no credentials to
	// rotate - it must not be torn down/rebuilt (losing its in-memory
	// per-service failure-streak state) every time an admin merely
	// reconnects Datadog.
	manualCancel context.CancelFunc
	manualDone   chan struct{}

	// leaderRetryInterval/leaderHeartbeatInterval default to
	// defaultLeaderRetryInterval/defaultLeaderHeartbeatInterval in
	// NewPollerManager; tests override them directly (same package) to
	// avoid waiting a full 10s per assertion.
	leaderRetryInterval     time.Duration
	leaderHeartbeatInterval time.Duration

	// leading is true only between RunLeaderLoop successfully acquiring the
	// poller leadership lock and losing it. Restart consults this so a call
	// arriving through IntegrationsHandler.ConnectDatadog on a non-leader
	// replica (the admin API is served by every replica, not just the
	// leader) becomes a no-op instead of starting a second poller alongside
	// the real leader's - the bug this field exists to close. A
	// single-replica deployment sets this true immediately at boot
	// (TryAcquire always succeeds uncontested), so behavior there is
	// unchanged (HA-07).
	leading atomic.Bool
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

		handle, ok, err := pglock.TryAcquire(ctx, dsnWithApplicationName(m.dsn, replicaApplicationName()), pollerLeaderLockKey)
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
		m.leading.Store(true)

		// Started unconditionally, in parallel to (not inside) Restart:
		// the manual scheduler has nothing to do with whether a Datadog
		// integration is connected (manual-polling-monitoring design.md -
		// polling-manual services must work in an install with zero
		// Datadog integration configured).
		m.startManualScheduler()

		if started, err := m.Restart(ctx); err != nil {
			m.logger.Error("poller leader election: failed to start poller after acquiring leadership", zap.Error(err))
		} else if !started {
			m.logger.Warn("poller leader election: acquired leadership but no datadog integration connected yet, poller not started")
		}

		m.heartbeatUntilLost(ctx, handle)

		// Lock lost or shutting down: abort whatever is in-flight (HA-05)
		// before releasing, so no partial poll cycle keeps running under a
		// lock we no longer safely hold. leading flips false and the
		// running poller is stopped under the same m.mu critical section
		// as Restart's own leading check (not a plain m.Stop() call, which
		// would take m.mu separately) - otherwise a Restart already past
		// its leading.Load() check but not yet holding m.mu could still
		// start a new poller after this replica has given up leadership,
		// racing this exact stop.
		m.mu.Lock()
		m.leading.Store(false)
		m.stopLocked()
		m.stopManualLocked()
		m.mu.Unlock()
		_ = handle.Release(context.Background())

		if ctx.Err() != nil {
			return
		}
		m.logger.Warn("poller leader election: lost leadership lock, stopped poller, retrying acquisition")
	}
}

// heartbeatUntilLost blocks until ctx is done or handle's session is found
// unhealthy, checking on leaderHeartbeatInterval. Each tick it also retries
// starting the poller if this replica is leading but has none running (AD-034):
// RunLeaderLoop's own Restart call at acquisition time is a one-shot attempt,
// so a Datadog connect/rotate that lands - via IntegrationsHandler, the admin
// API is served by every replica - on a replica that isn't currently leading
// makes Restart there a documented no-op (Restart's own leading check) that
// never reaches the real leader. Without this retry, the leader never learns
// a credential now exists until it loses and re-acquires leadership (or the
// process restarts), leaving every service stuck at not_configured
// indefinitely even though the integration shows connected. warnedNotStarted
// keeps the "not started" log to a single line per outage instead of
// spamming it every heartbeat while genuinely waiting on a first connect.
func (m *PollerManager) heartbeatUntilLost(ctx context.Context, handle *pglock.Handle) {
	ticker := time.NewTicker(m.leaderHeartbeatInterval)
	defer ticker.Stop()

	warnedNotStarted := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Bound each probe to leaderHeartbeatInterval instead of
			// inheriting ctx's process-lifetime deadline: on a network
			// partition or a Postgres restart, an unbounded SELECT 1 can
			// block on TCP retransmit well past the interval this loop is
			// supposed to detect session loss on, letting another replica
			// acquire leadership while this one still believes it holds it
			// (transient double leadership).
			probeCtx, cancel := context.WithTimeout(ctx, m.leaderHeartbeatInterval)
			healthy := handle.Healthy(probeCtx)
			cancel()
			if !healthy {
				return
			}

			m.mu.Lock()
			running := m.cancel != nil
			m.mu.Unlock()
			if running {
				warnedNotStarted = false
				continue
			}

			started, err := m.Restart(ctx)
			if err != nil {
				m.logger.Error("poller leader election: failed to start poller on heartbeat retry", zap.Error(err))
			} else if started {
				warnedNotStarted = false
			} else if !warnedNotStarted {
				m.logger.Warn("poller leader election: still no datadog integration connected, poller not started")
				warnedNotStarted = true
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

	if !m.leading.Load() {
		// Not this replica's turn to run a poller - RunLeaderLoop will call
		// Restart itself from the currently stored integration the moment
		// this replica (or whichever one is actually leading) acquires
		// leadership, so a credential connected/rotated here still takes
		// effect without a process restart, just not necessarily on this
		// exact replica or instantly if it never leads. Checked under m.mu,
		// same critical section RunLeaderLoop uses to flip leading false
		// and stop the poller on leadership loss - otherwise a Restart
		// that read leading=true just before losing it could still start a
		// new poller after this replica no longer safely holds the lock.
		return false, nil
	}

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
	m.stopManualLocked()
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

// startManualScheduler builds and starts a poller.ManualScheduler, tracking
// it in manualCancel/manualDone. It is idempotent - a call while one is
// already tracked as running is a no-op, since RunLeaderLoop only calls this
// once per leadership term (right after acquiring the lock), and this
// scheduler is never restarted by Restart (see manualCancel's own doc
// comment). Uses m.parentCtx (not the ctx passed to RunLeaderLoop) as the
// scheduler's parent context, exactly like Restart does for the Datadog
// poller - the server's own lifetime context, not whatever the caller's ctx
// happens to be.
func (m *PollerManager) startManualScheduler() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.manualCancel != nil {
		return
	}

	scheduler := newManualSchedulerFromPool(m.pool, m.logger)

	runCtx, cancel := context.WithCancel(m.parentCtx)
	done := make(chan struct{})
	m.manualCancel = cancel
	m.manualDone = done

	go func() {
		defer close(done)
		scheduler.Run(runCtx)
	}()
}

// stopManualLocked cancels and waits for the currently running manual
// scheduler, if any. Callers must hold m.mu.
func (m *PollerManager) stopManualLocked() {
	if m.manualCancel == nil {
		return
	}
	m.manualCancel()
	<-m.manualDone
	m.manualCancel = nil
	m.manualDone = nil
}
