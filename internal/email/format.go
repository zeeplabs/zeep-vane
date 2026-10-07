package email

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Layout is the deployment-wide chrome every redesigned transactional email
// shares (logo, notification-preferences link). It is not per-email data:
// the Sender implementation owns it (set once at boot from config via
// SetLayout) and stamps it onto each *EmailData value right before
// rendering, so no call site has to know about these URLs.
//
// Every field is optional; the templates omit the matching element (logo
// image, preferences link) when it is empty rather than rendering a broken
// image or a dead "#" link.
type Layout struct {
	// AssetsBaseURL is the absolute base URL the email logo
	// (vane-icon.png) is served under. Absolute on purpose: Gmail and
	// several ESPs strip data-URI images.
	AssetsBaseURL string
	// NotificationPrefsURL is the weekly digest's "Change how often you get
	// these emails" / "Unsubscribe" link - the Profile screen, where the
	// Notifications section lives.
	NotificationPrefsURL string
}

// logoFileName is the logo's file name under AssetsBaseURL - the same file
// shipped at web/public/email/vane-icon.png.
const logoFileName = "vane-icon.png"

// LogoURL returns the absolute logo URL, or "" when no assets base URL is
// configured (the template then renders the wordmark without an image).
func (l Layout) LogoURL() string {
	if l.AssetsBaseURL == "" {
		return ""
	}
	return strings.TrimRight(l.AssetsBaseURL, "/") + "/" + logoFileName
}

// Design tokens from the email handoff (README "Design tokens"), used by the
// data-struct methods that pick a color per severity/status. Kept as Go
// constants rather than template literals only where the color depends on
// data; static colors stay literal in the templates.
const (
	colorPurple       = "#5A46C7"
	colorCritical     = "#D6395B"
	colorCriticalBg   = "#FBE9ED"
	colorWarning      = "#B45309"
	colorWarningBg    = "#FEF3E2"
	colorSuccessDot   = "#3DD68C"
	colorSuccessText  = "#146C4A"
	colorSuccessBg    = "#E7F6EF"
	colorBarEmptyCell = "#ECE9F2"
)

// defaultTimezoneName is the product's default display timezone - the same
// fixed zone the public status page and the overview handler already
// render history in (internal/api/overview_handler.go).
const defaultTimezoneName = "America/Sao_Paulo"

var (
	defaultLocationOnce sync.Once
	defaultLocation     *time.Location
)

// DefaultLocation returns America/Sao_Paulo, or UTC if tzdata is somehow
// unavailable (cmd/vane blank-imports time/tzdata, so this fallback is only
// reachable in an unusual test environment).
func DefaultLocation() *time.Location {
	defaultLocationOnce.Do(func() {
		loc, err := time.LoadLocation(defaultTimezoneName)
		if err != nil {
			loc = time.UTC
		}
		defaultLocation = loc
	})
	return defaultLocation
}

// TenantLocation resolves a tenant's stored timezone setting to a
// *time.Location. tenants.timezone stores the settings-page label verbatim
// (e.g. "America/Sao_Paulo (GMT-3)", see validTimezones in
// internal/api/company_settings_handler.go), so the IANA name is the part
// before " (". A nil, empty, or unloadable value falls back to
// DefaultLocation.
func TenantLocation(timezone *string) *time.Location {
	if timezone == nil {
		return DefaultLocation()
	}
	name := strings.TrimSpace(*timezone)
	if i := strings.Index(name, " ("); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return DefaultLocation()
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return DefaultLocation()
	}
	return loc
}

func locOrDefault(loc *time.Location) *time.Location {
	if loc == nil {
		return DefaultLocation()
	}
	return loc
}

// formatDateTimeAt renders t as "Sep 10 at 4:02 PM" in loc. 12-hour clock
// with AM/PM, matching what the admin SPA's English locale renders for
// timestamps (web/src/lib/formatDate.ts uses toLocaleString with "en").
func formatDateTimeAt(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	lt := t.In(locOrDefault(loc))
	return lt.Format("Jan 2 at 3:04 PM")
}

