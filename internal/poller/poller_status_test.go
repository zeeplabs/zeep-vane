package poller

import "testing"

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"ok", "operational"},
		{"warning", "degraded"},
		{"breached", "outage"},
		{"no_data", "degraded"},
	}

	for _, tt := range tests {
		if got := normalizeStatus(tt.state); got != tt.want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}
