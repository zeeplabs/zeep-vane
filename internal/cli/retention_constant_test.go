package cli

import (
	"testing"
	"time"
)

// TestPruneRetention_Is95Days confirms pruneRetention holds the
// spec-confirmed 95-day value (TRS-08/TRS-09), long enough to cover the
// public status page's 90d range tier. The integration-level pruning
// *behavior* driven by this constant is covered one layer down in
// internal/retention.
func TestPruneRetention_Is95Days(t *testing.T) {
	want := 95 * 24 * time.Hour
	if pruneRetention != want {
		t.Fatalf("pruneRetention = %v, want %v", pruneRetention, want)
	}
}
