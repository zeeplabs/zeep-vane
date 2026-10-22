package poller

import (
	"math"
	"testing"
	"time"
)

// TestBurnRate pins burnRate's arithmetic and its total-function edges
// (BURN-17): no sample, zero budget, and errors outside [0, requests].
func TestBurnRate(t *testing.T) {
	tests := []struct {
		name     string
		errors   int64
		requests int64
		target   float64
		want     float64
	}{
		{name: "1% errors at 99.5 is 2x", errors: 1, requests: 100, target: 99.5, want: 2.0},
		{name: "no errors is 0", errors: 0, requests: 1000, target: 99.5, want: 0},
		{name: "zero requests is 0", errors: 0, requests: 0, target: 99.5, want: 0},
		{name: "zero requests with errors is 0", errors: 5, requests: 0, target: 99.5, want: 0},
		{name: "negative requests is 0", errors: 5, requests: -1, target: 99.5, want: 0},
		{name: "target 100 with errors is +Inf", errors: 1, requests: 1000, target: 100, want: math.Inf(1)},
		{name: "target 100 without errors is 0", errors: 0, requests: 1000, target: 100, want: 0},
		{name: "target above 100 with errors is +Inf", errors: 1, requests: 1000, target: 101, want: math.Inf(1)},
		{name: "errors above requests clamp to the cap", errors: 50, requests: 10, target: 99.5, want: 200},
		{name: "negative errors clamp to 0", errors: -3, requests: 100, target: 99.5, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := burnRate(tc.errors, tc.requests, tc.target)

			if math.IsInf(tc.want, 1) {
				if !math.IsInf(got, 1) {
					t.Errorf("burnRate(%d, %d, %v) = %v, want +Inf", tc.errors, tc.requests, tc.target, got)
				}
				return
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("burnRate(%d, %d, %v) = %v, want %v", tc.errors, tc.requests, tc.target, got, tc.want)
			}
		})
	}
}

