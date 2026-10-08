package cli

import (
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("time.LoadLocation(%q) returned unexpected error: %v", name, err)
	}
	return loc
}

func strPtr(s string) *string { return &s }

func timePtr(t time.Time) *time.Time { return &t }

// longAgo is a created_at old enough to never gate eligibility on its own.
var longAgo = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// TestNextDigestTick replaces the old nextDigestFire coverage: the scheduler
// now ticks hourly, digestTickOffset past each hour, always strictly after now.
func TestNextDigestTick(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"top of hour", time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC)},
		{"exactly on a tick", time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC), time.Date(2026, 9, 7, 13, 1, 0, 0, time.UTC)},
		{"mid hour", time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC), time.Date(2026, 9, 7, 13, 1, 0, 0, time.UTC)},
		{"crosses day", time.Date(2026, 9, 6, 23, 59, 0, 0, time.UTC), time.Date(2026, 9, 7, 0, 1, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDigestTick(tc.now); !got.Equal(tc.want) {
				t.Errorf("nextDigestTick(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

// TestWeeklyDigestFire covers the per-zone Monday 09:00 computation,
// including a DST week (US fall-back on 2026-11-01 must still yield 09:00
// local the following Monday).
func TestWeeklyDigestFire(t *testing.T) {
	saoPaulo := mustLoadLocation(t, "America/Sao_Paulo")
	newYork := mustLoadLocation(t, "America/New_York")
	cases := []struct {
		name string
		now  time.Time
		loc  *time.Location
		want time.Time
	}{
		{"monday after fire", time.Date(2026, 9, 7, 10, 0, 0, 0, saoPaulo), saoPaulo, time.Date(2026, 9, 7, 9, 0, 0, 0, saoPaulo)},
		{"monday exactly at fire", time.Date(2026, 9, 7, 9, 0, 0, 0, saoPaulo), saoPaulo, time.Date(2026, 9, 7, 9, 0, 0, 0, saoPaulo)},
		{"monday before fire is last week's", time.Date(2026, 9, 7, 8, 59, 0, 0, saoPaulo), saoPaulo, time.Date(2026, 8, 31, 9, 0, 0, 0, saoPaulo)},
		{"sunday is last week's", time.Date(2026, 9, 13, 23, 0, 0, 0, saoPaulo), saoPaulo, time.Date(2026, 9, 7, 9, 0, 0, 0, saoPaulo)},
		{"after DST fall-back", time.Date(2026, 11, 2, 9, 30, 0, 0, newYork), newYork, time.Date(2026, 11, 2, 9, 0, 0, 0, newYork)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := weeklyDigestFire(tc.now, tc.loc)
			if !got.Equal(tc.want) {
				t.Errorf("weeklyDigestFire(%s) = %s, want %s", tc.now, got, tc.want)
			}
			if h := got.In(tc.loc).Hour(); h != digestFireHour {
				t.Errorf("fire local hour = %d, want %d", h, digestFireHour)
			}
		})
	}
}

// TestDigestDue_DifferentZonesFireAtDifferentInstants: on the same Monday,
// a Sao Paulo tenant (UTC-3) is due from 12:00 UTC, a New York tenant
// (UTC-4 in September) only from 13:00 UTC.
func TestDigestDue_DifferentZonesFireAtDifferentInstants(t *testing.T) {
	sp := db.Tenant{ID: "sp", Timezone: strPtr("America/Sao_Paulo"), CreatedAt: longAgo}
	ny := db.Tenant{ID: "ny", Timezone: strPtr("America/New_York (GMT-4)"), CreatedAt: longAgo}

	at1201 := time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC)
	at1301 := time.Date(2026, 9, 7, 13, 1, 0, 0, time.UTC)

	if fire, due := digestDue(sp, at1201); !due || !fire.Equal(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("sao paulo at 12:01 UTC: due=%v fire=%s, want due at 12:00 UTC", due, fire)
	}
	if _, due := digestDue(ny, at1201); due {
		t.Error("new york at 12:01 UTC: due=true, want false (08:01 local)")
	}
	if fire, due := digestDue(ny, at1301); !due || !fire.Equal(time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("new york at 13:01 UTC: due=%v fire=%s, want due at 13:00 UTC", due, fire)
	}
}

func TestDigestDue_Eligibility(t *testing.T) {
	sp := "America/Sao_Paulo"
	fire := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) // Monday 09:00 Sao Paulo
	cases := []struct {
		name   string
		tenant db.Tenant
		now    time.Time
		want   bool
	}{
		{"never sent, inside window", db.Tenant{Timezone: &sp, CreatedAt: longAgo}, fire.Add(time.Minute), true},
		{"already sent this week", db.Tenant{Timezone: &sp, CreatedAt: longAgo, LastDigestSentAt: timePtr(fire.Add(time.Minute))}, fire.Add(time.Hour + time.Minute), false},
		{"last sent last week fires again", db.Tenant{Timezone: &sp, CreatedAt: longAgo, LastDigestSentAt: timePtr(fire.AddDate(0, 0, -7).Add(time.Minute))}, fire.Add(time.Minute), true},
		{"missed ticks still caught up a few hours later", db.Tenant{Timezone: &sp, CreatedAt: longAgo, LastDigestSentAt: timePtr(fire.AddDate(0, 0, -7))}, fire.Add(5 * time.Hour), true},
		{"past the catch-up window skips the week", db.Tenant{Timezone: &sp, CreatedAt: longAgo}, fire.Add(digestCatchUpWindow), false},
		{"not anyone's monday window (wednesday)", db.Tenant{Timezone: &sp, CreatedAt: longAgo}, time.Date(2026, 9, 9, 12, 1, 0, 0, time.UTC), false},
		{"monday before local 09:00", db.Tenant{Timezone: &sp, CreatedAt: longAgo}, fire.Add(-time.Minute), false},
		{"tenant created after this week's fire waits", db.Tenant{Timezone: &sp, CreatedAt: fire.Add(30 * time.Minute)}, fire.Add(time.Hour), false},
		{"nil timezone uses the default zone (Sao Paulo)", db.Tenant{CreatedAt: longAgo}, fire.Add(time.Minute), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := digestDue(tc.tenant, tc.now); got != tc.want {
				t.Errorf("digestDue() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDigestPeriod: the digest summarizes the previous tenant-local calendar
// week (Mon 00:00 - Mon 00:00 local), whatever tick in the catch-up window
// actually sends it.
func TestDigestPeriod(t *testing.T) {
	sp := mustLoadLocation(t, "America/Sao_Paulo")
	fire := time.Date(2026, 9, 7, 9, 0, 0, 0, sp)

	from, to := digestPeriod(fire, sp)
	wantFrom := time.Date(2026, 8, 31, 0, 0, 0, 0, sp)
	wantTo := time.Date(2026, 9, 7, 0, 0, 0, 0, sp)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Errorf("digestPeriod() = %s..%s, want %s..%s", from, to, wantFrom, wantTo)
	}
}
