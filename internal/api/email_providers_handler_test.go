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
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// fakeEmailProviderService is a no-DB, no-HTTP double for emailProviderService,
// letting this handler suite run as a fast unit test independent of
// email.Service's real dependencies (repository, provider factory).
type fakeEmailProviderService struct {
	connectErr    error
	activateErr   error
	listResult    email.ListResult
	listErr       error
	disconnectErr error

	connectCalls    []connectCall
	activateCalls   []string
	disconnectCalls []string
}

type connectCall struct {
	provider, apiKey, fromEmail, fromName string
}

func (f *fakeEmailProviderService) Connect(ctx context.Context, provider, apiKey, fromEmail, fromName string) error {
	f.connectCalls = append(f.connectCalls, connectCall{provider, apiKey, fromEmail, fromName})
	return f.connectErr
}

func (f *fakeEmailProviderService) Activate(ctx context.Context, provider string) error {
	f.activateCalls = append(f.activateCalls, provider)
	return f.activateErr
}

func (f *fakeEmailProviderService) List(ctx context.Context, page, pageSize int) (email.ListResult, error) {
	return f.listResult, f.listErr
}

func (f *fakeEmailProviderService) Disconnect(ctx context.Context, provider string) error {
	f.disconnectCalls = append(f.disconnectCalls, provider)
	return f.disconnectErr
}

// fakeEmailProviderRowGetter is a no-DB double for emailProviderRowGetter,
// letting the Disconnect handler's "fetch the row before deleting it"
// precondition be observed without a real database - Disconnect gates its
// record-audit branch on whether this Get call succeeds.
type fakeEmailProviderRowGetter struct {
	row      *db.EmailProvider
	err      error
	getCalls []string
}

func (f *fakeEmailProviderRowGetter) Get(ctx context.Context, provider string) (*db.EmailProvider, error) {
	f.getCalls = append(f.getCalls, provider)
	if f.err != nil {
		return nil, f.err
	}
	return f.row, nil
}

// newEmailProvidersRouterWithRows extends newEmailProvidersRouter with a
// wired rows getter and the DELETE disconnect route, for tests that need to
// assert the handler's "capture the row before deleting" precondition.
func newEmailProvidersRouterWithRows(svc emailProviderService, rows emailProviderRowGetter) http.Handler {
	h := NewEmailProvidersHandler(svc, rows, nil, zap.NewNop())
	r := chi.NewRouter()
	r.Post("/api/integrations/email/{provider}", h.Connect)
	r.Get("/api/integrations/email", h.List)
	r.Post("/api/integrations/email/{provider}/activate", h.Activate)
	r.Delete("/api/integrations/email/{provider}", h.Disconnect)
	return r
}

// newEmailProvidersRouter wires no UserFromContext actor into the request
// (no auth middleware here, unlike the integration test suite), so audit
// recording's actor lookup never succeeds - nil rows/auditLog are safe,
// since the handler only dereferences them inside that same guarded branch.
func newEmailProvidersRouter(svc emailProviderService) http.Handler {
	h := NewEmailProvidersHandler(svc, nil, nil, zap.NewNop())
	r := chi.NewRouter()
	r.Post("/api/integrations/email/{provider}", h.Connect)
	r.Get("/api/integrations/email", h.List)
	r.Post("/api/integrations/email/{provider}/activate", h.Activate)
	return r
}

func doConnectRequest(t *testing.T, r http.Handler, provider string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/"+provider, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestConnect_UnknownProvider_404 covers EMAIL-01 AC4: a {provider} path
// segment other than sendgrid/resend is rejected before the service is
// ever called.
func TestConnect_UnknownProvider_404(t *testing.T) {
	fake := &fakeEmailProviderService{}
	r := newEmailProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "k", "from_email": "a@b.com", "from_name": "A"})
	rec := doConnectRequest(t, r, "mailgun", body)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.connectCalls) != 0 {
		t.Errorf("Connect called %d times, want 0 for an unknown provider", len(fake.connectCalls))
	}
}

// TestConnect_MalformedJSON_422 covers the edge case: malformed request
// body responds 422 (matches ConnectDatadog's existing decode-failure
// handling per spec's Edge Cases).
func TestConnect_MalformedJSON_422(t *testing.T) {
	fake := &fakeEmailProviderService{}
	r := newEmailProvidersRouter(fake)

	rec := doConnectRequest(t, r, "sendgrid", []byte("{not json"))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(fake.connectCalls) != 0 {
		t.Errorf("Connect called %d times, want 0 for malformed JSON", len(fake.connectCalls))
	}
}

