package dbtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/auth"
)

// SeedIssueTestSession inserts the shared fixture user + sessions row that
// backs auth.IssueTestSessionID (a fixed UUID sentinel integration tests
// pass to IssueSessionWithTenant in place of a real sessions-table row).
// RequireAuth's lookup checks row existence + revoked_at only, never
// user_id-vs-sub, so one shared row is enough for every test that issues a
// token this way; ON CONFLICT DO NOTHING makes both inserts idempotent
// across tests sharing the same TEST_DATABASE_URL.
//
// Both integration fixtures that mint tokens with IssueTestSessionID
// (internal/api's newAPITenantScopedPool and internal/cli's
// newServeTestPoolWithTenant) call this, so each package's suite passes in
// isolation instead of depending on the other package's fixture having run
// first in a shared database - which is exactly why internal/cli used to
// 401 when run on its own.
func SeedIssueTestSession(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("dbtest: failed to open fixture connection: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)
		 ON CONFLICT (email) DO NOTHING`,
		auth.IssueTestSessionUserID, "issue-test-session-fixture@test.local", "fixture-hash",
	); err != nil {
		t.Fatalf("dbtest: seeding fixture user for IssueTestSessionID failed: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO sessions (id, user_id) VALUES ($1, $2)
		 ON CONFLICT (id) DO NOTHING`,
		auth.IssueTestSessionID, auth.IssueTestSessionUserID,
	); err != nil {
		t.Fatalf("dbtest: seeding fixture session row for IssueTestSessionID failed: %v", err)
	}
}
