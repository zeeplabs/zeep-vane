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

	"github.com/zeeplabs/zeep-vane/internal/email"
)

// fakeEmailProviderService is a no-DB, no-HTTP double for emailProviderService,
// letting this handler suite run as a fast unit test independent of
// email.Service's real dependencies (repository, provider factory).
type fakeEmailProviderService struct {
	connectErr  error
	activateErr error
	listResult  email.ListResult
	listErr     error

	connectCalls  []connectCall
	activateCalls []string
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

func (f *fakeEmailProviderService) List(ctx context.Context) (email.ListResult, error) {
	return f.listResult, f.listErr
}

func newEmailProvidersRouter(svc emailProviderService) http.Handler {
	h := NewEmailProvidersHandler(svc, zap.NewNop())
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
