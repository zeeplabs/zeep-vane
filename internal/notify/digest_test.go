package notify

import (
	"fmt"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func TestBuildWeeklyDigestData_AveragesPerServiceUptime(t *testing.T) {
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	got := BuildWeeklyDigestData(WeeklyDigestInput{
		Tenant:            db.Tenant{Name: "Acme"},
		Services:          []DigestServiceUptime{{Name: "b", UptimePercent: 100}, {Name: "a", UptimePercent: 50}},
		IncidentsOpened:   3,
		IncidentsResolved: 2,
		PeriodStart:       start, PeriodEnd: end,
	})

	if got.UptimePercent != 75 {
		t.Errorf("UptimePercent = %v, want 75 (mean of 100 and 50)", got.UptimePercent)
	}
	if got.TenantName != "Acme" || got.IncidentsOpened != 3 || got.IncidentsResolved != 2 {
		t.Errorf("data = %+v, want tenant Acme with counts 3/2", got)
	}
	if !got.PeriodStart.Equal(start) || !got.PeriodEnd.Equal(end) {
		t.Errorf("period = %s..%s, want %s..%s", got.PeriodStart, got.PeriodEnd, start, end)
	}
	if len(got.Services) != 2 || got.Services[0].Name != "a" || got.Services[1].Name != "b" {
		t.Errorf("Services = %+v, want both services in name order", got.Services)
	}
	if got.PeriodLabel() != "Sep 8–14, 2026" {
		t.Errorf("PeriodLabel() = %q, want the default-timezone label for a nil tenant timezone", got.PeriodLabel())
	}
}

func TestBuildWeeklyDigestData_SingleServiceUptime(t *testing.T) {
	got := BuildWeeklyDigestData(WeeklyDigestInput{Services: []DigestServiceUptime{{Name: "a", UptimePercent: 99.5}}})

	if got.UptimePercent != 99.5 {
		t.Errorf("UptimePercent = %v, want 99.5", got.UptimePercent)
	}
}

func TestBuildWeeklyDigestData_NoServices_DefaultsTo100(t *testing.T) {
	got := BuildWeeklyDigestData(WeeklyDigestInput{Tenant: db.Tenant{Name: "Acme"}})

	if got.UptimePercent != 100 {
		t.Errorf("UptimePercent = %v, want 100 with no service data", got.UptimePercent)
	}
	if len(got.Services) != 0 || got.IncidentCount() != 0 {
		t.Errorf("data = %+v, want empty lists", got)
	}
}

func TestBuildWeeklyDigestData_CapsServicesKeepingLowestUptime(t *testing.T) {
	services := make([]DigestServiceUptime, 0, 12)
	for i := 0; i < 12; i++ {
		services = append(services, DigestServiceUptime{Name: fmt.Sprintf("svc-%02d", i), UptimePercent: 100})
	}
	services[7].UptimePercent = 90 // the one degraded service must survive the cap

	got := BuildWeeklyDigestData(WeeklyDigestInput{Services: services})

	if len(got.Services) != 10 || got.TotalServices != 12 || got.HiddenServices() != 2 {
		t.Fatalf("Services=%d Total=%d Hidden=%d, want 10/12/2", len(got.Services), got.TotalServices, got.HiddenServices())
	}
	found := false
	for _, s := range got.Services {
		if s.Name == "svc-07" {
			found = true
		}
	}
	if !found {
		t.Error("the lowest-uptime service must be kept when the list is capped")
	}
}

func TestBuildWeeklyDigestData_IncidentDurationsAndMTTR(t *testing.T) {
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	resolvedA := start.Add(2*time.Hour + 30*time.Minute)
	resolvedB := start.Add(5 * time.Hour)

	got := BuildWeeklyDigestData(WeeklyDigestInput{
		PeriodStart: start, PeriodEnd: end,
		Incidents: []db.DigestIncident{
			{Title: "A", Status: "resolved", CreatedAt: start.Add(2 * time.Hour), ResolvedAt: &resolvedA, ServiceNames: []string{"api", "web"}},
			{Title: "B", Status: "resolved", CreatedAt: start.Add(4 * time.Hour), ResolvedAt: &resolvedB},
			{Title: "C", Status: "investigating", CreatedAt: end.Add(-3 * time.Hour)},
		},
	})

	if got.IncidentCount() != 3 {
		t.Fatalf("IncidentCount() = %d, want 3", got.IncidentCount())
	}
	if got.Incidents[0].ServiceName != "api, web" || got.Incidents[0].Duration != 30*time.Minute {
		t.Errorf("incident A = %+v, want joined service names and 30min", got.Incidents[0])
	}
	if got.Incidents[2].Duration != 3*time.Hour {
		t.Errorf("open incident C duration = %s, want 3h (to period end)", got.Incidents[2].Duration)
	}
	if got.MTTR != 45*time.Minute {
		t.Errorf("MTTR = %s, want 45m (mean of 30m and 1h)", got.MTTR)
	}
}
