package notify

import (
	"testing"
	"time"
)

func TestBuildWeeklyDigestData_AveragesPerServiceUptime(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)

	got := BuildWeeklyDigestData("Acme", []float64{100, 50}, 3, 2, start, end)

	if got.UptimePercent != 75 {
		t.Errorf("UptimePercent = %v, want 75 (mean of 100 and 50)", got.UptimePercent)
	}
	if got.TenantName != "Acme" || got.IncidentsOpened != 3 || got.IncidentsResolved != 2 {
		t.Errorf("data = %+v, want tenant Acme with counts 3/2", got)
	}
	if got.PeriodStart != "2026-09-01" || got.PeriodEnd != "2026-09-08" {
		t.Errorf("period = %q..%q, want 2026-09-01..2026-09-08", got.PeriodStart, got.PeriodEnd)
	}
}

func TestBuildWeeklyDigestData_SingleServiceUptime(t *testing.T) {
	got := BuildWeeklyDigestData("Acme", []float64{99.5}, 0, 0, time.Now(), time.Now())

	if got.UptimePercent != 99.5 {
		t.Errorf("UptimePercent = %v, want 99.5", got.UptimePercent)
	}
}

func TestBuildWeeklyDigestData_NoServices_DefaultsTo100(t *testing.T) {
	got := BuildWeeklyDigestData("Acme", nil, 0, 0, time.Now(), time.Now())

	if got.UptimePercent != 100 {
		t.Errorf("UptimePercent = %v, want 100 with no service data", got.UptimePercent)
	}
}
