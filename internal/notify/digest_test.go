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

// TestBuildWeeklyDigestData_CapsIncidentsOpenFirstThenLongest: with more than
// email.DigestIncidentsCap incidents, the ones still open at period end are
// kept first, then the longest resolved ones; the kept rows stay in input
// order and the counts keep the uncapped total.
func TestBuildWeeklyDigestData_CapsIncidentsOpenFirstThenLongest(t *testing.T) {
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	afterEnd := end.Add(2 * time.Hour)

	var incidents []db.DigestIncident
	// 12 short resolved incidents, durations 1..12 minutes.
	for i := 1; i <= 12; i++ {
		createdAt := start.Add(time.Duration(i) * time.Hour)
		resolvedAt := createdAt.Add(time.Duration(i) * time.Minute)
		incidents = append(incidents, db.DigestIncident{Title: fmt.Sprintf("resolved-%02d", i), Status: "resolved", CreatedAt: createdAt, ResolvedAt: &resolvedAt})
	}
	// Two incidents open at period end: one still open, one resolved after
	// the period end. Both are short-lived, so only the open-first rule
	// keeps them.
	incidents = append(incidents,
		db.DigestIncident{Title: "still-open", Status: "investigating", CreatedAt: end.Add(-time.Minute)},
		db.DigestIncident{Title: "resolved-after-end", Status: "resolved", CreatedAt: end.Add(-time.Minute), ResolvedAt: &afterEnd},
	)

	got := BuildWeeklyDigestData(WeeklyDigestInput{PeriodStart: start, PeriodEnd: end, Incidents: incidents})

	if len(got.Incidents) != 10 || got.IncidentCount() != 14 || got.HiddenIncidents() != 4 {
		t.Fatalf("Incidents=%d IncidentCount=%d Hidden=%d, want 10/14/4", len(got.Incidents), got.IncidentCount(), got.HiddenIncidents())
	}
	var titles []string
	for _, row := range got.Incidents {
		titles = append(titles, row.Title)
	}
	// Open-at-end pair first in priority, then the 8 longest resolved
	// (12..5 minutes); displayed in input order.
	want := []string{"resolved-05", "resolved-06", "resolved-07", "resolved-08", "resolved-09", "resolved-10", "resolved-11", "resolved-12", "still-open", "resolved-after-end"}
	if fmt.Sprint(titles) != fmt.Sprint(want) {
		t.Errorf("kept incidents = %v, want %v", titles, want)
	}
	// MTTR still covers every incident resolved in the period, capped or
	// not: mean of 1..12 minutes.
	if got.MTTR != 6*time.Minute+30*time.Second {
		t.Errorf("MTTR = %s, want 6m30s (mean over all 12 resolved, not just the shown ones)", got.MTTR)
	}
}

func TestBuildWeeklyDigestData_AtCap_NoTruncation(t *testing.T) {
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	var incidents []db.DigestIncident
	for i := 0; i < 10; i++ {
		incidents = append(incidents, db.DigestIncident{Title: fmt.Sprintf("inc-%d", i), Status: "investigating", CreatedAt: start})
	}

	got := BuildWeeklyDigestData(WeeklyDigestInput{PeriodStart: start, PeriodEnd: start.Add(7 * 24 * time.Hour), Incidents: incidents})

	if len(got.Incidents) != 10 || got.IncidentCount() != 10 || got.HiddenIncidents() != 0 {
		t.Errorf("Incidents=%d IncidentCount=%d Hidden=%d, want 10/10/0", len(got.Incidents), got.IncidentCount(), got.HiddenIncidents())
	}
}

// TestBuildWeeklyDigestData_ResolvedAfterPeriodEnd_ShownOngoing: an incident
// resolved between the period end and the send was open for the whole
// summarized week - shown ongoing (not RESOLVED), duration up to the period
// end, and excluded from MTTR.
func TestBuildWeeklyDigestData_ResolvedAfterPeriodEnd_ShownOngoing(t *testing.T) {
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	resolvedAfter := end.Add(9 * time.Hour) // resolved Monday morning, before the digest went out

	got := BuildWeeklyDigestData(WeeklyDigestInput{
		PeriodStart: start, PeriodEnd: end,
		Incidents: []db.DigestIncident{
			{Title: "late fix", Status: "resolved", CreatedAt: end.Add(-2 * time.Hour), ResolvedAt: &resolvedAfter},
		},
	})

	row := got.Incidents[0]
	if !row.OngoingAtPeriodEnd || row.StatusPill() != "ONGOING" {
		t.Errorf("row = %+v pill %q, want ongoing at period end", row, row.StatusPill())
	}
	if row.Duration != 2*time.Hour {
		t.Errorf("Duration = %s, want 2h (up to the period end)", row.Duration)
	}
	if got.MTTR != 0 {
		t.Errorf("MTTR = %s, want 0 (not resolved inside the period)", got.MTTR)
	}
}