// TestClassifyBurn covers every rule of classifyBurn with windows at a
// 99.5% target (budget 0.5%, so burn = error rate / 0.005) unless a case
// says otherwise.
func TestClassifyBurn(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time {
		ts := now.Add(-d)
		return &ts
	}
	empty := windowCounts{}

	tests := []struct {
		name string
		in   burnInput
		want string
	}{
		// BURN-04: healthy high-traffic service.
		{
			name: "healthy high traffic with few errors",
			in: burnInput{
				Short:    windowCounts{Requests: 10000, Errors: 5},   // 0.1x
				Long:     windowCounts{Requests: 120000, Errors: 60}, // 0.1x
				Previous: "operational",
			},
			want: "operational",
		},
		// Gateway with a ~1% baseline error rate (2x) must not page.
		{
			name: "gateway at 1% baseline stays operational",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 20},   // 2x
				Long:     windowCounts{Requests: 24000, Errors: 240}, // 2x
				Previous: "operational",
			},
			want: "operational",
		},
		{
			name: "gateway just under 7.2x on both windows stays operational",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 70},   // 7.0x
				Long:     windowCounts{Requests: 24000, Errors: 840}, // 7.0x
				Previous: "operational",
			},
			want: "operational",
		},
		// BURN-03.
		{
			name: "gateway just over 7.2x on both windows is degraded",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 73},   // 7.3x
				Long:     windowCounts{Requests: 24000, Errors: 876}, // 7.3x
				Previous: "operational",
			},
			want: "degraded",
		},
		{
			name: "short over 7.2x but long under 7.2x stays operational",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 73},   // 7.3x
				Long:     windowCounts{Requests: 24000, Errors: 240}, // 2x
				Previous: "operational",
			},
			want: "operational",
		},
		// BURN-02.
		{
			name: "sustained outage on both windows",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 400},   // 40x
				Long:     windowCounts{Requests: 24000, Errors: 4800}, // 40x
				Previous: "degraded",
			},
			want: "outage",
		},
		{
			name: "short over 14.4x but long only over 7.2x is degraded",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 200},   // 20x
				Long:     windowCounts{Requests: 24000, Errors: 1200}, // 10x
				Previous: "operational",
			},
			want: "degraded",
		},
		// BURN-05: minimum-errors guard.
		{
			name: "2-error noise is ignored whatever the burn",
			in: burnInput{
				Short:    windowCounts{Requests: 25, Errors: 2},   // 16x
				Long:     windowCounts{Requests: 300, Errors: 30}, // 20x
				Previous: "operational",
			},
			want: "operational",
		},
		{
			name: "3-error incident is caught",
			in: burnInput{
				Short:    windowCounts{Requests: 25, Errors: 3},   // 24x
				Long:     windowCounts{Requests: 300, Errors: 30}, // 20x
				Previous: "operational",
			},
			want: "outage",
		},
		// BURN-04: recovery is driven by the short window alone.
		{
			name: "recovery on the first clean short window",
			in: burnInput{
				Short:        windowCounts{Requests: 2000, Errors: 0},
				Long:         windowCounts{Requests: 24000, Errors: 4800}, // 40x
				Previous:     "outage",
				LastErrorAt:  ago(2 * time.Minute),
				LastChangeAt: now.Add(-30 * time.Minute),
			},
			want: "operational",
		},
		// BURN-07.
		{
			name: "zero traffic while operational stays operational",
			in: burnInput{
				Short:    empty,
				Long:     windowCounts{Requests: 300, Errors: 30}, // 20x
				Previous: "operational",
			},
			want: "operational",
		},
		{
			name: "zero traffic with an unknown previous status is operational",
			in: burnInput{
				Short:    empty,
				Long:     empty,
				Previous: "",
			},
			want: "operational",
		},
		// BURN-06: zero-traffic grace.
		{
			name: "zero traffic in outage before the grace keeps outage",
			in: burnInput{
				Short:        empty,
				Long:         windowCounts{Requests: 300, Errors: 30},
				Previous:     "outage",
				LastErrorAt:  ago(10 * time.Minute),
				LastChangeAt: now.Add(-40 * time.Minute),
			},
			want: "outage",
		},
		{
			name: "zero traffic in degraded before the grace keeps degraded",
			in: burnInput{
				Short:        empty,
				Long:         empty,
				Previous:     "degraded",
				LastErrorAt:  ago(14 * time.Minute),
				LastChangeAt: now.Add(-40 * time.Minute),
			},
			want: "degraded",
		},
		{
			name: "zero traffic in outage exactly at the grace is operational",
			in: burnInput{
				Short:        empty,
				Long:         empty,
				Previous:     "outage",
				LastErrorAt:  ago(zeroTrafficGrace),
				LastChangeAt: now.Add(-40 * time.Minute),
			},
			want: "operational",
		},
		{
			name: "zero traffic in outage after the grace is operational",
			in: burnInput{
				Short:        empty,
				Long:         windowCounts{Requests: 300, Errors: 30},
				Previous:     "outage",
				LastErrorAt:  ago(16 * time.Minute),
				LastChangeAt: now.Add(-40 * time.Minute),
			},
			want: "operational",
		},
		{
			name: "grace falls back to LastChangeAt when LastErrorAt is nil (within)",
			in: burnInput{
				Short:        empty,
				Long:         empty,
				Previous:     "outage",
				LastErrorAt:  nil,
				LastChangeAt: now.Add(-5 * time.Minute),
			},
			want: "outage",
		},
		{
			name: "grace falls back to LastChangeAt when LastErrorAt is nil (after)",
			in: burnInput{
				Short:        empty,
				Long:         empty,
				Previous:     "outage",
				LastErrorAt:  nil,
				LastChangeAt: now.Add(-20 * time.Minute),
			},
			want: "operational",
		},
		{
			name: "LastErrorAt wins over an older LastChangeAt",
			in: burnInput{
				Short:        empty,
				Long:         empty,
				Previous:     "outage",
				LastErrorAt:  ago(5 * time.Minute),
				LastChangeAt: now.Add(-60 * time.Minute),
			},
			want: "outage",
		},
		// BURN-10: first classification.
		{
			name: "not_configured with both windows empty stays not_configured",
			in: burnInput{
				Short:    empty,
				Long:     empty,
				Previous: "not_configured",
			},
			want: "not_configured",
		},
		{
			name: "not_configured with healthy long window is operational",
			in: burnInput{
				Short:    empty,
				Long:     windowCounts{Requests: 300, Errors: 1}, // 0.67x
				Previous: "not_configured",
			},
			want: "operational",
		},
		{
			name: "not_configured with burning long window is classified from it",
			in: burnInput{
				Short:    empty,
				Long:     windowCounts{Requests: 300, Errors: 60}, // 40x
				Previous: "not_configured",
			},
			want: "outage",
		},
		{
			name: "not_configured with long window under minimum errors is operational",
			in: burnInput{
				Short:    empty,
				Long:     windowCounts{Requests: 10, Errors: 2}, // 40x, 2 errors
				Previous: "not_configured",
			},
			want: "operational",
		},
		{
			name: "not_configured with short traffic is classified normally",
			in: burnInput{
				Short:    windowCounts{Requests: 2000, Errors: 73},   // 7.3x
				Long:     windowCounts{Requests: 24000, Errors: 876}, // 7.3x
				Previous: "not_configured",
			},
			want: "degraded",
		},
		// BURN-17: empty long window uses the short burn.
		{
			name: "empty long window uses short burn for outage",
			in: burnInput{
				Short:    windowCounts{Requests: 100, Errors: 10}, // 20x
				Long:     empty,
				Previous: "operational",
			},
			want: "outage",
		},
		{
			name: "empty long window uses short burn for degraded",
			in: burnInput{
				Short:    windowCounts{Requests: 100, Errors: 4}, // 8x
				Long:     empty,
				Previous: "operational",
			},
			want: "degraded",
		},
		{
			name: "empty long window with healthy short is operational",
			in: burnInput{
				Short:    windowCounts{Requests: 100, Errors: 0},
				Long:     empty,
				Previous: "operational",
			},
			want: "operational",
		},
		// BURN-17: target 100 has a zero budget.
		{
			name: "target 100 with minimum errors is outage",
			in: burnInput{
				Short:    windowCounts{Requests: 100000, Errors: 3},
				Long:     windowCounts{Requests: 1200000, Errors: 3},
				Target:   100,
				Previous: "operational",
			},
			want: "outage",
		},
		{
			name: "target 100 under minimum errors is operational",
			in: burnInput{
				Short:    windowCounts{Requests: 100000, Errors: 2},
				Long:     windowCounts{Requests: 1200000, Errors: 2},
				Target:   100,
				Previous: "operational",
			},
			want: "operational",
		},
		{
			name: "target 100 without errors is operational",
			in: burnInput{
				Short:    windowCounts{Requests: 100000, Errors: 0},
				Long:     windowCounts{Requests: 1200000, Errors: 0},
				Target:   100,
				Previous: "outage",
			},
			want: "operational",
		},
		// BURN-17: errors above requests are clamped.
		{
			name: "errors above requests are clamped and still classify",
			in: burnInput{
				Short:    windowCounts{Requests: 10, Errors: 50}, // clamped to 10 -> 200x
				Long:     windowCounts{Requests: 100, Errors: 500},
				Previous: "operational",
			},
			want: "outage",
		},
		{
			name: "clamped errors below the minimum stay operational",
			in: burnInput{
				Short:    windowCounts{Requests: 2, Errors: 50}, // clamped to 2
				Long:     windowCounts{Requests: 100, Errors: 500},
				Previous: "operational",
			},
			want: "operational",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			if in.Target == 0 {
				in.Target = 99.5
			}
			in.Now = now

			if got := classifyBurn(in); got != tc.want {
				t.Errorf("classifyBurn(%+v) = %q, want %q", in, got, tc.want)
			}
		})
	}
}
