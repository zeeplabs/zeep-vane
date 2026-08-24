//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

const testMasterKey = "integrations-handler-test-master-key"

// alwaysEmptySearch is the default search stub for tests not exercising
// SearchSLOs itself.
func alwaysEmptySearch(ctx context.Context, apiKey, appKey, query string) ([]datadog.SLOSummary, error) {
	return nil, nil
}

func newIntegrationsRouter(t *testing.T, validate validateDatadogCredentials, logger *zap.Logger) (http.Handler, *db.Pool, *db.AdminRepository) {
	return newIntegrationsRouterWithSearch(t, validate, alwaysEmptySearch, logger)
}

func newIntegrationsRouterWithSearch(t *testing.T, validate validateDatadogCredentials, search searchDatadogSLOs, logger *zap.Logger) (http.Handler, *db.Pool, *db.AdminRepository) {
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
	// Use context.Background() here, not the bounded ctx above: this lock
	// may need to wait for another concurrently-run package's test to
	// release the same advisory key, and a 5s setup deadline is
	// unrelated to (and shorter than) how long that wait can legitimately
	// take. Mirrors the pattern already used by
	// public_status_handler_test.go and poller_test.go's own
	// LockDatadogIntegration calls.
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	// RequireAuth's admin lookup runs against its own dedicated pool,
	// separate from pool above: TestConnectDatadog_ResponseAndLogs_
	// NeverContainPlaintextKey deliberately closes pool mid-test to force
	// the integrations repository to fail downstream of auth, and that
	// must not also break the auth lookup itself.
	authPool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() for auth returned unexpected error: %v", err)
	}
	t.Cleanup(authPool.Close)
	admins := db.NewAdminRepository(authPool)

	repo := db.NewIntegrationRepository(pool)
	handler := NewIntegrationsHandler(repo, validate, search, &spyPollerRestarter{}, testMasterKey, logger)

	r := chi.NewRouter()
	r.With(RequireAuth(middlewareTestSecret, admins)).Post("/api/integrations/datadog", handler.ConnectDatadog)
	r.With(RequireAuth(middlewareTestSecret, admins)).Get("/api/integrations/datadog/status", handler.Status)
	r.With(RequireAuth(middlewareTestSecret, admins)).Get("/api/integrations/datadog/slos", handler.SearchSLOs)

	return r, pool, admins
}

// newIntegrationsRouterWithPoller mirrors newIntegrationsRouterWithSearch
// but takes the caller's own pollerRestarter, letting tests observe or
// fail the poller (re)start triggered by a successful connect (PLD-01,
// PLD-06).
func newIntegrationsRouterWithPoller(t *testing.T, validate validateDatadogCredentials, poller pollerRestarter, logger *zap.Logger) (http.Handler, *db.Pool, *db.AdminRepository) {
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
	// See newIntegrationsRouterWithSearch's identical comment: use
	// context.Background() for the lock wait, not the bounded setup ctx.
	dbtest.LockDatadogIntegration(t, context.Background(), dsn)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })

	authPool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() for auth returned unexpected error: %v", err)
	}
	t.Cleanup(authPool.Close)
	admins := db.NewAdminRepository(authPool)

	repo := db.NewIntegrationRepository(pool)
	handler := NewIntegrationsHandler(repo, validate, alwaysEmptySearch, poller, testMasterKey, logger)

	r := chi.NewRouter()
	r.With(RequireAuth(middlewareTestSecret, admins)).Post("/api/integrations/datadog", handler.ConnectDatadog)

	return r, pool, admins
}

// spyPollerRestarter is the default pollerRestarter used by tests that
// don't care about the poller (re)start side effect - it always succeeds
// and does nothing else, matching the pre-fix behavior for tests unrelated
// to PLD-01/PLD-06.
type spyPollerRestarter struct {
	calls int
	err   error
}

func (s *spyPollerRestarter) Restart(ctx context.Context) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return true, nil
}

func getDatadogStatus(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/integrations/datadog/status", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func postConnectDatadog(t *testing.T, r http.Handler, token, apiKey, appKey string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(connectDatadogRequest{APIKey: apiKey, AppKey: appKey})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/datadog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	return rec
}

func fetchIntegrationRow(t *testing.T, pool *db.Pool) (encryptedAPIKey, encryptedAppKey []byte, found bool) {
	t.Helper()
	row := pool.QueryRow(context.Background(),
		"SELECT encrypted_api_key, encrypted_app_key FROM integrations WHERE provider = 'datadog'")
	if err := row.Scan(&encryptedAPIKey, &encryptedAppKey); err != nil {
		return nil, nil, false
	}
	return encryptedAPIKey, encryptedAppKey, true
}

