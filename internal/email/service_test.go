package email

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

const testMasterKey = "test-master-key"

// fakeStore is an in-memory EmailProviderStore double - no real DB.
type fakeStore struct {
	rows           map[string]*db.EmailProvider
	activeProvider string
	upsertErr      error
	getErr         error
	listErr        error
	getActiveErr   error
	setActiveErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string]*db.EmailProvider{}}
}

func (f *fakeStore) UpsertProvider(_ context.Context, provider string, encryptedAPIKey []byte, fromEmail, fromName string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.rows[provider] = &db.EmailProvider{
		Provider:        provider,
		EncryptedAPIKey: encryptedAPIKey,
		FromEmail:       fromEmail,
		FromName:        fromName,
		Status:          "connected",
	}
	return nil
}

func (f *fakeStore) Get(_ context.Context, provider string) (*db.EmailProvider, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.rows[provider]
	if !ok {
		return nil, db.ErrNotFound
	}
	return row, nil
}

func (f *fakeStore) ListPaginated(_ context.Context, page, pageSize int) ([]db.EmailProvider, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	rows := make([]db.EmailProvider, 0, len(f.rows))
	for _, row := range f.rows {
		rows = append(rows, *row)
	}
	total := len(rows)

	start := (page - 1) * pageSize
	if start >= total {
		return []db.EmailProvider{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return rows[start:end], total, nil
}

func (f *fakeStore) GetActiveProvider(_ context.Context) (string, error) {
	if f.getActiveErr != nil {
		return "", f.getActiveErr
	}
	return f.activeProvider, nil
}

func (f *fakeStore) SetActiveProvider(_ context.Context, provider string) error {
	if f.setActiveErr != nil {
		return f.setActiveErr
	}
	f.activeProvider = provider
	return nil
}

// fakeProvider is a Provider double recording whether it was asked to
// validate/send, what was sent, and what to return.
type fakeProvider struct {
	validateErr error
	sendErr     error
	sendCalls   int
	lastMessage Message
}

func (f *fakeProvider) ValidateCredentials(context.Context) error { return f.validateErr }
func (f *fakeProvider) Send(_ context.Context, msg Message) error {
	f.sendCalls++
	f.lastMessage = msg
	return f.sendErr
}

// newTestService builds a Service wired to store and factory, failing the
// test immediately if template parsing errors (it shouldn't, given T6's
// embedded templates are always present).
func newTestService(t *testing.T, store EmailProviderStore, factory ProviderFactory) *Service {
	t.Helper()
	svc, err := NewService(store, factory, testMasterKey, zap.NewNop())
	if err != nil {
		t.Fatalf("NewService() returned unexpected error: %v", err)
	}
	return svc
}

func TestConnect_ValidKey_EncryptsAndPersists(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey string) (Provider, error) {
		return &fakeProvider{}, nil
	}
	svc := newTestService(t, store, factory)

	err := svc.Connect(t.Context(), "sendgrid", "real-api-key", "owner@example.com", "Owner")
	if err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	row, ok := store.rows["sendgrid"]
	if !ok {
		t.Fatal("Connect() did not persist a row for sendgrid")
	}
	if row.Status != "connected" {
		t.Errorf("Status = %q, want %q", row.Status, "connected")
	}
	if row.FromEmail != "owner@example.com" || row.FromName != "Owner" {
		t.Errorf("FromEmail/FromName = %q/%q, want %q/%q", row.FromEmail, row.FromName, "owner@example.com", "Owner")
	}

	decrypted, err := crypto.Decrypt(testMasterKey, row.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("Decrypt() returned unexpected error: %v", err)
	}
	if string(decrypted) != "real-api-key" {
		t.Errorf("decrypted stored key = %q, want %q (never plaintext, but must round-trip)", decrypted, "real-api-key")
	}
}

func TestConnect_InvalidCredentials_ReturnsErrValidationFailed_PersistsNothing(t *testing.T) {
	store := newFakeStore()
	factory := func(provider, apiKey string) (Provider, error) {
		return &fakeProvider{validateErr: ErrUnauthorized}, nil
	}
	svc := newTestService(t, store, factory)

	err := svc.Connect(t.Context(), "sendgrid", "bad-key", "owner@example.com", "Owner")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("Connect() error = %v, want ErrValidationFailed", err)
	}
	if _, ok := store.rows["sendgrid"]; ok {
		t.Fatal("Connect() persisted a row despite validation failure")
	}
}

