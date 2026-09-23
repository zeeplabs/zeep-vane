package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// fakeLLMProviderService is a no-DB, no-HTTP double for llmProviderService,
// letting this handler suite run as a fast unit test independent of
// llm.Service's real dependencies (repository, provider factory).
type fakeLLMProviderService struct {
	connectErr    error
	setModelErr   error
	activateErr   error
	listResult    llm.ListResult
	listErr       error
	disconnectErr error

	setRootCauseErr error
	getRootCauseErr error

	connectCalls               []connectLLMCall
	setModelCalls              []setModelCall
	activateCalls              []string
	disconnectCalls            []string
	setRootCauseCalls          []bool
	rootCauseEnrichmentEnabled bool
}

type connectLLMCall struct {
	provider, apiKey, model string
}

type setModelCall struct {
	provider, model string
}

func (f *fakeLLMProviderService) Connect(ctx context.Context, provider, apiKey, model string) error {
	f.connectCalls = append(f.connectCalls, connectLLMCall{provider, apiKey, model})
	return f.connectErr
}

func (f *fakeLLMProviderService) SetModel(ctx context.Context, provider, model string) error {
	f.setModelCalls = append(f.setModelCalls, setModelCall{provider, model})
	return f.setModelErr
}

func (f *fakeLLMProviderService) Activate(ctx context.Context, provider string) error {
	f.activateCalls = append(f.activateCalls, provider)
	return f.activateErr
}

func (f *fakeLLMProviderService) List(ctx context.Context, page, pageSize int) (llm.ListResult, error) {
	return f.listResult, f.listErr
}

func (f *fakeLLMProviderService) Disconnect(ctx context.Context, provider string) error {
	f.disconnectCalls = append(f.disconnectCalls, provider)
	return f.disconnectErr
}

func (f *fakeLLMProviderService) SetRootCauseEnrichmentEnabled(ctx context.Context, enabled bool) error {
	f.setRootCauseCalls = append(f.setRootCauseCalls, enabled)
	if f.setRootCauseErr != nil {
		return f.setRootCauseErr
	}
	f.rootCauseEnrichmentEnabled = enabled
	return nil
}

func (f *fakeLLMProviderService) RootCauseEnrichmentEnabled(ctx context.Context) (bool, error) {
	if f.getRootCauseErr != nil {
		return false, f.getRootCauseErr
	}
	return f.rootCauseEnrichmentEnabled, nil
}

// fakeLLMProviderRowGetter is a no-DB double for llmProviderRowGetter,
// letting the Disconnect handler's "fetch the row before deleting it"
// precondition be observed without a real database - mirrors
// fakeEmailProviderRowGetter in email_providers_handler_test.go.
type fakeLLMProviderRowGetter struct {
	row      *db.LLMProvider
	err      error
	getCalls []string
}

func (f *fakeLLMProviderRowGetter) Get(ctx context.Context, provider string) (*db.LLMProvider, error) {
	f.getCalls = append(f.getCalls, provider)
	if f.err != nil {
		return nil, f.err
	}
	return f.row, nil
}

func newLLMProvidersRouter(svc llmProviderService) http.Handler {
	h := NewLLMProvidersHandler(svc, nil, nil, zap.NewNop())
	r := chi.NewRouter()
	r.Post("/api/integrations/llm/{provider}", h.Connect)
	r.Post("/api/integrations/llm/{provider}/model", h.SetModel)
	r.Post("/api/integrations/llm/{provider}/activate", h.Activate)
	r.Get("/api/integrations/llm", h.List)
	r.Patch("/api/integrations/llm/settings", h.UpdateSettings)
	return r
}

// doLLMUpdateSettingsRequest posts an arbitrary raw JSON body to PATCH
// /api/integrations/llm/settings.
func doLLMUpdateSettingsRequest(t *testing.T, r http.Handler, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/integrations/llm/settings", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// newLLMProvidersRouterWithRows extends newLLMProvidersRouter with a wired
// rows getter and the DELETE disconnect route, for tests that need to
// assert the handler's "capture the row before deleting" precondition.
func newLLMProvidersRouterWithRows(svc llmProviderService, rows llmProviderRowGetter) http.Handler {
	h := NewLLMProvidersHandler(svc, rows, nil, zap.NewNop())
	r := chi.NewRouter()
	r.Post("/api/integrations/llm/{provider}", h.Connect)
	r.Post("/api/integrations/llm/{provider}/model", h.SetModel)
	r.Post("/api/integrations/llm/{provider}/activate", h.Activate)
	r.Get("/api/integrations/llm", h.List)
	r.Delete("/api/integrations/llm/{provider}", h.Disconnect)
	return r
}

func doLLMConnectRequest(t *testing.T, r http.Handler, provider string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/"+provider, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestLLMConnect_UnknownProvider_404 covers AI-01's known-provider allowlist:
// a {provider} path segment other than "openai" is rejected before the
// service is ever called.
func TestLLMConnect_UnknownProvider_404(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "k"})
	rec := doLLMConnectRequest(t, r, "anthropic", body)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.connectCalls) != 0 {
		t.Errorf("Connect called %d times, want 0 for an unknown provider", len(fake.connectCalls))
	}
}

