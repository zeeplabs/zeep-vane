package tls

import (
	"context"
	"testing"
)

// fakeStatusPageStore is a StatusPageStore backed by an in-memory map, so
// HostPolicy can be tested without a Postgres dependency (this task's Gate
// is "quick": go test ./... && go vet ./..., no -tags=integration).
type fakeStatusPageStore struct {
	statesByHostname map[string]string
}

func (f *fakeStatusPageStore) StateByHostname(ctx context.Context, hostname string) (string, error) {
	state, ok := f.statesByHostname[hostname]
	if !ok {
		return "", ErrHostnameNotFound
	}
	return state, nil
}

func TestHostPolicy_UnregisteredHostname_Rejects(t *testing.T) {
	store := &fakeStatusPageStore{statesByHostname: map[string]string{}}
	policy := HostPolicy(store)

	err := policy(context.Background(), "attacker-controlled.example.com")

	if err == nil {
		t.Fatal("HostPolicy for an unregistered hostname returned nil error, want rejection - this is the abuse-prevention boundary, must never allow ACME for an arbitrary hostname")
	}
}

func TestHostPolicy_RegisteredNonDraftHostname_Allows(t *testing.T) {
	store := &fakeStatusPageStore{statesByHostname: map[string]string{
		"status.empresa.com": "pending_tls",
	}}
	policy := HostPolicy(store)

	err := policy(context.Background(), "status.empresa.com")

	if err != nil {
		t.Fatalf("HostPolicy for a registered, non-draft hostname returned error: %v, want nil", err)
	}
}

func TestHostPolicy_RegisteredDraftHostname_Rejects(t *testing.T) {
	store := &fakeStatusPageStore{statesByHostname: map[string]string{
		"status.empresa.com": "draft",
	}}
	policy := HostPolicy(store)

	err := policy(context.Background(), "status.empresa.com")

	if err == nil {
		t.Fatal("HostPolicy for a draft-state hostname returned nil error, want rejection - a draft status page must never trigger a live ACME request")
	}
}