func TestConnect_ReconnectWithBadKey_LeavesPreviousValidRowUntouched(t *testing.T) {
	store := newFakeStore()
	validFactory := func(provider, apiKey string) (Provider, error) {
		return &fakeProvider{}, nil
	}
	svc := newTestService(t, store, validFactory)

	if err := svc.Connect(t.Context(), "sendgrid", "good-key", "owner@example.com", "Owner"); err != nil {
		t.Fatalf("first Connect() returned unexpected error: %v", err)
	}
	originalRow := *store.rows["sendgrid"]

	svc.factory = func(provider, apiKey string) (Provider, error) {
		return &fakeProvider{validateErr: ErrUnauthorized}, nil
	}
	err := svc.Connect(t.Context(), "sendgrid", "bad-key", "attacker@example.com", "Attacker")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("reconnect Connect() error = %v, want ErrValidationFailed", err)
	}

	current := store.rows["sendgrid"]
	if current.EncryptedAPIKey == nil || string(current.EncryptedAPIKey) != string(originalRow.EncryptedAPIKey) {
		t.Errorf("EncryptedAPIKey changed after failed reconnect, want unchanged from %q", originalRow.EncryptedAPIKey)
	}
	if current.FromEmail != originalRow.FromEmail || current.FromName != originalRow.FromName {
		t.Errorf("FromEmail/FromName = %q/%q after failed reconnect, want unchanged %q/%q", current.FromEmail, current.FromName, originalRow.FromEmail, originalRow.FromName)
	}
}

func TestConnect_MissingAPIKey_ReturnsErrInvalidInput_NeverCallsFactory(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey string) (Provider, error) {
		factoryCalled = true
		return &fakeProvider{}, nil
	}
	svc := newTestService(t, store, factory)

	err := svc.Connect(t.Context(), "sendgrid", "", "owner@example.com", "Owner")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Connect() error = %v, want ErrInvalidInput", err)
	}
	if factoryCalled {
		t.Error("Connect() called the provider factory despite missing api_key")
	}
}

func TestConnect_MissingFromEmailOrFromName_ReturnsErrInvalidInput_NeverCallsFactory(t *testing.T) {
	cases := []struct {
		name      string
		fromEmail string
		fromName  string
	}{
		{"missing from_email", "", "Owner"},
		{"missing from_name", "owner@example.com", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			factoryCalled := false
			factory := func(provider, apiKey string) (Provider, error) {
				factoryCalled = true
				return &fakeProvider{}, nil
			}
			svc := newTestService(t, store, factory)

			err := svc.Connect(t.Context(), "sendgrid", "api-key", tc.fromEmail, tc.fromName)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Connect() error = %v, want ErrInvalidInput", err)
			}
			if factoryCalled {
				t.Error("Connect() called the provider factory despite missing required field")
			}
		})
	}
}

func TestConnect_MalformedFromEmail_ReturnsErrInvalidInput_NeverCallsFactory(t *testing.T) {
	store := newFakeStore()
	factoryCalled := false
	factory := func(provider, apiKey string) (Provider, error) {
		factoryCalled = true
		return &fakeProvider{}, nil
	}
	svc := newTestService(t, store, factory)

	err := svc.Connect(t.Context(), "sendgrid", "api-key", "not-an-email", "Owner")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Connect() error = %v, want ErrInvalidInput", err)
	}
	if factoryCalled {
		t.Error("Connect() called the provider factory despite a malformed from_email")
	}
}

