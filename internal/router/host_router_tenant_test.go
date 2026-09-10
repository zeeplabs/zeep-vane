//go:build integration

package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// routerRLSRole is a non-superuser Postgres role this file runs the
// request-scoped queries as. The disposable container AGENTS.md §3
// prescribes makes POSTGRES_USER a superuser, and a superuser bypasses RLS
// unconditionally - ALTER TABLE ... FORCE ROW LEVEL SECURITY (0024) only
// binds non-superuser roles. Without this, the assertions below would pass
// even if HostRouter set no tenant at all. Named distinctly from
// internal/db's own rlsTestRole so the two packages' test binaries (which
// `go test ./...` runs in parallel against the same database) never race on
// each other's CREATE ROLE / GRANT.
const routerRLSRole = "vane_router_rls_test"

// rlsRoleBeginner opens HostRouter's request transaction the same way
// db.Pool.BeginTenantTx does (SET LOCAL app.tenant_id), but first downgrades
// the connection from the superuser test role to routerRLSRole - a role RLS
// actually restricts. This is the whole point of the fixture: it makes the
// tenant scoping HostRouter installs load-bearing rather than decorative.
type rlsRoleBeginner struct {
	pool *db.Pool
	// begun counts BeginTenantTx calls, so a test can assert HostRouter
	// opened no transaction at all for a request it rejected.
	begun int
	// tenantIDs records the tenant id HostRouter asked for, in order.
	tenantIDs []string
}

func (b *rlsRoleBeginner) BeginTenantTx(ctx context.Context, userID, tenantID string) (pgx.Tx, error) {
	b.begun++
	b.tenantIDs = append(b.tenantIDs, tenantID)

	tx, err := b.pool.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET ROLE "+routerRLSRole); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if tenantID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return tx, nil
}

// newTenantRLSTestPool returns a migrated superuser pool with routerRLSRole
// created and granted read access to the tables a public status-page
// request touches. Unlike newHostRouterTestPool it pins no tenant onto the
// connection: these tests seed more than one tenant, and pinning one at
// session level would defeat the per-request SET LOCAL under test.
func newTenantRLSTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+routerRLSRole+`') THEN
				CREATE ROLE `+routerRLSRole+` NOLOGIN;
			END IF;
		END $$;`,
	); err != nil {
		t.Fatalf("creating %s role returned unexpected error: %v", routerRLSRole, err)
	}
	if _, err := pool.Exec(ctx,
		"GRANT SELECT ON tenants, services, incidents, status_pages, domains TO "+routerRLSRole,
	); err != nil {
		t.Fatalf("granting SELECT to %s returned unexpected error: %v", routerRLSRole, err)
	}

	return pool
}

// tenantFixture is one seeded tenant with a published status page and one
// service, everything named off prefix so cross-tenant leakage is visible
// by name alone.
type tenantFixture struct {
	tenantID    string
	tenantName  string
	hostname    string
	serviceName string
}

// seedTenantWithPublishedStatusPage is seedTenantWithStatusPage's
// published-state case, which is what most tests here need.
func seedTenantWithPublishedStatusPage(t *testing.T, pool *db.Pool, prefix string) tenantFixture {
	t.Helper()
	return seedTenantWithStatusPage(t, pool, prefix, "published")
}