// TestConnect_InvalidInput_422 covers EMAIL-01 AC2 (missing/invalid input):
// Service.Connect returning ErrInvalidInput maps to 422.
func TestConnect_InvalidInput_422(t *testing.T) {
	fake := &fakeEmailProviderService{connectErr: email.ErrInvalidInput}
	r := newEmailProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "", "from_email": "not-an-email", "from_name": ""})
	rec := doConnectRequest(t, r, "sendgrid", body)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestConnect_ValidationFailed_422 covers EMAIL-01 AC1/AC2: a provider
// rejecting the submitted key (ErrValidationFailed) maps to 422, and the
// response body must never contain the submitted api_key (EMAIL-01 AC5).
func TestConnect_ValidationFailed_422(t *testing.T) {
	fake := &fakeEmailProviderService{connectErr: email.ErrValidationFailed}
	r := newEmailProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "super-secret-key", "from_email": "a@b.com", "from_name": "A"})
	rec := doConnectRequest(t, r, "sendgrid", body)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-key") {
		t.Errorf("response body echoes the submitted api_key: %s", rec.Body.String())
	}
}

// TestConnect_Success_201 covers EMAIL-01 AC1/AC3: on validation success the
// handler responds 201 and calls Connect with exactly the submitted fields,
// and the response body never contains the submitted api_key (EMAIL-01
// AC5).
func TestConnect_Success_201(t *testing.T) {
	fake := &fakeEmailProviderService{}
	r := newEmailProvidersRouter(fake)

	body, _ := json.Marshal(map[string]string{"api_key": "super-secret-key", "from_email": "a@b.com", "from_name": "Acme"})
	rec := doConnectRequest(t, r, "sendgrid", body)

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
	want := connectCall{provider: "sendgrid", apiKey: "super-secret-key", fromEmail: "a@b.com", fromName: "Acme"}
	if got != want {
		t.Errorf("Connect called with %+v, want %+v", got, want)
	}
}

// TestActivate_UnknownProvider_404 covers the shared unknown-provider edge
// case for the activate route.
func TestActivate_UnknownProvider_404(t *testing.T) {
	fake := &fakeEmailProviderService{}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/mailgun/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.activateCalls) != 0 {
		t.Errorf("Activate called %d times, want 0 for an unknown provider", len(fake.activateCalls))
	}
}

// TestActivate_NotConnected_422 covers EMAIL-04 AC2/EMAIL-05: activating a
// provider with no connected row responds 422.
func TestActivate_NotConnected_422(t *testing.T) {
	fake := &fakeEmailProviderService{activateErr: email.ErrProviderNotConnected}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/resend/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestActivate_Success_200 covers EMAIL-04 AC1: activating a connected
// provider responds 200 and calls Activate with the path's provider.
func TestActivate_Success_200(t *testing.T) {
	fake := &fakeEmailProviderService{}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/email/resend/activate", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(fake.activateCalls) != 1 || fake.activateCalls[0] != "resend" {
		t.Errorf("activateCalls = %v, want [\"resend\"]", fake.activateCalls)
	}
}

// TestList_Empty_NoActiveProvider covers EMAIL-06 AC2/EMAIL-04 AC4: an
// empty provider list and a null active_provider when nothing has ever
// been connected - never a 404.
func TestList_Empty_NoActiveProvider(t *testing.T) {
	fake := &fakeEmailProviderService{listResult: email.ListResult{ActiveProvider: "", Providers: nil}}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/email", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp listEmailProvidersResponse
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

// TestList_WithProviders_ShapeAndNoKeyMaterial covers EMAIL-06 AC1: every
// connected provider's public fields are returned, the active provider is
// reported, and no key material (encrypted or otherwise) appears anywhere
// in the response body (EMAIL-06 AC1, EMAIL-01 AC5's "no api_key in any
// response body" guarantee applied to List too).
func TestList_WithProviders_ShapeAndNoKeyMaterial(t *testing.T) {
	checkedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lastErr := "boom"
	fake := &fakeEmailProviderService{listResult: email.ListResult{
		ActiveProvider: "resend",
		Providers: []email.ProviderStatus{
			{Provider: "sendgrid", Status: "connected", FromEmail: "a@b.com", FromName: "A", LastCheckedAt: &checkedAt},
			{Provider: "resend", Status: "invalid", FromEmail: "c@d.com", FromName: "C", LastError: &lastErr},
		},
	}}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/email", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp listEmailProvidersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.ActiveProvider == nil || *resp.ActiveProvider != "resend" {
		t.Errorf("active_provider = %v, want \"resend\"", resp.ActiveProvider)
	}
	if len(resp.Providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(resp.Providers))
	}
	if resp.Providers[0].Provider != "sendgrid" || resp.Providers[0].Status != "connected" || resp.Providers[0].FromEmail != "a@b.com" || resp.Providers[0].FromName != "A" {
		t.Errorf("providers[0] = %+v, want sendgrid/connected/a@b.com/A", resp.Providers[0])
	}
	if resp.Providers[0].LastCheckedAt == nil || *resp.Providers[0].LastCheckedAt != checkedAt.Format(time.RFC3339) {
		t.Errorf("providers[0].LastCheckedAt = %v, want %q", resp.Providers[0].LastCheckedAt, checkedAt.Format(time.RFC3339))
	}
	if resp.Providers[1].Status != "invalid" || resp.Providers[1].LastError == nil || *resp.Providers[1].LastError != "boom" {
		t.Errorf("providers[1] = %+v, want status=invalid, last_error=boom", resp.Providers[1])
	}
	if strings.Contains(rec.Body.String(), "api_key") || strings.Contains(rec.Body.String(), "encrypted") {
		t.Errorf("response body may contain key material: %s", rec.Body.String())
	}
}