func TestConnectDatadog_ValidCredentials_201SavesEncrypted(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, pool, admins := newIntegrationsRouter(t, alwaysValid, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	rec := postConnectDatadog(t, r, token, "real-api-key", "real-app-key")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	encAPIKey, encAppKey, found := fetchIntegrationRow(t, pool)
	if !found {
		t.Fatal("no integrations row found after successful connect, want a saved row")
	}

	gotAPIKey, err := crypto.Decrypt(testMasterKey, encAPIKey)
	if err != nil {
		t.Fatalf("Decrypt(stored api key) returned unexpected error: %v", err)
	}
	if string(gotAPIKey) != "real-api-key" {
		t.Errorf("decrypted stored api key = %q, want %q", gotAPIKey, "real-api-key")
	}

	gotAppKey, err := crypto.Decrypt(testMasterKey, encAppKey)
	if err != nil {
		t.Fatalf("Decrypt(stored app key) returned unexpected error: %v", err)
	}
	if string(gotAppKey) != "real-app-key" {
		t.Errorf("decrypted stored app key = %q, want %q", gotAppKey, "real-app-key")
	}

	if bytes.Contains(encAPIKey, []byte("real-api-key")) {
		t.Error("stored encrypted_api_key contains the plaintext key, want ciphertext only")
	}
}

func TestConnectDatadog_InvalidCredentials_422NothingSaved(t *testing.T) {
	alwaysInvalid := func(ctx context.Context, apiKey, appKey string) error { return datadog.ErrUnauthorized }
	r, pool, admins := newIntegrationsRouter(t, alwaysInvalid, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	rec := postConnectDatadog(t, r, token, "bad-api-key", "bad-app-key")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	_, _, found := fetchIntegrationRow(t, pool)
	if found {
		t.Error("integrations row found after rejected connect, want nothing persisted (SP-01.2)")
	}
}

func TestConnectDatadog_NoAuth_401(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, _, _ := newIntegrationsRouter(t, alwaysValid, zap.NewNop())

	rec := postConnectDatadog(t, r, "", "any-api-key", "any-app-key")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestConnectDatadog_ResponseAndLogs_NeverContainPlaintextKey(t *testing.T) {
	// Force a downstream error (persistence failure) after validation
	// succeeds, so the handler's error-logging path runs, and confirm even
	// then the raw key never reaches the logger or the response body
	// (SP-01.4).
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, pool, admins := newIntegrationsRouter(t, alwaysValid, logger)
	pool.Close() // force UpsertDatadog to fail after validation succeeds
	token := issueTestSessionToken(t, admins)

	const secretAPIKey = "super-secret-datadog-api-key"
	rec := postConnectDatadog(t, r, token, secretAPIKey, "app-key")

	if strings.Contains(rec.Body.String(), secretAPIKey) {
		t.Errorf("response body contains the plaintext api key: %s", rec.Body.String())
	}

	if len(logs.All()) == 0 {
		t.Fatal("no error log entry recorded, want the persistence failure to be logged (so this check is meaningful)")
	}

	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, secretAPIKey) {
			t.Errorf("log message contains the plaintext api key: %q", entry.Message)
		}
		for key, value := range entry.ContextMap() {
			if strings.Contains(fmt.Sprintf("%v", value), secretAPIKey) {
				t.Errorf("log field %q contains the plaintext api key", key)
			}
		}
	}
}

func TestDatadogStatus_NotConnectedYet_404(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, _, admins := newIntegrationsRouter(t, alwaysValid, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	rec := getDatadogStatus(t, r, token)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDatadogStatus_NoAuth_401(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, _, _ := newIntegrationsRouter(t, alwaysValid, zap.NewNop())

	rec := getDatadogStatus(t, r, "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestDatadogStatus_AfterConnectionFailure_ReportsInvalidAndReason covers
// T24's admin-facing side (SP-09): once the poller has recorded a
// connection failure via MarkDatadogInvalid, the status endpoint must
// surface both the "invalid" status and the recorded reason to the admin.
func TestDatadogStatus_AfterConnectionFailure_ReportsInvalidAndReason(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, pool, admins := newIntegrationsRouter(t, alwaysValid, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	postConnectDatadog(t, r, token, "real-api-key", "real-app-key")

	repo := db.NewIntegrationRepository(pool)
	const reason = "datadog: request timed out"
	if err := repo.MarkDatadogInvalid(context.Background(), reason); err != nil {
		t.Fatalf("MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	rec := getDatadogStatus(t, r, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp datadogStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}

	if resp.Status != "invalid" {
		t.Errorf("Status = %q, want %q", resp.Status, "invalid")
	}
	if resp.LastError == nil || *resp.LastError != reason {
		t.Errorf("LastError = %v, want %q", resp.LastError, reason)
	}
}

func getDatadogSLOs(t *testing.T, r http.Handler, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/integrations/datadog/slos?query="+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSearchDatadogSLOs_Connected_200ReturnsList(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	search := func(ctx context.Context, apiKey, appKey, query string) ([]datadog.SLOSummary, error) {
		if apiKey != "real-api-key" || appKey != "real-app-key" {
			t.Errorf("search called with apiKey=%q appKey=%q, want the stored, decrypted keys", apiKey, appKey)
		}
		if query != "checkout" {
			t.Errorf("search called with query=%q, want %q", query, "checkout")
		}
		return []datadog.SLOSummary{{ID: "slo-1", Name: "Checkout latência p95"}}, nil
	}
	r, _, admins := newIntegrationsRouterWithSearch(t, alwaysValid, search, zap.NewNop())
	token := issueTestSessionToken(t, admins)
	postConnectDatadog(t, r, token, "real-api-key", "real-app-key")

	rec := getDatadogSLOs(t, r, token, "checkout")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []sloSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if len(resp) != 1 || resp[0].ID != "slo-1" || resp[0].Name != "Checkout latência p95" {
		t.Errorf("resp = %+v, want [{slo-1 Checkout latência p95}]", resp)
	}
}

func TestSearchDatadogSLOs_NotConnectedYet_200EmptyList(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, _, admins := newIntegrationsRouterWithSearch(t, alwaysValid, alwaysEmptySearch, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	rec := getDatadogSLOs(t, r, token, "checkout")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "[]")
	}
}

func TestSearchDatadogSLOs_NoAuth_401(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	r, _, _ := newIntegrationsRouterWithSearch(t, alwaysValid, alwaysEmptySearch, zap.NewNop())

	rec := getDatadogSLOs(t, r, "", "checkout")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// issueTestSessionToken inserts a real admin row via admins (so RequireAuth's
// GetByID lookup succeeds) and issues a session token for it.
func issueTestSessionToken(t *testing.T, admins *db.AdminRepository) string {
	t.Helper()
	ctx := context.Background()
	admin := &db.Admin{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = admins.Delete(context.Background(), admin.ID) })

	token, err := auth.IssueSession(admin.ID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSession() returned unexpected error: %v", err)
	}
	return token
}

// TestConnectDatadog_ValidCredentials_RestartsPoller covers PLD-01: a
// successful connect must (re)start the poller in-process, not merely
// persist the row - the whole point of this fix is that an admin
// connecting Datadog doesn't have to also restart the server.
func TestConnectDatadog_ValidCredentials_RestartsPoller(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	poller := &spyPollerRestarter{}
	r, _, admins := newIntegrationsRouterWithPoller(t, alwaysValid, poller, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	rec := postConnectDatadog(t, r, token, "real-api-key", "real-app-key")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if poller.calls != 1 {
		t.Errorf("poller.Restart() calls = %d, want 1 - a successful connect must (re)start the poller in-process", poller.calls)
	}
}

// TestConnectDatadog_RotateKey_RestartsPollerAgain covers PLD-05 at the
// trigger level: rotating the key goes through the exact same handler
// method as the first connect (there is no separate "rotate" endpoint -
// the admin UI's "Rotacionar chave" button just calls this one again), so
// a second successful call must (re)start the poller a second time too,
// not just the first.
func TestConnectDatadog_RotateKey_RestartsPollerAgain(t *testing.T) {
	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	poller := &spyPollerRestarter{}
	r, _, admins := newIntegrationsRouterWithPoller(t, alwaysValid, poller, zap.NewNop())
	token := issueTestSessionToken(t, admins)

	first := postConnectDatadog(t, r, token, "first-api-key", "first-app-key")
	if first.Code != http.StatusCreated {
		t.Fatalf("first connect status = %d, want %d, body = %s", first.Code, http.StatusCreated, first.Body.String())
	}

	second := postConnectDatadog(t, r, token, "rotated-api-key", "rotated-app-key")
	if second.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d, want %d, body = %s", second.Code, http.StatusCreated, second.Body.String())
	}

	if poller.calls != 2 {
		t.Errorf("poller.Restart() calls = %d, want 2 - rotating the key must (re)start the poller again, same as the first connect", poller.calls)
	}
}

// TestConnectDatadog_PollerRestartFails_StillReturns201 covers PLD-06: the
// persisted row is authoritative for this response - a poller (re)start
// failure (e.g. a decrypt error) must not turn an already-successful
// connect into an error response.
func TestConnectDatadog_PollerRestartFails_StillReturns201(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	alwaysValid := func(ctx context.Context, apiKey, appKey string) error { return nil }
	poller := &spyPollerRestarter{err: fmt.Errorf("poller: simulated restart failure")}
	r, pool, admins := newIntegrationsRouterWithPoller(t, alwaysValid, poller, logger)
	token := issueTestSessionToken(t, admins)

	rec := postConnectDatadog(t, r, token, "real-api-key", "real-app-key")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if _, _, found := fetchIntegrationRow(t, pool); !found {
		t.Error("no integrations row found, want the connect itself to have still persisted despite the poller restart failure")
	}

	found := false
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "restart poller") {
			found = true
		}
	}
	if !found {
		t.Error("no log entry recorded for the poller restart failure, want it logged even though the response still succeeds")
	}
}
