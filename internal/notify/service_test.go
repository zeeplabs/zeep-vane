package notify

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

type fakeMembers struct {
	members []db.TenantMember
	err     error
}

func (f *fakeMembers) ListMembersWithEmail(context.Context, string) ([]db.TenantMember, error) {
	return f.members, f.err
}

type fakePreferences struct {
	enabled  map[string]bool
	err      error
	lastType string
	calls    int
}

func (f *fakePreferences) ResolveEnabledForUsers(_ context.Context, _ []string, notificationType string) (map[string]bool, error) {
	f.calls++
	f.lastType = notificationType
	if f.err != nil {
		return nil, f.err
	}
	return f.enabled, nil
}

type recordingSender struct {
	openedTo     []string
	resolvedTo   []string
	digestTo     []string
	expiringTo   []string
	driftTo      []string
	lastOpened   email.IncidentOpenedEmailData
	lastResolved email.IncidentResolvedEmailData
	lastDigest   email.WeeklyDigestEmailData
	lastExpiring email.DomainExpiringEmailData
	lastDrift    email.DomainNSDriftEmailData
	sendErrFor   map[string]error
}

func (s *recordingSender) SendWeeklyDigest(_ context.Context, to string, data email.WeeklyDigestEmailData) error {
	if err := s.sendErrFor[to]; err != nil {
		return err
	}
	s.digestTo = append(s.digestTo, to)
	s.lastDigest = data
	return nil
}

func (s *recordingSender) SendDomainExpiring(_ context.Context, to string, data email.DomainExpiringEmailData) error {
	if err := s.sendErrFor[to]; err != nil {
		return err
	}
	s.expiringTo = append(s.expiringTo, to)
	s.lastExpiring = data
	return nil
}

func (s *recordingSender) SendDomainNSDrift(_ context.Context, to string, data email.DomainNSDriftEmailData) error {
	if err := s.sendErrFor[to]; err != nil {
		return err
	}
	s.driftTo = append(s.driftTo, to)
	s.lastDrift = data
	return nil
}

func (s *recordingSender) SendIncidentOpened(_ context.Context, to string, data email.IncidentOpenedEmailData) error {
	if err := s.sendErrFor[to]; err != nil {
		return err
	}
	s.openedTo = append(s.openedTo, to)
	s.lastOpened = data
	return nil
}

func (s *recordingSender) SendIncidentResolved(_ context.Context, to string, data email.IncidentResolvedEmailData) error {
	if err := s.sendErrFor[to]; err != nil {
		return err
	}
	s.resolvedTo = append(s.resolvedTo, to)
	s.lastResolved = data
	return nil
}

func newTestService(members memberLister, prefs preferenceResolver, sender emailSender, baseURL string) *Service {
	return NewService(members, prefs, sender, baseURL, zap.NewNop())
}

func summary() IncidentSummary {
	return IncidentSummary{
		IncidentID:  "inc-1",
		Title:       "Elevated error rate",
		Severity:    "critical",
		ServiceName: "conversations-service",
		TenantName:  "Acme",
	}
}

// TestNotifyIncidentOpened_SendsOnlyToEnabledOwnerOperator covers
// NOTIFPREF-04 and NOTIFPREF-06: exactly the owner/operator members whose
// preference is enabled receive the email - never a viewer, regardless of that
// viewer's preference.
func TestNotifyIncidentOpened_SendsOnlyToEnabledOwnerOperator(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-on", Email: "owner-on@example.com", Role: "owner"},
		{UserID: "operator-off", Email: "operator-off@example.com", Role: "operator"},
		{UserID: "viewer-on", Email: "viewer-on@example.com", Role: "viewer"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{
		"owner-on": true, "operator-off": false, "viewer-on": true,
	}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}

	if len(sender.openedTo) != 1 || sender.openedTo[0] != "owner-on@example.com" {
		t.Errorf("openedTo = %v, want exactly [owner-on@example.com]", sender.openedTo)
	}
	// The viewer must never be consulted for a preference: the resolver only
	// receives the owner/operator candidate set.
	if prefs.lastType != db.NotificationTypeIncidentOpened {
		t.Errorf("resolved notification type = %q, want %q", prefs.lastType, db.NotificationTypeIncidentOpened)
	}
}

// TestNotifyIncidentOpened_NoOwnerOperator_NoSend proves a tenant whose only
// members are viewers gets no email and no preference lookup.
func TestNotifyIncidentOpened_NoOwnerOperator_NoSend(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "viewer", Email: "viewer@example.com", Role: "viewer"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"viewer": true}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}
	if len(sender.openedTo) != 0 {
		t.Errorf("openedTo = %v, want none", sender.openedTo)
	}
	if prefs.calls != 0 {
		t.Errorf("preference resolver calls = %d, want 0 (no eligible recipients)", prefs.calls)
	}
}

