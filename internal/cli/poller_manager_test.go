//go:build integration

package cli

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const pollerManagerTestMasterKey = "poller-manager-test-master-key"

// storeTestDatadogIntegration upserts a fake (never dialed - see the large
// PollIntervalSeconds used by these tests) Datadog integration row so
// newPollerFromStoredIntegration has something to build from.
func storeTestDatadogIntegration(t *testing.T, pool *db.Pool) {
	t.Helper()
	encAPIKey, err := crypto.Encrypt(pollerManagerTestMasterKey, []byte("fake-api-key"))
	if err != nil {
		t.Fatalf("crypto.Encrypt(api key) returned unexpected error: %v", err)
	}
	encAppKey, err := crypto.Encrypt(pollerManagerTestMasterKey, []byte("fake-app-key"))
	if err != nil {
		t.Fatalf("crypto.Encrypt(app key) returned unexpected error: %v", err)
	}

	repo := db.NewIntegrationRepository(pool)
	if err := repo.UpsertDatadog(context.Background(), encAPIKey, encAppKey); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}
}

// pollerManagerTestConfig returns a config.Config whose poll interval is
// large enough that no test in this file ever waits for a real tick - the
// poller's ticker never fires within the test's lifetime, so Run only ever
// exits via ctx cancellation, never via pollOnce (which would otherwise
// need real network access to Datadog).
func pollerManagerTestConfig() config.Config {
	return config.Config{MasterKey: pollerManagerTestMasterKey, PollIntervalSeconds: 3600}
}

// TestPollerManager_Restart_WithStoredIntegration_StartsAndTracksRunning
// covers PLD-01/PLD-04 at the PollerManager level directly (a real gap the
// Verifier found: no test exercised PollerManager itself, only the spy
// used by the HTTP-layer tests). A no-op Restart (Mutant B from the
// Verifier's sensor) would return started=false or leave m.cancel unset;
// this test fails against that mutant.
func TestPollerManager_Restart_WithStoredIntegration_StartsAndTracksRunning(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop())
	t.Cleanup(mgr.Stop)

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if !started {
		t.Fatal("Restart() started = false, want true - a stored integration exists")
	}

	mgr.mu.Lock()
	running := mgr.cancel != nil && mgr.done != nil
	mgr.mu.Unlock()
	if !running {
		t.Fatal("PollerManager has no cancel/done tracked after a successful Restart, want the poller goroutine to be tracked as running")
	}
}

// TestPollerManager_Restart_NoIntegration_ReturnsFalseWithoutError covers
// PLD-02/PLD-03's "nothing to start" contract at the manager level.
func TestPollerManager_Restart_NoIntegration_ReturnsFalseWithoutError(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	// Deliberately no storeTestDatadogIntegration call.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop())
	t.Cleanup(mgr.Stop)

	started, err := mgr.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart() returned unexpected error: %v", err)
	}
	if started {
		t.Fatal("Restart() started = true, want false - no integration is stored")
	}
}

// TestPollerManager_Stop_ExitsPromptlyAndClearsState covers PLD-04: once
// Stop returns, no poller goroutine is left tracked as running, and a
// subsequent Restart is unaffected by whatever ran before it.
func TestPollerManager_Stop_ExitsPromptlyAndClearsState(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop())

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("Restart() = (%v, %v), want (true, nil)", started, err)
	}

	stopped := make(chan struct{})
	go func() {
		mgr.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s, want it to cancel and wait for the running poller promptly")
	}

	mgr.mu.Lock()
	cleared := mgr.cancel == nil && mgr.done == nil
	mgr.mu.Unlock()
	if !cleared {
		t.Error("PollerManager still tracks a running poller after Stop(), want cancel/done cleared")
	}
}

// TestPollerManager_Restart_CalledTwice_TearsDownPreviousBeforeStartingNew
// covers PLD-05's "replace the running poller" contract and the Edge
// Cases' "two pollers never run concurrently" guarantee: a second Restart
// must fully tear down whatever is running before tracking a new one, so
// there is never a moment with two independently-tracked pollers.
func TestPollerManager_Restart_CalledTwice_TearsDownPreviousBeforeStartingNew(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewPollerManager(ctx, pool, pollerManagerTestConfig(), zap.NewNop())
	t.Cleanup(mgr.Stop)

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("first Restart() = (%v, %v), want (true, nil)", started, err)
	}
	mgr.mu.Lock()
	firstDone := mgr.done
	mgr.mu.Unlock()

	if started, err := mgr.Restart(context.Background()); err != nil || !started {
		t.Fatalf("second Restart() = (%v, %v), want (true, nil)", started, err)
	}

	select {
	case <-firstDone:
	default:
		t.Error("first poller's done channel is not closed after a second Restart, want the previous poller torn down before a new one starts")
	}

	mgr.mu.Lock()
	secondDone := mgr.done
	running := mgr.cancel != nil && secondDone != nil
	mgr.mu.Unlock()
	if !running {
		t.Fatal("no cancel/done tracked after second Restart, want the new poller tracked as running")
	}
	if secondDone == firstDone {
		t.Error("second Restart reused the first run's done channel, want a fresh one per Restart")
	}
}