// TestList_ServiceError_500 covers the un-listed but structurally required
// error path: an unexpected repository failure must not leak as a 200/422,
// mirroring every other handler's writeInternalError fallback in this
// package.
func TestList_ServiceError_500(t *testing.T) {
	fake := &fakeEmailProviderService{listErr: context.DeadlineExceeded}
	r := newEmailProvidersRouter(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/email", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func doDisconnectRequest(t *testing.T, r http.Handler, provider string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/integrations/email/"+provider, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestDisconnect_UnknownProvider_404 covers PROVDISC-01 AC4: a {provider}
// path segment other than sendgrid/resend is rejected before the service is
// ever called, same unknown-provider body Connect/Activate use.
func TestDisconnect_UnknownProvider_404(t *testing.T) {
	fake := &fakeEmailProviderService{}
	rows := &fakeEmailProviderRowGetter{err: db.ErrNotFound}
	r := newEmailProvidersRouterWithRows(fake, rows)

	rec := doDisconnectRequest(t, r, "mailgun")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 0 {
		t.Errorf("Disconnect called %d times, want 0 for an unknown provider", len(fake.disconnectCalls))
	}
}

// TestDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete covers
// PROVDISC-01 AC1/AC6: disconnecting a connected provider responds 204 and
// calls Disconnect with the path's provider. It also confirms the "capture
// before mutate" precondition the audit entry depends on: h.rows.Get is
// called (so the row's id/display name is available before the delete would
// make it unresolvable) - actual admin_audit_log insertion is covered by
// TestDisconnectEmailProvider_ValidRequest_RecordsEmailProviderDisconnectedAudit
// (T6, real DB), same split as Connect/Activate's own audit assertions.
func TestDisconnect_ConnectedProvider_204AndCapturesRowBeforeDelete(t *testing.T) {
	fake := &fakeEmailProviderService{}
	rows := &fakeEmailProviderRowGetter{row: &db.EmailProvider{ID: "row-1", Provider: "resend"}}
	r := newEmailProvidersRouterWithRows(fake, rows)

	rec := doDisconnectRequest(t, r, "resend")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 1 || fake.disconnectCalls[0] != "resend" {
		t.Errorf("disconnectCalls = %v, want [\"resend\"]", fake.disconnectCalls)
	}
	if len(rows.getCalls) != 1 || rows.getCalls[0] != "resend" {
		t.Errorf("rows.Get calls = %v, want [\"resend\"] (row must be captured before delete for the audit entry)", rows.getCalls)
	}
}

// TestDisconnect_NeverConnected_204Idempotent covers PROVDISC-02 AC5: a
// recognized provider with no connected row still responds 204 (idempotent
// delete), not 404 - and Disconnect is still called (the repository-level
// delete of zero rows is itself the idempotent no-op, not a handler-level
// short-circuit).
func TestDisconnect_NeverConnected_204Idempotent(t *testing.T) {
	fake := &fakeEmailProviderService{}
	rows := &fakeEmailProviderRowGetter{err: db.ErrNotFound}
	r := newEmailProvidersRouterWithRows(fake, rows)

	rec := doDisconnectRequest(t, r, "sendgrid")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if len(fake.disconnectCalls) != 1 || fake.disconnectCalls[0] != "sendgrid" {
		t.Errorf("disconnectCalls = %v, want [\"sendgrid\"]", fake.disconnectCalls)
	}
}

// TestDisconnect_ServiceError_500 mirrors TestList_ServiceError_500: an
// unexpected service-layer failure must not be reported as a 204/404,
// same writeInternalError fallback every other handler in this package uses.
func TestDisconnect_ServiceError_500(t *testing.T) {
	fake := &fakeEmailProviderService{disconnectErr: context.DeadlineExceeded}
	rows := &fakeEmailProviderRowGetter{row: &db.EmailProvider{ID: "row-1", Provider: "resend"}}
	r := newEmailProvidersRouterWithRows(fake, rows)

	rec := doDisconnectRequest(t, r, "resend")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