func TestActivate_ConnectedProvider_SetsActiveProvider(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return &fakeProvider{}, nil })

	if err := svc.Connect(t.Context(), "resend", "api-key", "owner@example.com", "Owner"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}

	if err := svc.Activate(t.Context(), "resend"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}
	if store.activeProvider != "resend" {
		t.Errorf("activeProvider = %q, want %q", store.activeProvider, "resend")
	}
}

func TestActivate_NeverConnected_ReturnsErrProviderNotConnected_DoesNotChangeActiveProvider(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return &fakeProvider{}, nil })

	err := svc.Activate(t.Context(), "sendgrid")
	if !errors.Is(err, ErrProviderNotConnected) {
		t.Fatalf("Activate() error = %v, want ErrProviderNotConnected", err)
	}
	if store.activeProvider != "" {
		t.Errorf("activeProvider = %q, want unchanged \"\"", store.activeProvider)
	}
}

func TestActivate_InvalidStatus_ReturnsErrProviderNotConnected(t *testing.T) {
	store := newFakeStore()
	store.rows["sendgrid"] = &db.EmailProvider{Provider: "sendgrid", Status: "invalid"}
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return &fakeProvider{}, nil })

	err := svc.Activate(t.Context(), "sendgrid")
	if !errors.Is(err, ErrProviderNotConnected) {
		t.Fatalf("Activate() error = %v, want ErrProviderNotConnected", err)
	}
	if store.activeProvider != "" {
		t.Errorf("activeProvider = %q, want unchanged \"\"", store.activeProvider)
	}
}

func TestList_NoneConnected_ReturnsEmptyProvidersAndNoActive(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return &fakeProvider{}, nil })

	result, err := svc.List(t.Context(), 1, 20)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if len(result.Providers) != 0 {
		t.Errorf("len(Providers) = %d, want 0", len(result.Providers))
	}
	if result.ActiveProvider != "" {
		t.Errorf("ActiveProvider = %q, want \"\"", result.ActiveProvider)
	}
}

func TestList_ConnectedAndActive_ReflectsBothWithoutKeyMaterial(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return &fakeProvider{}, nil })

	if err := svc.Connect(t.Context(), "sendgrid", "key-1", "s@example.com", "SendGrid Sender"); err != nil {
		t.Fatalf("Connect(sendgrid) returned unexpected error: %v", err)
	}
	if err := svc.Connect(t.Context(), "resend", "key-2", "r@example.com", "Resend Sender"); err != nil {
		t.Fatalf("Connect(resend) returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "resend"); err != nil {
		t.Fatalf("Activate(resend) returned unexpected error: %v", err)
	}

	result, err := svc.List(t.Context(), 1, 20)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if result.ActiveProvider != "resend" {
		t.Errorf("ActiveProvider = %q, want %q", result.ActiveProvider, "resend")
	}
	if len(result.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(result.Providers))
	}
	for _, p := range result.Providers {
		if p.Status != "connected" {
			t.Errorf("provider %q Status = %q, want %q", p.Provider, p.Status, "connected")
		}
	}
}

func TestSendAdminInvite_NoActiveProvider_ReturnsErrNoActiveProvider_NeverCallsSend(t *testing.T) {
	store := newFakeStore()
	sentProvider := &fakeProvider{}
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return sentProvider, nil })

	err := svc.SendAdminInvite(t.Context(), "invitee@example.com", AdminInviteEmailData{
		CompanyName: "Acme", Role: "operator", AcceptURL: "https://vane.example.com/invite/abc",
	})
	if !errors.Is(err, ErrNoActiveProvider) {
		t.Fatalf("SendAdminInvite() error = %v, want ErrNoActiveProvider", err)
	}
	if sentProvider.sendCalls != 0 {
		t.Errorf("Provider.Send call count = %d, want 0 (zero network calls with no active provider)", sentProvider.sendCalls)
	}
}

