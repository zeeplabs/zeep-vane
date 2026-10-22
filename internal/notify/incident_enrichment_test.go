package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

type fakeTenantGetter struct {
	tenant *db.Tenant
	err    error
	calls  int
}

func (f *fakeTenantGetter) Get(context.Context, string) (*db.Tenant, error) {
	f.calls++
	return f.tenant, f.err
}

type fakeIncidentDetails struct {
	names       []string
	namesErr    error
	updates     []db.IncidentUpdate
	updatesErr  error
	updateCalls int
}

func (f *fakeIncidentDetails) ListServiceNames(context.Context, string) ([]string, error) {
	return f.names, f.namesErr
}

func (f *fakeIncidentDetails) ListUpdatesPaginated(context.Context, string, int, int) ([]db.IncidentUpdate, int, error) {
	f.updateCalls++
	return f.updates, len(f.updates), f.updatesErr
}

func oneOwner() (*fakeMembers, *fakePreferences) {
	return &fakeMembers{members: []db.TenantMember{{UserID: "u1", Email: "owner@example.com", Role: "owner"}}},
		&fakePreferences{enabled: map[string]bool{"u1": true}}
}

func TestNotifyIncidentOpened_EnrichesTenantTimezoneServicesAndDescription(t *testing.T) {
	members, prefs := oneOwner()
	sender := &recordingSender{}
	tz := "America/New_York (GMT-5)"
	tenants := &fakeTenantGetter{tenant: &db.Tenant{Name: "Acme Health", Timezone: &tz}}
	details := &fakeIncidentDetails{names: []string{"api", "web"}}
	svc := newTestService(members, prefs, sender, "https://admin.example.com").WithIncidentDetails(tenants, details)

	opened := time.Date(2026, 9, 10, 20, 2, 0, 0, time.UTC)
	err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", IncidentSummary{
		IncidentID: "inc-1", Title: "API down", Severity: "critical", Status: "investigating",
		OpenedAt: opened, Description: "Error rate above SLO.",
	})
	if err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}

	got := sender.lastOpened
	if got.TenantName != "Acme Health" || got.ServiceName != "api, web" {
		t.Errorf("opened data = %+v, want tenant name and joined service names filled in", got)
	}
	if got.Location == nil || got.Location.String() != "America/New_York" {
		t.Errorf("Location = %v, want the tenant's America/New_York", got.Location)
	}
	if got.OpenedAtText() != "Sep 10 at 4:02 PM" {
		t.Errorf("OpenedAtText() = %q, want the opened time in the tenant zone", got.OpenedAtText())
	}
	if got.FirstUpdateMessage != "Error rate above SLO." || !got.FirstUpdateAt.Equal(opened) {
		t.Errorf("quote box = %q at %s, want the description at the open time", got.FirstUpdateMessage, got.FirstUpdateAt)
	}
	if details.updateCalls != 0 {
		t.Errorf("timeline lookups = %d, want 0 for an opened incident", details.updateCalls)
	}
}

func TestNotifyIncidentOpened_CallerValuesWinOverLookups(t *testing.T) {
	members, prefs := oneOwner()
	sender := &recordingSender{}
	tenants := &fakeTenantGetter{tenant: &db.Tenant{Name: "From DB"}}
	details := &fakeIncidentDetails{names: []string{"from-db"}}
	svc := newTestService(members, prefs, sender, "").WithIncidentDetails(tenants, details)

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}
	if sender.lastOpened.TenantName != "Acme" || sender.lastOpened.ServiceName != "conversations-service" {
		t.Errorf("opened data = %+v, want the caller-supplied tenant/service names kept", sender.lastOpened)
	}
}

