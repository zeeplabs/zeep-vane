package cli

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// PollerManager owns the single running Poller's lifecycle, letting it be
// (re)started after boot - when an admin connects Datadog or rotates its
// key through the API - without a process restart (PLD-01, PLD-05).
// Restart always tears down whatever poller is currently running (if any)
// before building and starting a fresh one from the integration row
// currently stored, so a rotated key is picked up the same way a first
// connect is (PLD-05) and concurrent calls never leave two pollers running
// against the same services (Edge Cases).
type PollerManager struct {
	mu        sync.Mutex
	parentCtx context.Context
	pool      *db.Pool
	cfg       config.Config
	logger    *zap.Logger
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewPollerManager builds a PollerManager. parentCtx is the server's own
// lifetime context - canceling it (e.g. on SIGINT/SIGTERM) is what lets a
// poller started via Restart still exit on shutdown, the same as the
// boot-time poller does today.
func NewPollerManager(parentCtx context.Context, pool *db.Pool, cfg config.Config, logger *zap.Logger) *PollerManager {
	return &PollerManager{parentCtx: parentCtx, pool: pool, cfg: cfg, logger: logger}
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