// TestNotifyIncidentOpened_AllDisabled_NoSend covers the opted-out case.
func TestNotifyIncidentOpened_AllDisabled_NoSend(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": false}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}
	if len(sender.openedTo) != 0 {
		t.Errorf("openedTo = %v, want none (recipient opted out)", sender.openedTo)
	}
}

// TestNotifyIncidentOpened_SendFailureForOneRecipient_ContinuesAndReturnsNil
// covers NOTIFPREF-05: a send failure for one recipient is swallowed (logged),
// the other recipients still get their email, and the call returns nil so the
// incident action is never failed by email delivery.
func TestNotifyIncidentOpened_SendFailureForOneRecipient_ContinuesAndReturnsNil(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-a", Email: "owner-a@example.com", Role: "owner"},
		{UserID: "owner-b", Email: "owner-b@example.com", Role: "operator"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner-a": true, "owner-b": true}}
	sender := &recordingSender{sendErrFor: map[string]error{"owner-a@example.com": errors.New("smtp down")}}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v, want nil despite one send failure", err)
	}
	if len(sender.openedTo) != 1 || sender.openedTo[0] != "owner-b@example.com" {
		t.Errorf("openedTo = %v, want [owner-b@example.com] - the failing recipient must not block the rest", sender.openedTo)
	}
}

// TestNotifyIncidentOpened_MemberListError_Returned covers the lookup failure
// path (the caller logs and ignores it, never failing the incident action).
func TestNotifyIncidentOpened_MemberListError_Returned(t *testing.T) {
	lookupErr := errors.New("db down")
	members := &fakeMembers{err: lookupErr}
	svc := newTestService(members, &fakePreferences{}, &recordingSender{}, "")

	err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary())
	if !errors.Is(err, lookupErr) {
		t.Fatalf("NotifyIncidentOpened() error = %v, want wrapped %v", err, lookupErr)
	}
}

// TestNotifyIncidentOpened_PreferenceResolverError_Returned covers the
// preference lookup failure path.
func TestNotifyIncidentOpened_PreferenceResolverError_Returned(t *testing.T) {
	lookupErr := errors.New("prefs down")
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{err: lookupErr}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary())
	if !errors.Is(err, lookupErr) {
		t.Fatalf("NotifyIncidentOpened() error = %v, want wrapped %v", err, lookupErr)
	}
	if len(sender.openedTo) != 0 {
		t.Errorf("openedTo = %v, want none when preferences cannot be resolved", sender.openedTo)
	}
}

// TestNotifyIncidentResolved_UsesResolvedPreferenceAndTemplate covers
// NOTIFPREF-07: the resolved path resolves the incident_resolved preference and
// sends the resolved template.
func TestNotifyIncidentResolved_UsesResolvedPreferenceAndTemplate(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": true}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentResolved(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentResolved() returned unexpected error: %v", err)
	}
	if prefs.lastType != db.NotificationTypeIncidentResolved {
		t.Errorf("resolved notification type = %q, want %q", prefs.lastType, db.NotificationTypeIncidentResolved)
	}
	if len(sender.resolvedTo) != 1 || sender.resolvedTo[0] != "owner@example.com" {
		t.Errorf("resolvedTo = %v, want [owner@example.com]", sender.resolvedTo)
	}
	if len(sender.openedTo) != 0 {
		t.Errorf("openedTo = %v, want none (only the resolved template must be sent)", sender.openedTo)
	}
}

// TestNotifyIncidentOpened_BuildsDashboardURLFromIncidentID covers the link
// assembly from the configured admin base URL.
func TestNotifyIncidentOpened_BuildsDashboardURLFromIncidentID(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": true}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "https://vane.example.com/")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}
	if sender.lastOpened.DashboardURL != "https://vane.example.com/incidents/inc-1" {
		t.Errorf("DashboardURL = %q, want %q", sender.lastOpened.DashboardURL, "https://vane.example.com/incidents/inc-1")
	}
	if sender.lastOpened.IncidentTitle != "Elevated error rate" || sender.lastOpened.Severity != "critical" {
		t.Errorf("opened data = %+v, want the summary's title and severity", sender.lastOpened)
	}
}

