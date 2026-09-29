package rdap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// realDomainBody is a fixture matching a real registry's RDAP domain object
// shape (RFC 9083 §5.3) - a registration, an expiration, and a registrar
// entity alongside a registrant one (the registrar is not simply the first
// entity).
const realDomainBody = `{
  "objectClassName": "domain",
  "ldhName": "EXAMPLE.COM",
  "events": [
    {"eventAction": "registration", "eventDate": "2010-01-01T00:00:00Z"},
    {"eventAction": "expiration", "eventDate": "2027-03-15T04:00:00Z"},
    {"eventAction": "last changed", "eventDate": "2024-02-01T00:00:00Z"}
  ],
  "entities": [
    {"roles": ["registrant"], "vcardArray": ["vcard", [["version", {}, "text", "4.0"], ["fn", {}, "text", "Jane Registrant"]]]},
    {"roles": ["registrar"], "handle": "376", "vcardArray": ["vcard", [["version", {}, "text", "4.0"], ["fn", {}, "text", "GoDaddy.com, LLC"]]]}
  ]
}`

// TestLookup_Success_ParsesExpirationAndRegistrar covers DHM-01: a
// successful RDAP response yields the expiration date and registrar.
func TestLookup_Success_ParsesExpirationAndRegistrar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/domain/example.com" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/domain/example.com")
		}
		w.Header().Set("Content-Type", "application/rdap+json")
		_, _ = w.Write([]byte(realDomainBody))
	}))
	defer server.Close()

	expiresAt, registrar, err := NewClient(server.URL).Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Lookup() returned unexpected error: %v", err)
	}

	wantExpires := time.Date(2027, 3, 15, 4, 0, 0, 0, time.UTC)
	if expiresAt == nil || !expiresAt.Equal(wantExpires) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, wantExpires)
	}
	if registrar == nil || *registrar != "GoDaddy.com, LLC" {
		t.Errorf("registrar = %v, want %q", registrar, "GoDaddy.com, LLC")
	}
}

// TestLookup_MissingExpirationEvent_NilWithoutError covers registries that
// omit the expiration event: expiresAt is nil, not an error.
func TestLookup_MissingExpirationEvent_NilWithoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
		  "events": [{"eventAction": "registration", "eventDate": "2010-01-01T00:00:00Z"}],
		  "entities": []
		}`))
	}))
	defer server.Close()

	expiresAt, registrar, err := NewClient(server.URL).Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Lookup() returned unexpected error: %v", err)
	}
	if expiresAt != nil {
		t.Errorf("expiresAt = %v, want nil when no expiration event is present", expiresAt)
	}
	if registrar != nil {
		t.Errorf("registrar = %v, want nil when no registrar entity is present", registrar)
	}
}

// TestLookup_Non2xx_ReturnsError covers an HTTP-level failure (unknown TLD,
// rate limit).
func TestLookup_Non2xx_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorCode":404,"title":"Not Found"}`))
	}))
	defer server.Close()

	expiresAt, registrar, err := NewClient(server.URL).Lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("Lookup() error = nil, want a non-nil error for a 404 response")
	}
	if expiresAt != nil || registrar != nil {
		t.Errorf("Lookup() returned (%v, %v) alongside an error, want nil, nil", expiresAt, registrar)
	}
}

// TestLookup_MalformedJSON_ReturnsError covers a 2xx response whose body
// isn't valid RDAP JSON.
func TestLookup_MalformedJSON_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events": [`))
	}))
	defer server.Close()

	_, _, err := NewClient(server.URL).Lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("Lookup() error = nil, want a non-nil error for malformed JSON")
	}
}

// TestLookup_Timeout_ReturnsError covers DHM-03's network-timeout branch: a
// slow server must surface as an error, never hang or report a zero-value
// success.
func TestLookup_Timeout_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(realDomainBody))
	}))
	defer server.Close()

	client := NewClient(server.URL).WithHTTPClient(&http.Client{Timeout: 25 * time.Millisecond})
	expiresAt, registrar, err := client.Lookup(context.Background(), "example.com")
	if err == nil {
		t.Fatal("Lookup() error = nil, want a timeout error")
	}
	if expiresAt != nil || registrar != nil {
		t.Errorf("Lookup() returned (%v, %v) alongside an error, want nil, nil", expiresAt, registrar)
	}
}
