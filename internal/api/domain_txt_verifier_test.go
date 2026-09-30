package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeTXTResolver is the injected DNS seam for netApexTXTVerifier tests. It
// records the queried name and returns canned values/error; block makes it
// wait for context cancellation, to exercise the bounded-timeout path.
type fakeTXTResolver struct {
	values []string
	err    error
	block  bool
	name   string
}

func (f *fakeTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	f.name = name
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.values, f.err
}

// TestNetApexTXTVerifier_ExactMatch_FoundAndMatches covers DATV-03: the exact
// expected token present at _vane-verify.<hostname> marks the check as a
// match.
func TestNetApexTXTVerifier_ExactMatch_FoundAndMatches(t *testing.T) {
	resolver := &fakeTXTResolver{values: []string{"abc123token"}}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if !got.TXTFound {
		t.Error("TXTFound = false, want true (record present)")
	}
	if !got.TXTMatches {
		t.Error("TXTMatches = false, want true (value equals the expected token)")
	}
	if resolver.name != "_vane-verify.example.com" {
		t.Errorf("looked up %q, want %q", resolver.name, "_vane-verify.example.com")
	}
}

// TestNetApexTXTVerifier_ValueMismatch_FoundNotMatches covers DATV-04: a
// record exists but none of its values equals the token.
func TestNetApexTXTVerifier_ValueMismatch_FoundNotMatches(t *testing.T) {
	resolver := &fakeTXTResolver{values: []string{"some-other-value"}}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if !got.TXTFound {
		t.Error("TXTFound = false, want true (a record was returned)")
	}
	if got.TXTMatches {
		t.Error("TXTMatches = true, want false (value differs from the expected token)")
	}
}

// TestNetApexTXTVerifier_RecordAbsent_NotFound covers DATV-04's "not found"
// case: the resolver errors (NXDOMAIN/no record).
func TestNetApexTXTVerifier_RecordAbsent_NotFound(t *testing.T) {
	resolver := &fakeTXTResolver{err: errors.New("no such host")}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if got.TXTFound {
		t.Error("TXTFound = true, want false (lookup errored)")
	}
	if got.TXTMatches {
		t.Error("TXTMatches = true, want false")
	}
}

// TestNetApexTXTVerifier_EmptyAnswer_NotFound covers the edge where the
// lookup succeeds but returns no values.
func TestNetApexTXTVerifier_EmptyAnswer_NotFound(t *testing.T) {
	resolver := &fakeTXTResolver{values: []string{}}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if got.TXTFound {
		t.Error("TXTFound = true, want false (empty answer)")
	}
	if got.TXTMatches {
		t.Error("TXTMatches = true, want false (empty answer)")
	}
}

// TestNetApexTXTVerifier_MultipleRecords_OneMatches covers the design's
// multiple-TXT-records case: any value matching (not just the first) is a
// match.
func TestNetApexTXTVerifier_MultipleRecords_OneMatches(t *testing.T) {
	resolver := &fakeTXTResolver{values: []string{"unrelated", "abc123token", "another"}}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if !got.TXTFound {
		t.Error("TXTFound = false, want true")
	}
	if !got.TXTMatches {
		t.Error("TXTMatches = false, want true (the token is one of several values)")
	}
}

// TestNetApexTXTVerifier_MultipleRecords_NoneMatches covers multiple records
// with no match.
func TestNetApexTXTVerifier_MultipleRecords_NoneMatches(t *testing.T) {
	resolver := &fakeTXTResolver{values: []string{"one", "two", "three"}}
	v := &netApexTXTVerifier{resolver: resolver}

	got := v.Verify(context.Background(), "example.com", "abc123token")

	if !got.TXTFound {
		t.Error("TXTFound = false, want true")
	}
	if got.TXTMatches {
		t.Error("TXTMatches = true, want false (none of the values matches)")
	}
}

// TestNetApexTXTVerifier_Timeout_NotFound covers the bounded-lookup path: a
// resolver that blocks past the deadline reports not-found rather than
// hanging. A short parent deadline keeps the test fast while exercising the
// same cancellation path.
func TestNetApexTXTVerifier_Timeout_NotFound(t *testing.T) {
	resolver := &fakeTXTResolver{block: true}
	v := &netApexTXTVerifier{resolver: resolver}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	got := v.Verify(ctx, "example.com", "abc123token")

	if got.TXTFound {
		t.Error("TXTFound = true, want false (lookup timed out)")
	}
	if got.TXTMatches {
		t.Error("TXTMatches = true, want false (lookup timed out)")
	}
}
