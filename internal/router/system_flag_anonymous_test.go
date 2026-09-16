//go:build integration

package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// TestHostRouter_AnonymousRequest_AppIsSystemNeverSet is the anonymous
// half of AD-024's containment guarantee (the authenticated half lives in
// internal/api/system_flag_http_test.go): the public status-page path is
// the one HTTP path that legitimately runs with no tenant of its own -
// the same session shape the poller's enumeration needs - so it is the
// path where a mistakenly broad system flag would be most damaging. The
// probe reads app.is_system from the request's own transaction, which
// HostRouter opened after resolving the hostname's tenant.
func TestHostRouter_AnonymousRequest_AppIsSystemNeverSet(t *testing.T) {
	pool := newTenantRLSTestPool(t)
	fixture := seedTenantWithPublishedStatusPage(t, pool, fmt.Sprintf("anon-issystem-%d", tRouterUniqueSuffix()))

	anonTx, anonCtx := beginAnonymousRLSTx(t, pool)
	defer func() { _ = anonTx.Rollback(context.Background()) }()

	var gotIsSystem string
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := pool.QueryRow(r.Context(), "SELECT COALESCE(current_setting('app.is_system', true), '')").Scan(&gotIsSystem); err != nil {
			t.Errorf("reading app.is_system returned unexpected error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/public-status", nil).WithContext(anonCtx)
	req.Host = fixture.hostname
	rec := httptest.NewRecorder()

	HostRouter(db.NewStatusPageRepository(pool), &rlsRoleBeginner{pool: pool}, publicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotIsSystem == "true" {
		t.Errorf("app.is_system seen by an anonymous public-status request = %q, want it unset (AD-024: no HTTP path may claim system scope)", gotIsSystem)
	}
}