// seedTenantWithStatusPage creates a tenant, a domain, a status page under
// it in the given state, and one service - all inside a transaction with
// app.tenant_id set to the new tenant, which every 0024 tenant_id DEFAULT
// and WITH CHECK requires.
func seedTenantWithStatusPage(t *testing.T, pool *db.Pool, prefix, state string) tenantFixture {
	t.Helper()
	ctx := context.Background()

	var tenantID string
	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		t.Fatalf("generating tenant id returned unexpected error: %v", err)
	}

	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	txCtx := db.WithTenantTx(ctx, tx)

	fixture := tenantFixture{
		tenantID:    tenantID,
		tenantName:  prefix + "-tenant",
		serviceName: prefix + "-service",
	}

	if _, err := pool.Exec(txCtx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, fixture.tenantName); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(txCtx, "INSERT INTO services (name, slo_id) VALUES ($1, $2)", fixture.serviceName, prefix+"-slo"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seeding service returned unexpected error: %v", err)
	}

	rootHostname := prefix + ".example.com"
	subdomain := "status"
	fixture.hostname = subdomain + "." + rootHostname

	domains := db.NewDomainRepository(pool)
	domain := &db.Domain{Hostname: rootHostname}
	if err := domains.Create(txCtx, domain); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seeding domain returned unexpected error: %v", err)
	}

	// Inserted directly rather than through StatusPageRepository.Create:
	// Create always inserts at the "draft" default and has no way to set
	// state, which is exactly what these fixtures vary.
	if _, err := pool.Exec(txCtx,
		"INSERT INTO status_pages (name, subdomain, domain_id, state) VALUES ($1, $2, $3, $4)",
		prefix+"-page", subdomain, domain.ID, state,
	); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seeding status page returned unexpected error: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing tenant fixture returned unexpected error: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		cleanupTx, err := pool.BeginTenantTx(cleanupCtx, "", tenantID)
		if err != nil {
			return
		}
		c := db.WithTenantTx(cleanupCtx, cleanupTx)
		_, _ = pool.Exec(c, "DELETE FROM status_pages WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(c, "DELETE FROM domains WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(c, "DELETE FROM services WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(c, "DELETE FROM tenants WHERE id = $1", tenantID)
		_ = cleanupTx.Commit(cleanupCtx)
	})

	return fixture
}

// collectStrings runs sql on the request's context (so it lands on the
// transaction HostRouter opened) and returns every scanned value.
func collectStrings(t *testing.T, pool *db.Pool, ctx context.Context, sql string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("Query(%q) returned unexpected error: %v", sql, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("Scan() returned unexpected error: %v", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() returned unexpected error: %v", err)
	}
	return out
}

// TestHostRouter_PublicRequest_ScopedToHostnameTenant proves TENANT-01/02
// for the unauthenticated path: a visitor hitting tenant A's status-page
// hostname sees only tenant A's rows, and a visitor hitting tenant B's sees
// only tenant B's - asserted from a non-superuser role with deliberately
// unfiltered queries ("SELECT name FROM services", the shape a repository
// that forgot "WHERE tenant_id = ?" would run) so RLS, not the SQL, is what
// does the filtering. Both directions are exercised: isolation must not
// hold merely because one tenant was seeded first.
func TestHostRouter_PublicRequest_ScopedToHostnameTenant(t *testing.T) {
	pool := newTenantRLSTestPool(t)
	suffix := tRouterUniqueSuffix()
	tenantA := seedTenantWithPublishedStatusPage(t, pool, fmt.Sprintf("host-tenant-a-%d", suffix))
	tenantB := seedTenantWithPublishedStatusPage(t, pool, fmt.Sprintf("host-tenant-b-%d", suffix))

	statusPages := db.NewStatusPageRepository(pool)

	for _, tc := range []struct {
		name string
		want tenantFixture
		// other is the tenant whose data must never appear.
		other tenantFixture
	}{
		{name: "tenant A hostname", want: tenantA, other: tenantB},
		{name: "tenant B hostname", want: tenantB, other: tenantA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotTenantID string
			var gotTenantIDPresent bool
			var services, tenants []string

			publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotTenantID, gotTenantIDPresent = TenantIDFromContext(r.Context())
				services = collectStrings(t, pool, r.Context(), "SELECT name FROM services")
				tenants = collectStrings(t, pool, r.Context(), "SELECT name FROM tenants")
				w.WriteHeader(http.StatusOK)
			})

			beginner := &rlsRoleBeginner{pool: pool}
			req := httptest.NewRequest(http.MethodGet, "/api/public-status", nil)
			req.Host = tc.want.hostname
			rec := httptest.NewRecorder()

			HostRouter(statusPages, beginner, publicHandler).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if !gotTenantIDPresent {
				t.Fatal("TenantIDFromContext() reported no tenant id on the request context")
			}
			if gotTenantID != tc.want.tenantID {
				t.Errorf("TenantIDFromContext() = %q, want %q", gotTenantID, tc.want.tenantID)
			}
			if beginner.begun != 1 {
				t.Errorf("BeginTenantTx() called %d times, want 1", beginner.begun)
			}
			if len(beginner.tenantIDs) != 1 || beginner.tenantIDs[0] != tc.want.tenantID {
				t.Errorf("BeginTenantTx() tenant ids = %v, want [%q]", beginner.tenantIDs, tc.want.tenantID)
			}

			if len(services) != 1 || services[0] != tc.want.serviceName {
				t.Errorf("services visible to the public request = %v, want exactly [%q]", services, tc.want.serviceName)
			}
			if len(tenants) != 1 || tenants[0] != tc.want.tenantName {
				t.Errorf("tenants visible to the public request = %v, want exactly [%q]", tenants, tc.want.tenantName)
			}
			for _, name := range services {
				if name == tc.other.serviceName {
					t.Errorf("public request for %s saw the other tenant's service %q - cross-tenant leak", tc.want.hostname, name)
				}
			}
			for _, name := range tenants {
				if name == tc.other.tenantName {
					t.Errorf("public request for %s saw the other tenant %q - cross-tenant leak", tc.want.hostname, name)
				}
			}
		})
	}
}

