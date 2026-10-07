package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testLayout = Layout{
	AssetsBaseURL:        "https://admin.example.com/email",
	NotificationPrefsURL: "https://admin.example.com/profile",
}

func mustParseTemplates(t *testing.T) *templates {
	t.Helper()
	tmpls, err := parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates() returned unexpected error: %v", err)
	}
	return tmpls
}

// dumpRendered writes the rendered HTML to $VANE_EMAIL_RENDER_DIR when set,
// so a reviewer can open each email in a browser and compare it side by side
// with emails-handoff/templates/*.html. A no-op in normal test runs.
func dumpRendered(t *testing.T, name, html string) {
	t.Helper()
	dir := os.Getenv("VANE_EMAIL_RENDER_DIR")
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(html), 0o600); err != nil {
		t.Fatalf("failed to write rendered email: %v", err)
	}
}

// assertCommonLayout checks the invariants every redesigned email must hold
// (email handoff README "Implementation rules" and acceptance checklist).
func assertCommonLayout(t *testing.T, html, text string) {
	t.Helper()
	for _, want := range []string{
		`<img src="https://admin.example.com/email/vane-icon.png" width="30" height="24" alt="Vane"`,
		"<!--[if mso]>",
		"font-family:Arial, Helvetica, 'Segoe UI', sans-serif;",
		"You received this email because your account is associated with Vane.",
		"Sent by Vane · Real-time infrastructure monitoring",
		`<html lang="en"`,
		`<body style="margin:0; padding:0; background:#F1EFF7;">
<div style="display:none;`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(html, "data:image") {
		t.Error("HTML must not embed a data-URI image (Gmail strips them)")
	}
	if strings.Contains(html, `href="#"`) {
		t.Error(`HTML must not contain a dead href="#" link`)
	}
	for _, banned := range []string{"display:flex", "display:grid", "var(--"} {
		if strings.Contains(html, banned) {
			t.Errorf("HTML contains email-unsafe CSS %q", banned)
		}
	}
	for _, want := range []string{"You received this email because your account is associated with Vane.", "Sent by Vane · Real-time infrastructure monitoring"} {
		if !strings.Contains(text, want) {
			t.Errorf("plain text missing footer %q", want)
		}
	}
}

func TestRenderAdminInvite_FullData(t *testing.T) {
	tmpls := mustParseTemplates(t)
	data := AdminInviteEmailData{
		Layout: testLayout, CompanyName: "Acme Health", Role: "owner",
		AcceptURL:   "https://admin.example.com/accept-invite/tok123",
		InviterName: "Ana Silva", InviterEmail: "ana@acme.example", ExpiresIn: "1 hour",
	}
	html, text, err := tmpls.renderAdminInvite(data)
	if err != nil {
		t.Fatalf("renderAdminInvite() error: %v", err)
	}
	dumpRendered(t, "admin_invite", html)
	assertCommonLayout(t, html, text)

	for _, want := range []string{"Ana Silva", "ana@acme.example", "Acme Health", "Admin", "expires in 1 hour", "background:#5A46C7;"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if got := strings.Count(html, `href="https://admin.example.com/accept-invite/tok123"`); got != 2 {
		t.Errorf("accept URL appears in %d hrefs, want 2 (CTA + plain-text fallback link)", got)
	}
	if !strings.Contains(text, "https://admin.example.com/accept-invite/tok123") || !strings.Contains(text, "Role: Admin") {
		t.Errorf("text body missing URL/role:\n%s", text)
	}
	if got := data.Subject(); got != "Ana Silva invited you to join Vane" {
		t.Errorf("Subject() = %q", got)
	}
}

func TestRenderAdminInvite_EscapesUserProvidedValues(t *testing.T) {
	tmpls := mustParseTemplates(t)
	html, _, err := tmpls.renderAdminInvite(AdminInviteEmailData{
		CompanyName: `<script>alert(1)</script>`, InviterName: `"><img src=x>`, Role: "viewer",
		AcceptURL: "javascript:alert(1)",
	})
	if err != nil {
		t.Fatalf("renderAdminInvite() error: %v", err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") || strings.Contains(html, `"><img src=x>`) {
		t.Error("user-provided values must be HTML-escaped")
	}
	if strings.Contains(html, `href="javascript:`) {
		t.Error("a javascript: URL must be neutralized by html/template")
	}
}

func TestRenderAdminInvite_NoLayout_OmitsLogoAndFooterLinks(t *testing.T) {
	tmpls := mustParseTemplates(t)
	html, _, err := tmpls.renderAdminInvite(AdminInviteEmailData{CompanyName: "Acme", Role: "viewer", AcceptURL: "https://x.example/a"})
	if err != nil {
		t.Fatalf("renderAdminInvite() error: %v", err)
	}
	if strings.Contains(html, "<img") {
		t.Error("with no assets base URL the logo <img> must be omitted, not rendered broken")
	}
	if !strings.Contains(html, "Read-only") {
		t.Error("viewer role must render its label")
	}
}

func TestRenderPasswordReset_FullData(t *testing.T) {
	tmpls := mustParseTemplates(t)
	data := PasswordResetEmailData{
		Layout: testLayout, CompanyName: "Acme", ResetURL: "https://admin.example.com/reset-password/r1",
		UserEmail: "ana@acme.example", ExpiresIn: "1 hour",
	}
	html, text, err := tmpls.renderPasswordReset(data)
	if err != nil {
		t.Fatalf("renderPasswordReset() error: %v", err)
	}
	dumpRendered(t, "password_reset", html)
	assertCommonLayout(t, html, text)
	for _, want := range []string{"ana@acme.example", "this link expires in 1 hour", "The link expires in 1 hour."} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if got := strings.Count(html, `href="https://admin.example.com/reset-password/r1"`); got != 2 {
		t.Errorf("reset URL appears in %d hrefs, want 2", got)
	}
	if !strings.Contains(text, "https://admin.example.com/reset-password/r1") {
		t.Errorf("text body missing reset URL:\n%s", text)
	}
}

func TestRenderSignupVerification_LinkOnlyVariant(t *testing.T) {
	tmpls := mustParseTemplates(t)
	data := SignupVerificationEmailData{Layout: testLayout, TenantName: "Acme", VerifyURL: "https://admin.example.com/verify-email/v1", ExpiresIn: "1 hour"}
	html, text, err := tmpls.renderSignupVerification(data)
	if err != nil {
		t.Fatalf("renderSignupVerification() error: %v", err)
	}
	dumpRendered(t, "signup_verification", html)
	assertCommonLayout(t, html, text)
	if strings.Contains(html, "Verification code") || strings.Contains(html, ">or<") {
		t.Error("link-only variant must not render the code block or its 'ou' divider")
	}
	if got := strings.Count(html, `href="https://admin.example.com/verify-email/v1"`); got != 2 {
		t.Errorf("verify URL appears in %d hrefs, want 2", got)
	}
	if !strings.Contains(text, "https://admin.example.com/verify-email/v1") {
		t.Errorf("text body missing verify URL:\n%s", text)
	}
}

func TestRenderIncidentOpened_CriticalIsRed_ModerateIsAmber(t *testing.T) {
	tmpls := mustParseTemplates(t)
	sp := TenantLocation(strPtr("America/Sao_Paulo (GMT-3)"))
	opened := time.Date(2026, 9, 10, 19, 2, 0, 0, time.UTC) // 4:02 PM in America/Sao_Paulo
	data := IncidentOpenedEmailData{
		Layout: testLayout, TenantName: "Acme Health", ServiceName: "Webhook Receiver",
		IncidentTitle: "Webhook Receiver down", Severity: "critical", Status: "investigating",
		OpenedAt: opened, Location: sp, FirstUpdateAt: opened,
		FirstUpdateMessage: "6 consecutive check failures", DashboardURL: "https://admin.example.com/incidents/i2",
	}
	html, text, err := tmpls.renderIncidentOpened(data)
	if err != nil {
		t.Fatalf("renderIncidentOpened() error: %v", err)
	}
	dumpRendered(t, "incident_opened", html)
	assertCommonLayout(t, html, text)
	for _, want := range []string{
		"background:#D6395B;", "CRITICAL", "Opened Sep 10 at 4:02 PM · Organization Acme Health",
		"<strong>4:02 PM</strong> — 6 consecutive check failures", "Investigating", "Webhook Receiver",
		`href="https://admin.example.com/incidents/i2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(html, ">Duration<") {
		t.Error("duration row must be omitted at open time")
	}
	if got := data.Subject(); got != "[Critical] New incident: Webhook Receiver down" {
		t.Errorf("Subject() = %q", got)
	}

	data.Severity = "moderate"
	data.FirstUpdateMessage = ""
	html, _, err = tmpls.renderIncidentOpened(data)
	if err != nil {
		t.Fatalf("renderIncidentOpened() error: %v", err)
	}
	if !strings.Contains(html, "background:#B45309;") || strings.Contains(html, "#D6395B") {
		t.Error("moderate severity must use the amber accent, never the critical red")
	}
	if !strings.Contains(html, "MODERATE") {
		t.Error("moderate severity badge missing")
	}
	if strings.Contains(html, "border-left:3px solid") {
		t.Error("quote box must be hidden when there is no update message")
	}
}

func TestRenderIncidentResolved_FullData_AndEmptySummaryHidesBox(t *testing.T) {
	tmpls := mustParseTemplates(t)
	sp := TenantLocation(strPtr("America/Sao_Paulo (GMT-3)"))
	data := IncidentResolvedEmailData{
		Layout: testLayout, TenantName: "Acme Health", ServiceName: "painel.acme.health",
		IncidentTitle: "Intermittent DNS failure", Severity: "minor",
		OpenedAt:   time.Date(2026, 9, 8, 12, 15, 0, 0, time.UTC),
		ResolvedAt: time.Date(2026, 9, 8, 13, 20, 0, 0, time.UTC),
		Location:   sp, ResolutionSummary: "TTL adjusted.", DashboardURL: "https://admin.example.com/incidents/i3",
	}
	html, text, err := tmpls.renderIncidentResolved(data)
	if err != nil {
		t.Fatalf("renderIncidentResolved() error: %v", err)
	}
	dumpRendered(t, "incident_resolved", html)
	assertCommonLayout(t, html, text)
	for _, want := range []string{
		"background:#3DD68C;", "INCIDENT RESOLVED", "Resolved Sep 8 at 10:20 AM · Organization Acme Health",
		"TTL adjusted.", "1h 05min", "Sep 8, 9:15 AM", "Sep 8, 10:20 AM",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if got := data.Subject(); got != "Resolved: Intermittent DNS failure" {
		t.Errorf("Subject() = %q", got)
	}

	data.ResolutionSummary = ""
	html, _, err = tmpls.renderIncidentResolved(data)
	if err != nil {
		t.Fatalf("renderIncidentResolved() error: %v", err)
	}
	if strings.Contains(html, "border-left:3px solid") {
		t.Error("quote box must be hidden when the resolution summary is empty")
	}
}

func TestRenderWeeklyDigest_ListsServicesAndIncidents(t *testing.T) {
	tmpls := mustParseTemplates(t)
	end := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	data := WeeklyDigestEmailData{
		Layout: testLayout, TenantName: "Acme Health", UptimePercent: 99.94,
		PeriodStart: end.Add(-7 * 24 * time.Hour), PeriodEnd: end, Location: DefaultLocation(),
		Services: []DigestServiceRow{
			{Name: "API Gateway", UptimePercent: 99.99},
			{Name: "Billing Worker", UptimePercent: 99.42},
		},
		TotalServices: 12,
		Incidents: []DigestIncidentRow{
			{Title: "High latency", ServiceName: "Billing Worker", Status: "monitoring", Duration: 130 * time.Minute},
			{Title: "Webhook down", ServiceName: "Webhook Receiver", Status: "resolved", Duration: 38 * time.Minute},
		},
		MTTR: 34 * time.Minute, DashboardURL: "https://admin.example.com/overview",
	}
	html, text, err := tmpls.renderWeeklyDigest(data)
	if err != nil {
		t.Fatalf("renderWeeklyDigest() error: %v", err)
	}
	dumpRendered(t, "weekly_digest", html)
	assertCommonLayout(t, html, text)
	for _, want := range []string{
		"Sep 8–14, 2026 · Acme Health", "99.94%", ">2<", "34min",
		`<td width="100" style="height:6px; line-height:6px; font-size:0; background:#3DD68C;`,
		`<td width="99" style="height:6px; line-height:6px; font-size:0; background:#B45309;`,
		"Degraded this week", "MONITORING", "RESOLVED", "2h 10min", "10 more services",
		"Change how often you get these emails", "Unsubscribe", `href="https://admin.example.com/overview"`,
		"Weekly summary: 99.94% average uptime, 2 incidents, 34min MTTR.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	// Last row of each list has no bottom border: 2 services + 2 incidents
	// = 4 rows, 2 cells each, minus the 2 last rows' cells = 4 bordered cells.
	if got := strings.Count(html, "padding:13px 0; border-bottom:1px solid #ECE9F2;"); got != 4 {
		t.Errorf("bordered list cells = %d, want 4 (last row of each list unbordered)", got)
	}
	if !strings.Contains(text, "Billing Worker: 99.42% (Degraded this week)") {
		t.Errorf("text body missing service row:\n%s", text)
	}
}

func TestFormatPeriod(t *testing.T) {
	sp := DefaultLocation()
	cases := []struct {
		end  time.Time
		loc  *time.Location
		want string
	}{
		{time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), sp, "Sep 8–14, 2026"},
		{time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), time.UTC, "Sep 8–14, 2026"},
		{time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), sp, "Sep 28 – Oct 4, 2026"},
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), sp, "Dec 29, 2025 – Jan 4, 2026"},
	}
	for _, c := range cases {
		if got := formatPeriod(c.end, c.loc); got != c.want {
			t.Errorf("formatPeriod(%s, %s) = %q, want %q", c.end, c.loc, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                "less than 1min",
		38 * time.Minute:                 "38min",
		65 * time.Minute:                 "1h 05min",
		2 * time.Hour:                    "2h",
		27 * time.Hour:                   "1d 3h",
		48 * time.Hour:                   "2d",
		2*time.Hour + 10*time.Minute + 5: "2h 10min",
	}
	for d, want := range cases {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestFormatTTL(t *testing.T) {
	cases := map[time.Duration]string{
		time.Hour:          "1 hour",
		2 * time.Hour:      "2 hours",
		15 * time.Minute:   "15 minutes",
		7 * 24 * time.Hour: "7 days",
		24 * time.Hour:     "1 day",
	}
	for d, want := range cases {
		if got := FormatTTL(d); got != want {
			t.Errorf("FormatTTL(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestTenantLocation(t *testing.T) {
	if got := TenantLocation(strPtr("America/New_York (GMT-5)")).String(); got != "America/New_York" {
		t.Errorf("TenantLocation(New_York label) = %q", got)
	}
	if got := TenantLocation(strPtr("UTC (GMT+0)")).String(); got != "UTC" {
		t.Errorf("TenantLocation(UTC label) = %q", got)
	}
	if got := TenantLocation(nil); got != DefaultLocation() {
		t.Errorf("TenantLocation(nil) = %v, want DefaultLocation", got)
	}
	if got := TenantLocation(strPtr("Not/AZone")); got != DefaultLocation() {
		t.Errorf("TenantLocation(invalid) = %v, want DefaultLocation", got)
	}
}

func TestDigestServiceRow_BarRule(t *testing.T) {
	cases := []struct {
		uptime       float64
		filled       int
		healthyColor bool
	}{
		{99.95, 100, true},
		{99.9, 100, true},
		{99.6, 100, true},
		{99.42, 99, false},
		{80.4, 80, false},
	}
	for _, c := range cases {
		r := DigestServiceRow{UptimePercent: c.uptime}
		if r.BarFilled() != c.filled || r.BarFilled()+r.BarEmpty() != 100 {
			t.Errorf("uptime %.2f: filled=%d empty=%d, want filled=%d summing to 100", c.uptime, r.BarFilled(), r.BarEmpty(), c.filled)
		}
		if (r.BarColor() == colorSuccessDot) != c.healthyColor {
			t.Errorf("uptime %.2f: bar color %s, healthy=%v", c.uptime, r.BarColor(), c.healthyColor)
		}
	}
}

func strPtr(s string) *string { return &s }