// TestNotifyIncidentOpened_EmptyAdminBaseURL_NoDashboardLink proves the link
// is optional: an empty base URL yields an empty DashboardURL rather than a
// malformed one.
func TestNotifyIncidentOpened_EmptyAdminBaseURL_NoDashboardLink(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": true}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyIncidentOpened(context.Background(), "tenant-1", summary()); err != nil {
		t.Fatalf("NotifyIncidentOpened() returned unexpected error: %v", err)
	}
	if sender.lastOpened.DashboardURL != "" {
		t.Errorf("DashboardURL = %q, want empty when no admin base URL is configured", sender.lastOpened.DashboardURL)
	}
}

// TestWeeklyDigestRecipients_OnlyEnabledOwnerOperator covers NOTIFPREF-10: the
// recipient list is owner/operator members with weekly_digest enabled - never a
// viewer, never an opted-out owner.
func TestWeeklyDigestRecipients_OnlyEnabledOwnerOperator(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-on", Email: "owner-on@example.com", Role: "owner"},
		{UserID: "operator-off", Email: "operator-off@example.com", Role: "operator"},
		{UserID: "viewer-on", Email: "viewer-on@example.com", Role: "viewer"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{
		"owner-on": true, "operator-off": false, "viewer-on": true,
	}}
	svc := newTestService(members, prefs, &recordingSender{}, "")

	recipients, err := svc.WeeklyDigestRecipients(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("WeeklyDigestRecipients() returned unexpected error: %v", err)
	}
	if len(recipients) != 1 || recipients[0].Email != "owner-on@example.com" {
		t.Errorf("recipients = %+v, want exactly [owner-on@example.com]", recipients)
	}
	if prefs.lastType != db.NotificationTypeWeeklyDigest {
		t.Errorf("resolved notification type = %q, want %q", prefs.lastType, db.NotificationTypeWeeklyDigest)
	}
}

// TestWeeklyDigestRecipients_NoEligible_EmptyList covers NOTIFPREF-12: a tenant
// with no owner/operator opted into the digest yields an empty list (the
// scheduler then sends nothing for that tenant).
func TestWeeklyDigestRecipients_NoEligible_EmptyList(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-off", Email: "owner-off@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner-off": false}}
	svc := newTestService(members, prefs, &recordingSender{}, "")

	recipients, err := svc.WeeklyDigestRecipients(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("WeeklyDigestRecipients() returned unexpected error: %v", err)
	}
	if len(recipients) != 0 {
		t.Errorf("recipients = %+v, want empty when nobody opted in", recipients)
	}
}

func domainExpiringSummary() DomainExpiringSummary {
	return DomainExpiringSummary{
		DomainID:      "dom-1",
		Hostname:      "starbem.app",
		DaysRemaining: 6,
		ThresholdDays: 7,
		TenantName:    "Acme",
	}
}

func domainNSDriftSummary() DomainNSDriftSummary {
	return DomainNSDriftSummary{
		DomainID:   "dom-1",
		Hostname:   "starbem.app",
		ExpectedNS: []string{"ns1.old.net", "ns2.old.net"},
		CurrentNS:  []string{"ns1.new.net"},
		TenantName: "Acme",
	}
}

// TestNotifyDomainExpiring_SendsOnlyToEnabledOwnerOperator covers DHM-02's
// notification path: exactly the owner/operator members whose domain_expiring
// preference is enabled receive the email, carrying the domain/threshold data
// and the domains dashboard link.
func TestNotifyDomainExpiring_SendsOnlyToEnabledOwnerOperator(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-on", Email: "owner-on@example.com", Role: "owner"},
		{UserID: "operator-off", Email: "operator-off@example.com", Role: "operator"},
		{UserID: "viewer-on", Email: "viewer-on@example.com", Role: "viewer"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{
		"owner-on": true, "operator-off": false, "viewer-on": true,
	}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "https://vane.example.com")

	if err := svc.NotifyDomainExpiring(context.Background(), "tenant-1", domainExpiringSummary()); err != nil {
		t.Fatalf("NotifyDomainExpiring() returned unexpected error: %v", err)
	}
	if len(sender.expiringTo) != 1 || sender.expiringTo[0] != "owner-on@example.com" {
		t.Errorf("expiringTo = %v, want exactly [owner-on@example.com]", sender.expiringTo)
	}
	if prefs.lastType != db.NotificationTypeDomainExpiring {
		t.Errorf("resolved notification type = %q, want %q", prefs.lastType, db.NotificationTypeDomainExpiring)
	}
	if sender.lastExpiring.Hostname != "starbem.app" || sender.lastExpiring.DaysRemaining != 6 || sender.lastExpiring.ThresholdDays != 7 {
		t.Errorf("expiring data = %+v, want hostname=starbem.app days=6 threshold=7", sender.lastExpiring)
	}
	if sender.lastExpiring.DashboardURL != "https://vane.example.com/domains" {
		t.Errorf("DashboardURL = %q, want %q", sender.lastExpiring.DashboardURL, "https://vane.example.com/domains")
	}
}