// TestHostRouter_UnknownHostname_404_NoTenantTransaction is the regression
// guard on the pre-existing 404 behaviour: adding tenant resolution must
// not change what an unregistered hostname gets, and must not open a
// transaction for a request that never reaches the public handler.
func TestHostRouter_UnknownHostname_404_NoTenantTransaction(t *testing.T) {
	pool := newTenantRLSTestPool(t)
	statusPages := db.NewStatusPageRepository(pool)

	publicHandlerCalled := false
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	beginner := &rlsRoleBeginner{pool: pool}
	req := httptest.NewRequest(http.MethodGet, "/api/public-status", nil)
	req.Host = fmt.Sprintf("no-such-page-%d.example.com", tRouterUniqueSuffix())
	rec := httptest.NewRecorder()

	HostRouter(statusPages, beginner, publicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if publicHandlerCalled {
		t.Error("publicHandler was invoked for an unknown hostname")
	}
	if beginner.begun != 0 {
		t.Errorf("BeginTenantTx() called %d times for an unknown hostname, want 0", beginner.begun)
	}
}

// beginAnonymousRLSTx opens a transaction downgraded to routerRLSRole with
// app.tenant_id deliberately left unset - the exact session shape a real
// deployment's hostname lookup runs in, since an anonymous visitor carries
// no tenant signal and the lookup is what discovers one. Returns the tx
// (caller must roll it back) and a context that routes pool.* calls onto it.
func beginAnonymousRLSTx(t *testing.T, pool *db.Pool) (pgx.Tx, context.Context) {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET ROLE "+routerRLSRole); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("SET ROLE returned unexpected error: %v", err)
	}

	return tx, db.WithTenantTx(ctx, tx)
}

