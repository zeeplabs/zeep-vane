//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func newInstanceConfigRouter(t *testing.T, dnsTarget string) (http.Handler, *db.AdminRepository) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	admins := db.NewAdminRepository(pool)
	handler := NewInstanceConfigHandler(dnsTarget, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/instance/dns-target", handler.DNSTarget)
	})

	return r, admins
}

func getDNSTarget(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/instance/dns-target", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestDNSTarget_Configured_200ReturnsValue asserts SPD-10: a configured
// PUBLIC_DNS_TARGET is returned verbatim.
func TestDNSTarget_Configured_200ReturnsValue(t *testing.T) {
	r, admins := newInstanceConfigRouter(t, "vane.example.com")
	token := issueTestSessionToken(t, admins)

	rec := getDNSTarget(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body dnsTargetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Target == nil || *body.Target != "vane.example.com" {
		t.Errorf("Target = %v, want %q", body.Target, "vane.example.com")
	}
}

// TestDNSTarget_Unconfigured_200ReturnsNull asserts SPD-10's fallback:
// an unconfigured PUBLIC_DNS_TARGET (empty string) surfaces as target: null,
// not an error - the missing hint never blocks anything.
func TestDNSTarget_Unconfigured_200ReturnsNull(t *testing.T) {
	r, admins := newInstanceConfigRouter(t, "")
	token := issueTestSessionToken(t, admins)

	rec := getDNSTarget(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body dnsTargetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if body.Target != nil {
		t.Errorf("Target = %v, want nil", *body.Target)
	}
}

// TestDNSTarget_NoAuth_401 asserts SPD-11's RBAC floor on this endpoint:
// unauthenticated requests are rejected.
func TestDNSTarget_NoAuth_401(t *testing.T) {
	r, _ := newInstanceConfigRouter(t, "vane.example.com")

	rec := getDNSTarget(t, r, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