// formatDateTimeComma renders t as "Sep 8, 9:15 AM" in loc (the
// detail-table variant used by incident_resolved).
func formatDateTimeComma(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	lt := t.In(locOrDefault(loc))
	return lt.Format("Jan 2, 3:04 PM")
}

// formatClock renders t as "4:02 PM" in loc.
func formatClock(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	lt := t.In(locOrDefault(loc))
	return lt.Format("3:04 PM")
}

// formatPeriod renders a 7-day digest window ending at end (exclusive) as
// "Sep 8–14, 2026" in loc. The digest window is aligned to Monday 00:00 UTC,
// which in a non-UTC zone falls mid-evening/early-morning; anchoring the
// last day on end-12h makes the label name the 7 calendar days the window
// mostly covers for any zone within UTC±12, instead of printing an 8-day
// span. Cross-month and cross-year windows render as "Sep 28 – Oct 4, 2026"
// and "Dec 29, 2025 – Jan 4, 2026".
func formatPeriod(end time.Time, loc *time.Location) string {
	if end.IsZero() {
		return ""
	}
	last := end.Add(-12 * time.Hour).In(locOrDefault(loc))
	last = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, last.Location())
	first := last.AddDate(0, 0, -6)

	switch {
	case first.Year() != last.Year():
		return first.Format("Jan 2, 2006") + " – " + last.Format("Jan 2, 2006")
	case first.Month() != last.Month():
		return first.Format("Jan 2") + " – " + last.Format("Jan 2, 2006")
	default:
		return fmt.Sprintf("%s %d–%d, %d", first.Format("Jan"), first.Day(), last.Day(), last.Year())
	}
}

// formatDuration renders d in the handoff's compact style: "38min",
// "1h 05min", "2h", "1d 3h". A sub-minute duration renders as
// "less than 1min" so a just-opened-and-resolved incident never reads
// "0min".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "less than 1min"
	}
	totalMinutes := int(d / time.Minute)
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes % (24 * 60)) / 60
	minutes := totalMinutes % 60

	switch {
	case days > 0:
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %02dmin", hours, minutes)
	default:
		return fmt.Sprintf("%dmin", minutes)
	}
}

// formatTTL renders a token TTL as reader-facing copy ("1 hour", "2 hours",
// "15 minutes", "7 days"), so the "expires in ..." line always matches the
// real backend TTL instead of a hardcoded sample value.
func formatTTL(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%(24*time.Hour) == 0:
		return pluralize(int(d/(24*time.Hour)), "day")
	case d%time.Hour == 0:
		return pluralize(int(d/time.Hour), "hour")
	default:
		minutes := int(d / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		return pluralize(minutes, "minute")
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// FormatTTL is formatTTL exported for the API handlers that build email
// data from their own token TTL constants.
func FormatTTL(d time.Duration) string { return formatTTL(d) }

// roleLabels mirrors the English role labels the Users screen shows
// (web/src/locales/en.json admins.roleOptions), so the role named in the
// invite email is the same word the inviter picked in the UI.
var roleLabels = map[string]string{
	"owner":    "Admin",
	"operator": "Member",
	"viewer":   "Read-only",
}

// severityLabels mirrors web/src/locales/en.json's incident severity labels
// (INCSEV-01: minor/moderate/critical).
var severityLabels = map[string]string{
	"minor":    "Minor",
	"moderate": "Moderate",
	"critical": "Critical",
}

// incidentStatusLabels mirrors web/src/locales/en.json's incident status
// labels.
var incidentStatusLabels = map[string]string{
	"investigating": "Investigating",
	"identified":    "Identified",
	"monitoring":    "Monitoring",
	"resolved":      "Resolved",
}

func labelOr(labels map[string]string, key string) string {
	if label, ok := labels[key]; ok {
		return label
	}
	return key
}