func TestNotifyIncidentResolved_UsesLatestAuthoredNote_SkippingSystemEntries(t *testing.T) {
	members, prefs := oneOwner()
	sender := &recordingSender{}
	author := "user-1"
	details := &fakeIncidentDetails{updates: []db.IncidentUpdate{
		{Body: "Status changed to resolved"},
		{Body: "Rolled back the bad deploy.", AuthorID: &author},
		{Body: "Investigating.", AuthorID: &author},
	}}
	svc := newTestService(members, prefs, sender, "").WithIncidentDetails(nil, details)

	opened := time.Date(2026, 9, 8, 12, 15, 0, 0, time.UTC)
	resolved := opened.Add(65 * time.Minute)
	if err := svc.NotifyIncidentResolved(context.Background(), "tenant-1", IncidentSummary{
		IncidentID: "inc-1", Title: "DNS", OpenedAt: opened, ResolvedAt: &resolved,
	}); err != nil {
		t.Fatalf("NotifyIncidentResolved() returned unexpected error: %v", err)
	}

	got := sender.lastResolved
	if got.ResolutionSummary != "Rolled back the bad deploy." {
		t.Errorf("ResolutionSummary = %q, want the latest human-authored note", got.ResolutionSummary)
	}
	if got.TotalDuration() != "1h 05min" || !got.ResolvedAt.Equal(resolved) {
		t.Errorf("TotalDuration() = %q ResolvedAt = %s, want 1h 05min and the resolved time", got.TotalDuration(), got.ResolvedAt)
	}
}

// TestNotifyIncidentResolved_PrefersClosingNoteOverOlderNote mirrors the
// timeline IncidentRepository.Transition writes for a manual resolve with a
// closing note (newest first: the authored note, then the system status
// entry, then older human notes): the email must quote the closing note, not
// the stale "still investigating" note written before the resolution.
func TestNotifyIncidentResolved_PrefersClosingNoteOverOlderNote(t *testing.T) {
	members, prefs := oneOwner()
	sender := &recordingSender{}
	author := "user-1"
	details := &fakeIncidentDetails{updates: []db.IncidentUpdate{
		{Body: "Rolled back the bad deploy; error rate back to baseline.", AuthorID: &author},
		{Body: "Status changed to resolved"},
		{Body: "Still investigating, no root cause yet.", AuthorID: &author},
	}}
	svc := newTestService(members, prefs, sender, "").WithIncidentDetails(nil, details)

	if err := svc.NotifyIncidentResolved(context.Background(), "tenant-1", IncidentSummary{IncidentID: "inc-1", Title: "DNS"}); err != nil {
		t.Fatalf("NotifyIncidentResolved() returned unexpected error: %v", err)
	}
	if got := sender.lastResolved.ResolutionSummary; got != "Rolled back the bad deploy; error rate back to baseline." {
		t.Errorf("ResolutionSummary = %q, want the closing note recorded with the resolve", got)
	}
}

func TestNotifyIncidentResolved_AISummaryCounts_AndLookupFailuresAreNonFatal(t *testing.T) {
	members, prefs := oneOwner()
	sender := &recordingSender{}
	details := &fakeIncidentDetails{updates: []db.IncidentUpdate{{Body: "AI summary.", IsAISummary: true}}, namesErr: errors.New("boom")}
	tenants := &fakeTenantGetter{err: errors.New("db down")}
	svc := newTestService(members, prefs, sender, "").WithIncidentDetails(tenants, details)

	if err := svc.NotifyIncidentResolved(context.Background(), "tenant-1", IncidentSummary{IncidentID: "inc-1", Title: "DNS"}); err != nil {
		t.Fatalf("NotifyIncidentResolved() returned unexpected error: %v (lookups are best-effort)", err)
	}
	if len(sender.resolvedTo) != 1 {
		t.Fatalf("resolvedTo = %v, want the email still sent despite lookup failures", sender.resolvedTo)
	}
	if sender.lastResolved.ResolutionSummary != "AI summary." {
		t.Errorf("ResolutionSummary = %q, want the AI closing summary", sender.lastResolved.ResolutionSummary)
	}
	if sender.lastResolved.Location != nil {
		t.Errorf("Location = %v, want nil (template falls back to the default zone) when the tenant lookup fails", sender.lastResolved.Location)
	}
}

func TestSendWeeklyDigest_FillsDashboardURLFromAdminBaseURL(t *testing.T) {
	sender := &recordingSender{}
	svc := newTestService(&fakeMembers{}, &fakePreferences{}, sender, "https://admin.example.com/")

	if err := svc.SendWeeklyDigest(context.Background(), "owner@example.com", BuildWeeklyDigestData(WeeklyDigestInput{})); err != nil {
		t.Fatalf("SendWeeklyDigest() returned unexpected error: %v", err)
	}
	if sender.lastDigest.DashboardURL != "https://admin.example.com/overview" {
		t.Errorf("DashboardURL = %q, want the overview link", sender.lastDigest.DashboardURL)
	}
}