// TestNotifyDomainExpiring_DisabledPreference_NoSend covers the opted-out
// path for the expiring type.
func TestNotifyDomainExpiring_DisabledPreference_NoSend(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": false}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyDomainExpiring(context.Background(), "tenant-1", domainExpiringSummary()); err != nil {
		t.Fatalf("NotifyDomainExpiring() returned unexpected error: %v", err)
	}
	if len(sender.expiringTo) != 0 {
		t.Errorf("expiringTo = %v, want none (recipient opted out)", sender.expiringTo)
	}
}

// TestNotifyDomainNSDrift_SendsOnlyToEnabledOwnerOperator covers DHM-06's
// notification path: the expected-vs-current NS sets ride on the email.
func TestNotifyDomainNSDrift_SendsOnlyToEnabledOwnerOperator(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner-on", Email: "owner-on@example.com", Role: "owner"},
		{UserID: "operator-off", Email: "operator-off@example.com", Role: "operator"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner-on": true, "operator-off": false}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "https://vane.example.com")

	if err := svc.NotifyDomainNSDrift(context.Background(), "tenant-1", domainNSDriftSummary()); err != nil {
		t.Fatalf("NotifyDomainNSDrift() returned unexpected error: %v", err)
	}
	if len(sender.driftTo) != 1 || sender.driftTo[0] != "owner-on@example.com" {
		t.Errorf("driftTo = %v, want exactly [owner-on@example.com]", sender.driftTo)
	}
	if prefs.lastType != db.NotificationTypeDomainNSDrift {
		t.Errorf("resolved notification type = %q, want %q", prefs.lastType, db.NotificationTypeDomainNSDrift)
	}
	if sender.lastDrift.Hostname != "starbem.app" {
		t.Errorf("drift Hostname = %q, want %q", sender.lastDrift.Hostname, "starbem.app")
	}
	if !reflect.DeepEqual(sender.lastDrift.ExpectedNS, []string{"ns1.old.net", "ns2.old.net"}) {
		t.Errorf("ExpectedNS = %v, want the baseline set", sender.lastDrift.ExpectedNS)
	}
	if !reflect.DeepEqual(sender.lastDrift.CurrentNS, []string{"ns1.new.net"}) {
		t.Errorf("CurrentNS = %v, want the newly resolved set", sender.lastDrift.CurrentNS)
	}
	if sender.lastDrift.DashboardURL != "https://vane.example.com/domains" {
		t.Errorf("DashboardURL = %q, want %q", sender.lastDrift.DashboardURL, "https://vane.example.com/domains")
	}
}

// TestNotifyDomainNSDrift_DisabledPreference_NoSend covers the opted-out path
// for the drift type.
func TestNotifyDomainNSDrift_DisabledPreference_NoSend(t *testing.T) {
	members := &fakeMembers{members: []db.TenantMember{
		{UserID: "owner", Email: "owner@example.com", Role: "owner"},
	}}
	prefs := &fakePreferences{enabled: map[string]bool{"owner": false}}
	sender := &recordingSender{}
	svc := newTestService(members, prefs, sender, "")

	if err := svc.NotifyDomainNSDrift(context.Background(), "tenant-1", domainNSDriftSummary()); err != nil {
		t.Fatalf("NotifyDomainNSDrift() returned unexpected error: %v", err)
	}
	if len(sender.driftTo) != 0 {
		t.Errorf("driftTo = %v, want none (recipient opted out)", sender.driftTo)
	}
}

// TestSend_UnknownNotificationType_ReturnsError covers T5's done-when: the
// switch still rejects an unrecognized type with the existing error message.
func TestSend_UnknownNotificationType_ReturnsError(t *testing.T) {
	svc := newTestService(&fakeMembers{}, &fakePreferences{}, &recordingSender{}, "")

	err := svc.send(context.Background(), "to@example.com", "bogus_type", notificationPayload{}, "")
	if err == nil {
		t.Fatal("send(unknown type) returned nil error, want the unknown-type error")
	}
	if !strings.Contains(err.Error(), `notify: unknown notification type "bogus_type"`) {
		t.Errorf("error = %q, want the unchanged unknown-type message", err)
	}
}
