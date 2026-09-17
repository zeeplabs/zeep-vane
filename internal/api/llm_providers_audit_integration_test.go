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
	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// alwaysValidLLMProvider is a ProviderFactory whose Provider always
// validates successfully - same reasoning as alwaysValidEmailProvider,
// exercising the real llm.Service without hitting a real OpenAI API.
type alwaysValidLLMProvider struct{}

func (alwaysValidLLMProvider) Complete(context.Context, string, string) (string, error) {
	return "", nil
}
func (alwaysValidLLMProvider) ValidateCredentials(context.Context) error { return nil }

func alwaysValidLLMProviderFactory(provider, apiKey, model string) (llm.Provider, error) {
	return alwaysValidLLMProvider{}, nil
}

// newLLMProvidersIntegrationRouter mirrors newEmailProvidersIntegrationRouter:
// the real llm.Service behind real authentication, so Connect/Activate
// genuinely persist an llm_providers row for the audit entry to reference.
func newLLMProvidersIntegrationRouter(t *testing.T) (http.Handler, *db.Pool, *db.UserRepository) {
	t.Helper()
	pool, _ := newAPITenantScopedPool(t)
	admins := db.NewUserRepository(pool)

	repo := db.NewLLMProviderRepository(pool)
	svc := llm.NewService(db.NewLLMProviderStore(repo), alwaysValidLLMProviderFactory, "llm-audit-test-master-key", zap.NewNop())
	handler := NewLLMProvidersHandler(svc, repo, audit.NewLog(pool), zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins, db.NewSessionRepository(pool), zap.NewNop()))
		protected.Use(TenantContext(pool, db.NewTenantMembershipRepository(pool), zap.NewNop()))
		protected.Post("/api/integrations/llm/{provider}", handler.Connect)
		protected.Post("/api/integrations/llm/{provider}/activate", handler.Activate)
		protected.Delete("/api/integrations/llm/{provider}", handler.Disconnect)
	})
	return r, pool, admins
}

func doAuthedLLMConnectRequest(t *testing.T, r http.Handler, token, provider string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/"+provider, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doAuthedLLMActivateRequest(t *testing.T, r http.Handler, token, provider string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/"+provider+"/activate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doAuthedLLMDisconnectRequest(t *testing.T, r http.Handler, token, provider string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/integrations/llm/"+provider, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestConnectLLMProvider_ValidRequest_RecordsLLMProviderConnectedAudit
// covers AUDITEXP-11.
func TestConnectLLMProvider_ValidRequest_RecordsLLMProviderConnectedAudit(t *testing.T) {
	r, pool, admins := newLLMProvidersIntegrationRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM llm_providers WHERE provider = 'openai'") })

	rec := doAuthedLLMConnectRequest(t, r, token, "openai", []byte(`{"api_key":"k","model":"gpt-4o-mini"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE action = 'llm_provider_connected' ORDER BY created_at DESC LIMIT 1")
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "OpenAI" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "OpenAI")
	}
}

// TestActivateLLMProvider_ValidRequest_RecordsLLMProviderActivatedAudit
// covers AUDITEXP-12.
func TestActivateLLMProvider_ValidRequest_RecordsLLMProviderActivatedAudit(t *testing.T) {
	r, pool, admins := newLLMProvidersIntegrationRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM llm_providers WHERE provider = 'openai'") })

	connectRec := doAuthedLLMConnectRequest(t, r, token, "openai", []byte(`{"api_key":"k","model":"gpt-4o-mini"}`))
	if connectRec.Code != http.StatusCreated {
		t.Fatalf("setup connect status = %d, want %d, body = %s", connectRec.Code, http.StatusCreated, connectRec.Body.String())
	}

	rec := doAuthedLLMActivateRequest(t, r, token, "openai")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var gotTargetLabel string
	row := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE action = 'llm_provider_activated' ORDER BY created_at DESC LIMIT 1")
	if err := row.Scan(&gotTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotTargetLabel != "OpenAI" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotTargetLabel, "OpenAI")
	}
}

// TestDisconnectLLMProvider_ValidRequest_RecordsLLMProviderDisconnectedAudit
// covers PROVDISC-04 AC5/PROVDISC-06: disconnecting a connected provider
// through the real router (route wired in T8) produces exactly one
// llm_provider_disconnected audit row.
func TestDisconnectLLMProvider_ValidRequest_RecordsLLMProviderDisconnectedAudit(t *testing.T) {
	r, pool, admins := newLLMProvidersIntegrationRouter(t)
	token := issueTestSessionTokenWithTenant(t, admins, pool)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM llm_providers WHERE provider = 'openai'") })

	connectRec := doAuthedLLMConnectRequest(t, r, token, "openai", []byte(`{"api_key":"k","model":"gpt-4o-mini"}`))
	if connectRec.Code != http.StatusCreated {
		t.Fatalf("setup connect status = %d, want %d, body = %s", connectRec.Code, http.StatusCreated, connectRec.Body.String())
	}

	rec := doAuthedLLMDisconnectRequest(t, r, token, "openai")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var auditRowCount int
	countRow := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM admin_audit_log WHERE action = 'llm_provider_disconnected'")
	if err := countRow.Scan(&auditRowCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if auditRowCount != 1 {
		t.Fatalf("admin_audit_log rows with action=llm_provider_disconnected = %d, want exactly 1", auditRowCount)
	}

	var gotDisconnectTargetLabel string
	disconnectRow := pool.QueryRow(context.Background(),
		"SELECT target_label FROM admin_audit_log WHERE action = 'llm_provider_disconnected' ORDER BY created_at DESC LIMIT 1")
	if err := disconnectRow.Scan(&gotDisconnectTargetLabel); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if gotDisconnectTargetLabel != "OpenAI" {
		t.Errorf("admin_audit_log target_label = %q, want %q", gotDisconnectTargetLabel, "OpenAI")
	}

	var rowCount int
	rowCountRow := pool.QueryRow(context.Background(), "SELECT count(*) FROM llm_providers WHERE provider = 'openai'")
	if err := rowCountRow.Scan(&rowCount); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}
	if rowCount != 0 {
		t.Errorf("llm_providers row count for openai = %d, want 0 after disconnect", rowCount)
	}
}
