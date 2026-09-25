package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeStatusPageHostLookup returns a fixed result without touching Postgres,
// so the fallback dispatch paths can be tested as pure unit tests.
type fakeStatusPageHostLookup struct {
	page *db.StatusPage
	err  error
}

func (f *fakeStatusPageHostLookup) GetByHostname(ctx context.Context, hostname string) (*db.StatusPage, error) {
	return f.page, f.err
}

// countingTenantTxBeginner records whether BeginTenantTx was reached. The
// fallback paths must never open a tenant transaction - they run before any
// tenant is resolved.
type countingTenantTxBeginner struct {
	begun int
}

func (b *countingTenantTxBeginner) BeginTenantTx(ctx context.Context, userID, tenantID string) (pgx.Tx, error) {
	b.begun++
	return nil, errors.New("BeginTenantTx must not be called on the fallback path")
}

// TestHostRouter_UnmatchedOrUnpublishedHostname_ServesFallback proves
// AD-038's load-bearing behavior change: a hostname HostRouter does not
// resolve to a published status page now reaches the caller's fallback
// handler instead of a hard-coded 404. The fallback's status and body are
// deliberately distinct from a 404 so a literal http.NotFound could not
// satisfy this test. Both call sites are exercised: an unregistered hostname
// (lookup error) and a registered-but-unpublished page (state check).
func TestHostRouter_UnmatchedOrUnpublishedHostname_ServesFallback(t *testing.T) {
	tests := []struct {
		name   string
		lookup statusPageHostLookup
		host   string
	}{
		{
			name:   "unregistered hostname",
			lookup: &fakeStatusPageHostLookup{err: db.ErrNotFound},
			host:   "admin.example.com",
		},
		{
			name: "registered but unpublished page",
			lookup: &fakeStatusPageHostLookup{page: &db.StatusPage{
				ID:       "draft-status-page",
				TenantID: "draft-tenant",
				State:    "draft",
			}},
			host: "status.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publicHandlerCalled := false
			publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				publicHandlerCalled = true
			})

			fallbackCalled := false
			fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalled = true
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("fallback-served"))
			})

			beginner := &countingTenantTxBeginner{}
			req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			HostRouter(tt.lookup, beginner, publicHandler, fallback).ServeHTTP(rec, req)

			if !fallbackCalled {
				t.Fatal("fallback was not invoked for an unmatched/unpublished hostname")
			}
			if publicHandlerCalled {
				t.Error("publicHandler was invoked for an unmatched/unpublished hostname")
			}
			if rec.Code != http.StatusTeapot {
				t.Errorf("status = %d, want %d from the fallback handler", rec.Code, http.StatusTeapot)
			}
			if rec.Body.String() != "fallback-served" {
				t.Errorf("body = %q, want %q from the fallback handler", rec.Body.String(), "fallback-served")
			}
			if beginner.begun != 0 {
				t.Errorf("BeginTenantTx() called %d times on the fallback path, want 0", beginner.begun)
			}
		})
	}
}
