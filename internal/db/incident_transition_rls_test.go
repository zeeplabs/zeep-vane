//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// grantIncidentTablesToRLSRole extends newRLSTestPool's fixed GRANT list
// with the incident tables (same approach as domain_repository_test.go).
func grantIncidentTablesToRLSRole(t *testing.T, pool *Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"GRANT SELECT, INSERT, UPDATE, DELETE ON incidents, incident_updates, incident_services TO "+rlsTestRole,
	); err != nil {
		t.Fatalf("granting incident DML to %s returned unexpected error: %v", rlsTestRole, err)
	}
}

// createRLSIncident creates an incident owned by tenantID through that
// tenant's own RLS session, committed, and registers its cleanup (run as
// the superuser connection, which bypasses RLS). Registered after the
// tenant seed, so it runs before the tenant row is deleted.
func createRLSIncident(t *testing.T, pool *Pool, repo *IncidentRepository, tenantID, title string) string {
	t.Helper()
	tx, ctx := beginRLSTx(t, pool, "", tenantID)
	incident := &Incident{Title: title}
	if err := repo.Create(ctx, incident, nil); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("setup commit returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		asSessionSuperuser(t, pool, func(ctx context.Context) {
			_, _ = pool.Exec(ctx, "DELETE FROM incident_updates WHERE incident_id = $1", incident.ID)
			_, _ = pool.Exec(ctx, "DELETE FROM incident_services WHERE incident_id = $1", incident.ID)
			_, _ = pool.Exec(ctx, "DELETE FROM incidents WHERE id = $1", incident.ID)
		})
	})
	return incident.ID
}

// asSessionSuperuser runs fn in a transaction whose current role is the
// connection's session user (the superuser test DSN role, which bypasses
// RLS). beginRLSTx's plain SET ROLE outlives a committed transaction on the
// pooled connection, so a bare pool.Exec afterwards may still run as
// rlsTestRole with no tenant and see nothing; SET LOCAL ROLE NONE undoes
// that for this transaction only.
func asSessionSuperuser(t *testing.T, pool *Pool, fn func(ctx context.Context)) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("SET LOCAL ROLE NONE returned unexpected error: %v", err)
	}
	fn(WithTenantTx(ctx, tx))
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
}

// incidentStateAsSuperuser reads an incident's status and timeline size
// outside any tenant session (superuser bypasses RLS), so assertions see
// the committed truth regardless of which tenant wrote it.
func incidentStateAsSuperuser(t *testing.T, pool *Pool, incidentID string) (status string, updates int) {
	t.Helper()
	asSessionSuperuser(t, pool, func(ctx context.Context) {
		if err := pool.QueryRow(ctx, "SELECT status FROM incidents WHERE id = $1", incidentID).Scan(&status); err != nil {
			t.Fatalf("reading incident status returned unexpected error: %v", err)
		}
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM incident_updates WHERE incident_id = $1", incidentID).Scan(&updates); err != nil {
			t.Fatalf("counting incident updates returned unexpected error: %v", err)
		}
	})
	return status, updates
}

// TestIncidentRepository_Transition_TenantTx_RLS runs Transition as the
// non-superuser rlsTestRole inside a tenant transaction (the shape the HTTP
// TenantContext middleware provides). It must run on that transaction - so
// RLS sees app.tenant_id and the caller keeps commit/rollback ownership -
// and must not reach another tenant's incident.
func TestIncidentRepository_Transition_TenantTx_RLS(t *testing.T) {
	pool := newRLSTestPool(t)
	grantIncidentTablesToRLSRole(t, pool)
	repo := NewIncidentRepository(pool)
	tenantA := seedTenantWithService(t, pool, fmt.Sprintf("transition-rls-a-%d", tUniqueSuffix()))
	tenantB := seedTenantWithService(t, pool, fmt.Sprintf("transition-rls-b-%d", tUniqueSuffix()))
	incidentA := createRLSIncident(t, pool, repo, tenantA, "transition rls incident")

	// Own tenant, caller rolls back: nothing persists, proving Transition
	// wrote on the caller's transaction instead of committing its own.
	txA, ctxA := beginRLSTx(t, pool, "", tenantA)
	inc, changed, err := repo.Transition(ctxA, incidentA, "resolved", "closing note", nil)
	if err != nil || !changed || inc.Status != "resolved" || inc.ResolvedAt == nil {
		_ = txA.Rollback(context.Background())
		t.Fatalf("Transition() as tenant A = (%+v, changed=%v, err=%v), want resolved and changed", inc, changed, err)
	}
	if err := txA.Rollback(context.Background()); err != nil {
		t.Fatalf("rollback returned unexpected error: %v", err)
	}
	if status, updates := incidentStateAsSuperuser(t, pool, incidentA); status != "investigating" || updates != 0 {
		t.Fatalf("after caller rollback: status=%q updates=%d, want investigating/0 (Transition must not commit a caller-owned tx)", status, updates)
	}

	// Own tenant, caller commits: the transition and both timeline rows land.
	txA, ctxA = beginRLSTx(t, pool, "", tenantA)
	if _, changed, err := repo.Transition(ctxA, incidentA, "resolved", "closing note", nil); err != nil || !changed {
		_ = txA.Rollback(context.Background())
		t.Fatalf("Transition() as tenant A = (changed=%v, err=%v), want changed", changed, err)
	}
	if err := txA.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
	if status, updates := incidentStateAsSuperuser(t, pool, incidentA); status != "resolved" || updates != 2 {
		t.Fatalf("after commit: status=%q updates=%d, want resolved/2", status, updates)
	}

	// Same status again on its own tenant: a no-op returning the incident.
	txA, ctxA = beginRLSTx(t, pool, "", tenantA)
	inc, changed, err = repo.Transition(ctxA, incidentA, "resolved", "", nil)
	if err != nil || changed || inc == nil || inc.Status != "resolved" {
		_ = txA.Rollback(context.Background())
		t.Fatalf("repeat Transition() as tenant A = (%+v, changed=%v, err=%v), want unchanged resolved incident", inc, changed, err)
	}
	if err := txA.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	// Another tenant: A's incident is invisible - ErrNotFound, nothing written.
	txB, ctxB := beginRLSTx(t, pool, "", tenantB)
	if _, _, err := repo.Transition(ctxB, incidentA, "investigating", "", nil); !errors.Is(err, ErrNotFound) {
		_ = txB.Rollback(context.Background())
		t.Fatalf("Transition() of tenant A's incident as tenant B error = %v, want ErrNotFound", err)
	}
	if err := txB.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
	if status, updates := incidentStateAsSuperuser(t, pool, incidentA); status != "resolved" || updates != 2 {
		t.Errorf("after tenant B attempt: status=%q updates=%d, want resolved/2 untouched", status, updates)
	}
}