// TestLLMConnect_MalformedJSON_422 covers the malformed-body edge case.
func TestLLMConnect_MalformedJSON_422(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	rec := doLLMConnectRequest(t, r, "openai", []byte("{not json"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(fake.connectCalls) != 0 {
		t.Errorf("Connect called %d times, want 0 for malformed JSON", len(fake.connectCalls))
	}
}

// TestLLMConnect_InvalidInput_422 covers AI-02: Service.Connect returning
// ErrInvalidInput maps to 422.
func TestLLMConnect_InvalidInput_422(t *testing.T) {
	fake := &fakeLLMProviderService{connectErr: llm.ErrInvalidInput}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": ""})
	rec := doLLMConnectRequest(t, r, "openai", body)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestLLMConnect_ValidationFailed_422 covers AI-02: a provider rejecting the
// submitted key (ErrValidationFailed) maps to 422, and the response body
// must never contain the submitted api_key.
func TestLLMConnect_ValidationFailed_422(t *testing.T) {
	fake := &fakeLLMProviderService{connectErr: llm.ErrValidationFailed}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "super-secret-key"})
	rec := doLLMConnectRequest(t, r, "openai", body)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-key") {
		t.Errorf("response body echoes the submitted api_key: %s", rec.Body.String())
	}
}

// TestLLMConnect_Success_201 covers AI-01/AI-03: on validation success the
// handler responds 201, calls Connect with exactly the submitted fields, and
// the response body never contains the submitted api_key.
func TestLLMConnect_Success_201(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "super-secret-key", "model": "gpt-4o-mini"})
	rec := doLLMConnectRequest(t, r, "openai", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-key") {
		t.Errorf("response body echoes the submitted api_key: %s", rec.Body.String())
	}
	if len(fake.connectCalls) != 1 {
		t.Fatalf("Connect called %d times, want 1", len(fake.connectCalls))
	}
	got := fake.connectCalls[0]
	want := connectLLMCall{provider: "openai", apiKey: "super-secret-key", model: "gpt-4o-mini"}
	if got != want {
		t.Errorf("Connect called with %+v, want %+v", got, want)
	}
}

// TestLLMSetModel_UnknownProvider_404 covers the shared unknown-provider
// edge case for the set-model route.
func TestLLMSetModel_UnknownProvider_404(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"model": "gpt-4o"})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/anthropic/model", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.setModelCalls) != 0 {
		t.Errorf("SetModel called %d times, want 0 for an unknown provider", len(fake.setModelCalls))
	}
}

// TestLLMSetModel_UnknownModel_422 covers AI-04: a model outside the
// provider's allowlist responds 422.
func TestLLMSetModel_UnknownModel_422(t *testing.T) {
	fake := &fakeLLMProviderService{setModelErr: llm.ErrUnknownModel}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"model": "not-a-real-model"})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/openai/model", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestLLMSetModel_NotConnected_422 covers SetModel on an unconnected
// provider.
func TestLLMSetModel_NotConnected_422(t *testing.T) {
	fake := &fakeLLMProviderService{setModelErr: llm.ErrProviderNotConnected}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"model": "gpt-4o"})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/openai/model", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestLLMSetModel_Success_200 covers AI-04's happy path.
func TestLLMSetModel_Success_200(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"model": "gpt-4o"})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/openai/model", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(fake.setModelCalls) != 1 || fake.setModelCalls[0] != (setModelCall{"openai", "gpt-4o"}) {
		t.Errorf("setModelCalls = %v, want [{openai gpt-4o}]", fake.setModelCalls)
	}
}

// TestLLMActivate_UnknownProvider_404 covers the shared unknown-provider
// edge case for the activate route.
func TestLLMActivate_UnknownProvider_404(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/anthropic/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.activateCalls) != 0 {
		t.Errorf("Activate called %d times, want 0 for an unknown provider", len(fake.activateCalls))
	}
}

