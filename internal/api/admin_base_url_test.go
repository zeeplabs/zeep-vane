package api

import "testing"

func TestAdminBaseURL_ConfiguredValue_ReturnedVerbatim(t *testing.T) {
	got := adminBaseURL("https://vane.example.com")
	if got != "https://vane.example.com" {
		t.Errorf("adminBaseURL(%q) = %q, want it returned verbatim", "https://vane.example.com", got)
	}
}

// TestAdminBaseURL_Unconfigured_ReturnsPlaceholder_NeverEmpty guards against
// adminBaseURL("") ever returning "" - a caller building "%s/reset-password/%s"
// from an empty base would emit a path-relative link ("/reset-password/<token>")
// that resolves against whatever host the *recipient's browser* is on, silently
// reintroducing the same host-confusion problem this function exists to close.
func TestAdminBaseURL_Unconfigured_ReturnsPlaceholder_NeverEmpty(t *testing.T) {
	got := adminBaseURL("")
	if got == "" {
		t.Fatal("adminBaseURL(\"\") = \"\", want a non-empty placeholder")
	}
	if got != unconfiguredAdminBaseURLPlaceholder {
		t.Errorf("adminBaseURL(\"\") = %q, want the placeholder %q", got, unconfiguredAdminBaseURLPlaceholder)
	}
}