// TestIncidentRepository_ConfirmPendingClose_TenantTx_RLS: same guarantees
// for ConfirmPendingClose - works on the caller's tenant transaction for its
// own incident, ErrNotFound for another tenant's.
func TestIncidentRepository_ConfirmPendingClose_TenantTx_RLS(t *testing.T) {
	pool := newRLSTestPool(t)
	grantIncidentTablesToRLSRole(t, pool)
	repo := NewIncidentRepository(pool)
	tenantA := seedTenantWithService(t, pool, fmt.Sprintf("confirm-close-rls-a-%d", tUniqueSuffix()))
	tenantB := seedTenantWithService(t, pool, fmt.Sprintf("confirm-close-rls-b-%d", tUniqueSuffix()))
	incidentA := createRLSIncident(t, pool, repo, tenantA, "confirm close rls incident")

	const comment = "service recovered"
	txA, ctxA := beginRLSTx(t, pool, "", tenantA)
	if err := repo.SetPendingCloseComment(ctxA, incidentA, comment); err != nil {
		_ = txA.Rollback(context.Background())
		t.Fatalf("setup SetPendingCloseComment() returned unexpected error: %v", err)
	}
	if err := txA.Commit(context.Background()); err != nil {
		t.Fatalf("setup commit returned unexpected error: %v", err)
	}

	txB, ctxB := beginRLSTx(t, pool, "", tenantB)
	if _, err := repo.ConfirmPendingClose(ctxB, incidentA, comment); !errors.Is(err, ErrNotFound) {
		_ = txB.Rollback(context.Background())
		t.Fatalf("ConfirmPendingClose() of tenant A's incident as tenant B error = %v, want ErrNotFound", err)
	}
	if err := txB.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
	if status, updates := incidentStateAsSuperuser(t, pool, incidentA); status != "investigating" || updates != 0 {
		t.Fatalf("after tenant B attempt: status=%q updates=%d, want investigating/0 untouched", status, updates)
	}

	txA, ctxA = beginRLSTx(t, pool, "", tenantA)
	inc, err := repo.ConfirmPendingClose(ctxA, incidentA, comment)
	if err != nil || inc.Status != "resolved" || inc.ResolvedAt == nil {
		_ = txA.Rollback(context.Background())
		t.Fatalf("ConfirmPendingClose() as tenant A = (%+v, err=%v), want resolved", inc, err)
	}
	if err := txA.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
	if status, updates := incidentStateAsSuperuser(t, pool, incidentA); status != "resolved" || updates != 1 {
		t.Errorf("after confirm: status=%q updates=%d, want resolved/1 (the AI summary entry)", status, updates)
	}
}

// TestIncidentRepository_Transition_SameStatus_NoOp covers the idempotent
// path without RLS: repeating the current status writes nothing (resolved_at
// unchanged, no timeline rows, note dropped) and reports changed=false.
func TestIncidentRepository_Transition_SameStatus_NoOp(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "transition same status incident")

	first, changed, err := repo.Transition(context.Background(), incident.ID, "resolved", "", nil)
	if err != nil || !changed {
		t.Fatalf("first Transition() = (changed=%v, err=%v), want changed", changed, err)
	}

	again, changed, err := repo.Transition(context.Background(), incident.ID, "resolved", "late note", nil)
	if err != nil {
		t.Fatalf("repeat Transition() returned unexpected error: %v", err)
	}
	if changed {
		t.Error("repeat Transition() changed = true, want false")
	}
	if again.Status != "resolved" || again.Title != incident.Title || again.ResolvedAt == nil || !again.ResolvedAt.Equal(*first.ResolvedAt) {
		t.Errorf("repeat Transition() = %+v, want the current incident with resolved_at %v", again, first.ResolvedAt)
	}

	updates, err := repo.ListUpdates(context.Background(), incident.ID)
	if err != nil {
		t.Fatalf("ListUpdates() returned unexpected error: %v", err)
	}
	if len(updates) != 1 {
		t.Errorf("len(updates) = %d, want 1 (only the first transition's entry)", len(updates))
	}
}

// TestIncidentRepository_Transition_UnknownIncident_ErrNotFound: the no-op
// path must not mask a missing incident.
func TestIncidentRepository_Transition_UnknownIncident_ErrNotFound(t *testing.T) {
	repo, _ := newIncidentRepoTestPool(t)

	if _, _, err := repo.Transition(context.Background(), "00000000-0000-0000-0000-000000000000", "resolved", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Transition() error = %v, want ErrNotFound", err)
	}
}