// TestLLMActivate_NotConnected_422 covers AI-06: activating a provider with
// no connected row responds 422.
func TestLLMActivate_NotConnected_422(t *testing.T) {
	fake := &fakeLLMProviderService{activateErr: llm.ErrProviderNotConnected}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/openai/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestLLMActivate_Success_200 covers AI-06's happy path.
func TestLLMActivate_Success_200(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/llm/openai/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(fake.activateCalls) != 1 || fake.activateCalls[0] != "openai" {
		t.Errorf("activateCalls = %v, want [\"openai\"]", fake.activateCalls)
	}
}

// TestLLMList_Empty_NoActiveProvider covers an empty provider list and a
// null active_provider when nothing has ever been connected - never a 404.
func TestLLMList_Empty_NoActiveProvider(t *testing.T) {
	fake := &fakeLLMProviderService{listResult: llm.ListResult{ActiveProvider: "", Providers: nil}}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/llm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp listLLMProvidersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.ActiveProvider != nil {
		t.Errorf("active_provider = %q, want null", *resp.ActiveProvider)
	}
	if len(resp.Providers) != 0 {
		t.Errorf("providers = %v, want empty", resp.Providers)
	}
	if !strings.Contains(rec.Body.String(), `"providers":[]`) {
		t.Errorf("body = %s, want providers serialized as an empty array, not null", rec.Body.String())
	}
}

// TestLLMList_WithProviders_ShapeAndNoKeyMaterial covers the paginated
// envelope, the active provider being reported, and no key material
// (encrypted or otherwise) appearing anywhere in the response body.
func TestLLMList_WithProviders_ShapeAndNoKeyMaterial(t *testing.T) {
	checkedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lastErr := "boom"
	fake := &fakeLLMProviderService{listResult: llm.ListResult{
		ActiveProvider: "openai",
		Providers: []llm.ProviderStatus{
			{Provider: "openai", Model: "gpt-4o-mini", Status: "connected", LastCheckedAt: &checkedAt},
		},
		Total:    1,
		Page:     1,
		PageSize: llmProvidersPageSize,
	}}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/llm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp listLLMProvidersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.ActiveProvider == nil || *resp.ActiveProvider != "openai" {
		t.Errorf("active_provider = %v, want \"openai\"", resp.ActiveProvider)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].Model != "gpt-4o-mini" || resp.Providers[0].Status != "connected" {
		t.Errorf("providers = %+v, want [openai/gpt-4o-mini/connected]", resp.Providers)
	}
	if resp.Providers[0].LastCheckedAt == nil || *resp.Providers[0].LastCheckedAt != checkedAt.Format(time.RFC3339) {
		t.Errorf("providers[0].LastCheckedAt = %v, want %q", resp.Providers[0].LastCheckedAt, checkedAt.Format(time.RFC3339))
	}
	if strings.Contains(rec.Body.String(), lastErr) {
		t.Errorf("response body unexpectedly contains last_error %q", lastErr)
	}
	if strings.Contains(rec.Body.String(), "api_key") || strings.Contains(rec.Body.String(), "encrypted") {
		t.Errorf("response body may contain key material: %s", rec.Body.String())
	}
	if resp.Total != 1 || resp.Page != 1 || resp.PageSize != llmProvidersPageSize {
		t.Errorf("pagination envelope = total:%d page:%d page_size:%d, want 1/1/%d", resp.Total, resp.Page, resp.PageSize, llmProvidersPageSize)
	}
}

func doLLMDisconnectRequest(t *testing.T, r http.Handler, provider string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/integrations/llm/"+provider, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestLLMDisconnect_UnknownProvider_404 covers PROVDISC-04 AC3: a
// {provider} path segment other than "openai" is rejected before the
// service is ever called, same unknown-provider body Connect/Activate use.
func TestLLMDisconnect_UnknownProvider_404(t *testing.T) {
	fake := &fakeLLMProviderService{}
	rows := &fakeLLMProviderRowGetter{err: db.ErrNotFound}
	r := newLLMProvidersRouterWithRows(fake, rows)

	rec := doLLMDisconnectRequest(t, r, "anthropic")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 0 {
		t.Errorf("Disconnect called %d times, want 0 for an unknown provider", len(fake.disconnectCalls))
	}
}

// TestLLMDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete covers
// PROVDISC-04 AC1/PROVDISC-06 AC5: disconnecting a connected provider
// responds 204 and calls Disconnect with the path's provider. It also
// confirms the "capture before mutate" precondition the audit entry
// depends on - actual admin_audit_log insertion is covered by
// TestDisconnectLLMProvider_ValidRequest_RecordsLLMProviderDisconnectedAudit
// (T8, real DB), same split as Connect/Activate's own audit assertions.
func TestLLMDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete(t *testing.T) {
	fake := &fakeLLMProviderService{}
	rows := &fakeLLMProviderRowGetter{row: &db.LLMProvider{ID: "row-1", Provider: "openai"}}
	r := newLLMProvidersRouterWithRows(fake, rows)

	rec := doLLMDisconnectRequest(t, r, "openai")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 1 || fake.disconnectCalls[0] != "openai" {
		t.Errorf("disconnectCalls = %v, want [\"openai\"]", fake.disconnectCalls)
	}
	if len(rows.getCalls) != 1 || rows.getCalls[0] != "openai" {
		t.Errorf("rows.Get calls = %v, want [\"openai\"] (row must be captured before delete for the audit entry)", rows.getCalls)
	}
}

// TestLLMDisconnect_NeverConnected_204Idempotent covers PROVDISC-05 AC4: a
// recognized provider with no connected row still responds 204 (idempotent
// delete), not 404.
func TestLLMDisconnect_NeverConnected_204Idempotent(t *testing.T) {
	fake := &fakeLLMProviderService{}
	rows := &fakeLLMProviderRowGetter{err: db.ErrNotFound}
	r := newLLMProvidersRouterWithRows(fake, rows)

	rec := doLLMDisconnectRequest(t, r, "openai")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 1 || fake.disconnectCalls[0] != "openai" {
		t.Errorf("disconnectCalls = %v, want [\"openai\"]", fake.disconnectCalls)
	}
}

// TestLLMDisconnect_ServiceError_500 mirrors the email handler's equivalent
// test: an unexpected service-layer failure must not be reported as a
// 204/404, same writeInternalError fallback every other handler uses.
func TestLLMDisconnect_ServiceError_500(t *testing.T) {
	fake := &fakeLLMProviderService{disconnectErr: context.DeadlineExceeded}
	rows := &fakeLLMProviderRowGetter{row: &db.LLMProvider{ID: "row-1", Provider: "openai"}}
	r := newLLMProvidersRouterWithRows(fake, rows)

	rec := doLLMDisconnectRequest(t, r, "openai")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestLLMList_ServiceError_500 covers the un-listed but structurally
// required error path: an unexpected repository failure must not leak as a
// 200/422, mirroring every other handler's writeInternalError fallback.
func TestLLMList_ServiceError_500(t *testing.T) {
	fake := &fakeLLMProviderService{listErr: context.DeadlineExceeded}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/llm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestLLMList_IncludesRootCauseEnrichmentEnabled covers RCA-08: List's
// response must reflect the current toggle value, not just its list
// endpoint's original fields.
func TestLLMList_IncludesRootCauseEnrichmentEnabled(t *testing.T) {
	fake := &fakeLLMProviderService{rootCauseEnrichmentEnabled: true}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/llm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp listLLMProvidersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if !resp.RootCauseEnrichmentEnabled {
		t.Errorf("root_cause_enrichment_enabled = false, want true")
	}
}

// TestLLMList_RootCauseEnrichmentEnabledError_500 mirrors
// TestLLMList_ServiceError_500 for the settings-read failure path.
func TestLLMList_RootCauseEnrichmentEnabledError_500(t *testing.T) {
	fake := &fakeLLMProviderService{getRootCauseErr: context.DeadlineExceeded}
	r := newLLMProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/llm", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestLLMUpdateSettings_ValidRequest_200Toggles covers RCA-07/RCA-09's
// happy path: a well-formed body calls through to
// SetRootCauseEnrichmentEnabled with the submitted value and echoes it
// back at 200.
func TestLLMUpdateSettings_ValidRequest_200Toggles(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	rec := doLLMUpdateSettingsRequest(t, r, `{"root_cause_enrichment_enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if len(fake.setRootCauseCalls) != 1 || !fake.setRootCauseCalls[0] {
		t.Fatalf("setRootCauseCalls = %v, want [true]", fake.setRootCauseCalls)
	}

	var resp updateLLMSettingsRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if !resp.RootCauseEnrichmentEnabled {
		t.Errorf("response root_cause_enrichment_enabled = false, want true")
	}
}

// TestLLMUpdateSettings_MalformedBody_422 covers RCA-09: an invalid JSON
// body is rejected before the service is ever called.
func TestLLMUpdateSettings_MalformedBody_422(t *testing.T) {
	fake := &fakeLLMProviderService{}
	r := newLLMProvidersRouter(fake)

	rec := doLLMUpdateSettingsRequest(t, r, `{"root_cause_enrichment_enabled":`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(fake.setRootCauseCalls) != 0 {
		t.Errorf("setRootCauseCalls = %v, want none (malformed body must never reach the service)", fake.setRootCauseCalls)
	}
}

// TestLLMUpdateSettings_ServiceError_500 mirrors every other handler's
// writeInternalError fallback: an unexpected repository failure must not
// leak as a 200/422.
func TestLLMUpdateSettings_ServiceError_500(t *testing.T) {
	fake := &fakeLLMProviderService{setRootCauseErr: context.DeadlineExceeded}
	r := newLLMProvidersRouter(fake)

	rec := doLLMUpdateSettingsRequest(t, r, `{"root_cause_enrichment_enabled":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