func TestSendAdminInvite_ActiveProvider_RendersTemplateAndSendsWithDecryptedKeyAndStoredSender(t *testing.T) {
	store := newFakeStore()
	sentProvider := &fakeProvider{}
	var factoryProvider, factoryAPIKey string
	factory := func(provider, apiKey string) (Provider, error) {
		factoryProvider = provider
		factoryAPIKey = apiKey
		return sentProvider, nil
	}
	svc := newTestService(t, store, factory)

	if err := svc.Connect(t.Context(), "sendgrid", "decrypted-api-key", "invites@acme.example.com", "Acme Invites"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "sendgrid"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	data := AdminInviteEmailData{
		CompanyName: "Acme Inc.",
		Role:        "operator",
		AcceptURL:   "https://vane.example.com/invite/abc123",
	}
	if err := svc.SendAdminInvite(t.Context(), "invitee@example.com", data); err != nil {
		t.Fatalf("SendAdminInvite() returned unexpected error: %v", err)
	}

	if sentProvider.sendCalls != 1 {
		t.Fatalf("Provider.Send call count = %d, want 1", sentProvider.sendCalls)
	}
	if factoryProvider != "sendgrid" {
		t.Errorf("factory provider = %q, want %q", factoryProvider, "sendgrid")
	}
	if factoryAPIKey != "decrypted-api-key" {
		t.Errorf("factory apiKey = %q, want decrypted value %q (EMAIL-07: Send must be called with the decrypted api_key)", factoryAPIKey, "decrypted-api-key")
	}

	msg := sentProvider.lastMessage
	if msg.To != "invitee@example.com" {
		t.Errorf("Message.To = %q, want %q", msg.To, "invitee@example.com")
	}
	if msg.FromEmail != "invites@acme.example.com" || msg.FromName != "Acme Invites" {
		t.Errorf("Message.FromEmail/FromName = %q/%q, want stored %q/%q (EMAIL-07)", msg.FromEmail, msg.FromName, "invites@acme.example.com", "Acme Invites")
	}
	if !strings.Contains(msg.HTMLBody, data.AcceptURL) || !strings.Contains(msg.HTMLBody, data.Role) || !strings.Contains(msg.HTMLBody, data.CompanyName) {
		t.Errorf("HTMLBody = %q, want it to contain AcceptURL=%q, Role=%q, CompanyName=%q (EMAIL-09)", msg.HTMLBody, data.AcceptURL, data.Role, data.CompanyName)
	}
	if !strings.Contains(msg.TextBody, data.AcceptURL) || !strings.Contains(msg.TextBody, data.Role) || !strings.Contains(msg.TextBody, data.CompanyName) {
		t.Errorf("TextBody = %q, want it to contain AcceptURL=%q, Role=%q, CompanyName=%q (EMAIL-09)", msg.TextBody, data.AcceptURL, data.Role, data.CompanyName)
	}
}

func TestSendAdminInvite_ProviderSendFails_ReturnsErrorUnmodified_ExactlyOneCall(t *testing.T) {
	store := newFakeStore()
	sendFailure := errors.New("sendgrid: server error")
	sentProvider := &fakeProvider{sendErr: sendFailure}
	svc := newTestService(t, store, func(provider, apiKey string) (Provider, error) { return sentProvider, nil })

	if err := svc.Connect(t.Context(), "sendgrid", "api-key", "owner@example.com", "Owner"); err != nil {
		t.Fatalf("Connect() returned unexpected error: %v", err)
	}
	if err := svc.Activate(t.Context(), "sendgrid"); err != nil {
		t.Fatalf("Activate() returned unexpected error: %v", err)
	}

	err := svc.SendAdminInvite(t.Context(), "invitee@example.com", AdminInviteEmailData{
		CompanyName: "Acme", Role: "operator", AcceptURL: "https://vane.example.com/invite/abc",
	})
	if !errors.Is(err, sendFailure) {
		t.Fatalf("SendAdminInvite() error = %v, want the underlying send failure %v unmodified", err, sendFailure)
	}
	if sentProvider.sendCalls != 1 {
		t.Errorf("Provider.Send call count = %d, want exactly 1 (no retry)", sentProvider.sendCalls)
	}
}
