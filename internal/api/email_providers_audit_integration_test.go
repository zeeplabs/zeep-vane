//go:build integration

package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// alwaysValidEmailProvider is a ProviderFactory whose Provider always
// validates successfully and never actually sends anything - lets these
// tests exercise the real email.Service (so Connect/Activate genuinely
// persist an email_providers row, which the audit entries need a real ID
// from) without hitting a real SendGrid/Resend API.
type alwaysValidEmailProvider struct{}

func (alwaysValidEmailProvider) ValidateCredentials(context.Context) error { return nil }
func (alwaysValidEmailProvider) Send(context.Context, email.Message) error { return nil }

func alwaysValidEmailProviderFactory(provider, apiKey string) (email.Provider, error) {
	return alwaysValidEmailProvider{}, nil
}

// newEmailProvidersIntegrationRouter wires the real email.Service (not the
// fake used by the rest of this file's unit tests) behind real
// authentication, so a successful Connect/Activate actually persists an
// email_providers row and its own audit entry - unlike newEmailProvidersRouter,
// where no actor is ever in context and the fake service persists nothing.
func newEmailProvidersIntegrationRouter(t *testing.T) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()
	pool, _ := newAPITenantScopedPool(t)
	admins := db.NewUserRepository(pool)

	repo := db.NewEmailProviderRepository(pool)
	svc, err := email.NewService(repo, alwaysValidEmailProviderFactory, "email-audit-test-master-key", zap.NewNop())
	if err != nil {
		t.Fatalf("email.NewService() returned unexpected error: %v", err)
	}
	handler := NewEmailProvidersHandler(svc, repo, audit.NewLog(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Post("/api/integrations/email/{provider}", handler.Connect)
		protected.Post("/api/integrations/email/{provider}/activate", handler.Activate)
	})
	return r, pool, admins
}

func doAuthedConnectRequest(t *testing.T, r http.Handler, token, provider string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/"+provider, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doAuthedActivateRequest(t *testing.T, r http.Handler, token, provider string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/"+provider+"/activate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestConnectEmailProvider_ValidRequest_RecordsEmailProviderConnectedAudit
// covers AUDITEXP-09.
func TestConnectEmailProvider_ValidRequest_RecordsEmailProviderConnectedAudit(t *testing.T) {
	r, pool, admins := newEmailProvidersIntegrationRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM email_providers WHERE provider = 'sendgrid'")
	})

	rec := doAuthedConnectRequest(t, r, token, "sendgrid", []byte(`{"api_key":"k","from_email":"a@b.com","from_name":"Vane"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE action = 'email_provider_connected' ORDER BY created_at DESC LIMIT 1")
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "SendGrid" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "SendGrid")
	}
}

// TestActivateEmailProvider_ValidRequest_RecordsEmailProviderActivatedAudit
// covers AUDITEXP-10.
func TestActivateEmailProvider_ValidRequest_RecordsEmailProviderActivatedAudit(t *testing.T) {
	r, pool, admins := newEmailProvidersIntegrationRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM email_providers WHERE provider = 'resend'")
	})

	connectRec := doAuthedConnectRequest(t, r, token, "resend", []byte(`{"api_key":"k","from_email":"a@b.com","from_name":"Vane"}`))
	if connectRec.Code != http.StatusCreated {
		t.Fatalf("setup connect status = %d, want %d, body = %s", connectRec.Code, http.StatusCreated, connectRec.Body.String())
	}

	rec := doAuthedActivateRequest(t, r, token, "resend")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE action = 'email_provider_activated' ORDER BY created_at DESC LIMIT 1")
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "Resend" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "Resend")
	}
}