// TestHostRouter_AnonymousSession_ResolvesPublishedPageAndScopesData is the
// regression test for the bootstrap paradox AD-023 closes: the hostname ->
// tenant lookup runs before any tenant is known, so under a non-superuser
// role with no app.tenant_id it hit 0024's fail-closed status_pages/domains
// policies and returned ErrNotFound for a page that exists and is published
// - every public status page would 404. Here the whole request, lookup
// included, runs in such a session, and must still resolve the page, carry
// its tenant id into the handler, and return that tenant's data only.
func TestHostRouter_AnonymousSession_ResolvesPublishedPageAndScopesData(t *testing.T) {
	pool := newTenantRLSTestPool(t)
	suffix := tRouterUniqueSuffix()
	want := seedTenantWithPublishedStatusPage(t, pool, fmt.Sprintf("anon-published-%d", suffix))
	other := seedTenantWithPublishedStatusPage(t, pool, fmt.Sprintf("anon-other-%d", suffix))

	statusPages := db.NewStatusPageRepository(pool)

	anonTx, anonCtx := beginAnonymousRLSTx(t, pool)
	defer func() { _ = anonTx.Rollback(context.Background()) }()

	// The lookup itself, under the anonymous session, is the thing that used
	// to fail.
	statusPage, err := statusPages.GetByHostname(anonCtx, want.hostname)
	if err != nil {
		t.Fatalf("GetByHostname() in an anonymous non-superuser session returned unexpected error: %v", err)
	}
	if statusPage.TenantID != want.tenantID {
		t.Errorf("GetByHostname() TenantID = %q, want %q", statusPage.TenantID, want.tenantID)
	}
	if statusPage.State != "published" {
		t.Errorf("GetByHostname() State = %q, want %q", statusPage.State, "published")
	}

	var gotTenantID string
	var gotTenantIDPresent bool
	var services []string
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenantID, gotTenantIDPresent = TenantIDFromContext(r.Context())
		services = collectStrings(t, pool, r.Context(), "SELECT name FROM services")
		w.WriteHeader(http.StatusOK)
	})

	beginner := &rlsRoleBeginner{pool: pool}
	req := httptest.NewRequest(http.MethodGet, "/api/public-status", nil).WithContext(anonCtx)
	req.Host = want.hostname
	rec := httptest.NewRecorder()

	HostRouter(statusPages, beginner, publicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotTenantIDPresent {
		t.Fatal("TenantIDFromContext() reported no tenant id on the request context")
	}
	if gotTenantID != want.tenantID {
		t.Errorf("TenantIDFromContext() = %q, want %q", gotTenantID, want.tenantID)
	}
	if len(services) != 1 || services[0] != want.serviceName {
		t.Errorf("services visible to the anonymous request = %v, want exactly [%q]", services, want.serviceName)
	}
	for _, name := range services {
		if name == other.serviceName {
			t.Errorf("anonymous request for %s saw the other tenant's service %q - cross-tenant leak", want.hostname, name)
		}
	}
}

// TestHostRouter_AnonymousSession_DraftPageStaysInvisible is the other half
// of AD-023: the new read policy is scoped to published pages, so a draft
// page's hostname must still resolve to nothing in an anonymous session -
// both at the repository (the policy, not HostRouter's state check, is what
// hides it) and end to end.
func TestHostRouter_AnonymousSession_DraftPageStaysInvisible(t *testing.T) {
	pool := newTenantRLSTestPool(t)
	draft := seedTenantWithStatusPage(t, pool, fmt.Sprintf("anon-draft-%d", tRouterUniqueSuffix()), "draft")

	statusPages := db.NewStatusPageRepository(pool)

	anonTx, anonCtx := beginAnonymousRLSTx(t, pool)
	defer func() { _ = anonTx.Rollback(context.Background()) }()

	if _, err := statusPages.GetByHostname(anonCtx, draft.hostname); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("GetByHostname() for a draft page's hostname error = %v, want ErrNotFound", err)
	}

	publicHandlerCalled := false
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	beginner := &rlsRoleBeginner{pool: pool}
	req := httptest.NewRequest(http.MethodGet, "/api/public-status", nil).WithContext(anonCtx)
	req.Host = draft.hostname
	rec := httptest.NewRecorder()

	HostRouter(statusPages, beginner, publicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if publicHandlerCalled {
		t.Error("publicHandler was invoked for a draft status page's hostname")
	}
	if beginner.begun != 0 {
		t.Errorf("BeginTenantTx() called %d times for a draft page, want 0", beginner.begun)
	}
}

var tRouterUniqueCounter int64

// tRouterUniqueSuffix returns a value unique per call and per run, so
// fixtures never collide on hostname or tenant name - within this file's
// tests (the counter) or against rows a previous interrupted run left
// behind (the wall clock).
func tRouterUniqueSuffix() int64 {
	tRouterUniqueCounter++
	return time.Now().UnixNano() + tRouterUniqueCounter
}
