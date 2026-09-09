//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// createServiceFixtureForIncidentAI creates a single service for
// HasOpenIncidentForService tests, registering its cleanup.
func createServiceFixtureForIncidentAI(t *testing.T, pool *Pool) *Service {
	t.Helper()
	services := NewServiceRepository(pool)
	svc := &Service{Name: fmt.Sprintf("incident-ai-svc-%d", time.Now().UnixNano()), SLOID: "slo-incident-ai"}
	if err := services.Create(context.Background(), svc); err != nil {
		t.Fatalf("setup service Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", svc.ID) })
	return svc
}

// createIncidentFixtureForService inserts an incident linked to serviceID,
// optionally with description/autoCreated set, and registers its cleanup.
func createIncidentFixtureForService(t *testing.T, repo *IncidentRepository, pool *Pool, title, serviceID string, autoCreated bool) *Incident {
	t.Helper()
	incident := &Incident{Title: title, AutoCreated: autoCreated}
	var serviceIDs []string
	if serviceID != "" {
		serviceIDs = []string{serviceID}
	}
	if err := repo.Create(context.Background(), incident, serviceIDs); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })
	return incident
}

func TestIncidentRepository_Create_PersistsDescriptionAndAutoCreated(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	description := "generic fallback description"
	incident := &Incident{Title: "auto incident", Description: &description, AutoCreated: true}

	if err := repo.Create(context.Background(), incident, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	var found *Incident
	for i := range items {
		if items[i].ID == incident.ID {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatalf("incident %s not found in ListPaginated()", incident.ID)
	}
	if found.Description == nil || *found.Description != description {
		t.Errorf("Description = %v, want %q", found.Description, description)
	}
	if !found.AutoCreated {
		t.Error("AutoCreated = false, want true")
	}
}

func TestIncidentRepository_Create_ManualIncident_DescriptionNilAutoCreatedFalse(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "manual incident")

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	var found *Incident
	for i := range items {
		if items[i].ID == incident.ID {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatalf("incident %s not found in ListPaginated()", incident.ID)
	}
	if found.Description != nil {
		t.Errorf("Description = %q, want nil", *found.Description)
	}
	if found.AutoCreated {
		t.Error("AutoCreated = true, want false")
	}
}

func TestIncidentRepository_SetDescription_RoundTrips(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident for description")

	if err := repo.SetDescription(context.Background(), incident.ID, "an LLM-generated description"); err != nil {
		t.Fatalf("SetDescription() returned unexpected error: %v", err)
	}

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	var found *Incident
	for i := range items {
		if items[i].ID == incident.ID {
			found = &items[i]
		}
	}
	if found == nil {
		t.Fatalf("incident %s not found in ListPaginated()", incident.ID)
	}
	if found.Description == nil || *found.Description != "an LLM-generated description" {
		t.Errorf("Description = %v, want %q", found.Description, "an LLM-generated description")
	}
}

func TestIncidentRepository_SetDescription_UnknownIncident_ErrNotFound(t *testing.T) {
	repo, _ := newIncidentRepoTestPool(t)

	err := repo.SetDescription(context.Background(), "00000000-0000-0000-0000-000000000000", "x")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetDescription() error = %v, want ErrNotFound", err)
	}
}

func TestIncidentRepository_HasOpenIncidentForService_ManualIncidentOpen_ReturnsFalse(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	svc := createServiceFixtureForIncidentAI(t, pool)
	createIncidentFixtureForService(t, repo, pool, "manual maintenance incident", svc.ID, false)

	_, found, err := repo.HasOpenIncidentForService(context.Background(), svc.ID)
	if err != nil {
		t.Fatalf("HasOpenIncidentForService() returned unexpected error: %v", err)
	}
	if found {
		t.Error("found = true, want false (a manually-created incident must not count as an open auto-created one)")
	}
}

func TestIncidentRepository_HasOpenIncidentForService_OpenIncidentExists_ReturnsTrue(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	svc := createServiceFixtureForIncidentAI(t, pool)
	incident := createIncidentFixtureForService(t, repo, pool, "open incident", svc.ID, true)

	gotID, found, err := repo.HasOpenIncidentForService(context.Background(), svc.ID)
	if err != nil {
		t.Fatalf("HasOpenIncidentForService() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if gotID != incident.ID {
		t.Errorf("incidentID = %q, want %q", gotID, incident.ID)
	}
}

func TestIncidentRepository_HasOpenIncidentForService_NoIncident_ReturnsFalse(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	svc := createServiceFixtureForIncidentAI(t, pool)
	_ = repo

	_, found, err := repo.HasOpenIncidentForService(context.Background(), svc.ID)
	if err != nil {
		t.Fatalf("HasOpenIncidentForService() returned unexpected error: %v", err)
	}
	if found {
		t.Error("found = true, want false (no incidents linked to this service)")
	}
}

func TestIncidentRepository_HasOpenIncidentForService_OnlyResolvedIncident_ReturnsFalse(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	svc := createServiceFixtureForIncidentAI(t, pool)
	incident := createIncidentFixtureForService(t, repo, pool, "resolved incident", svc.ID, true)

	if _, err := repo.Transition(context.Background(), incident.ID, "resolved"); err != nil {
		t.Fatalf("setup Transition() returned unexpected error: %v", err)
	}

	_, found, err := repo.HasOpenIncidentForService(context.Background(), svc.ID)
	if err != nil {
		t.Fatalf("HasOpenIncidentForService() returned unexpected error: %v", err)
	}
	if found {
		t.Error("found = true, want false (only a resolved incident is linked)")
	}
}

func TestIncidentRepository_AllLinkedServicesOperational_SingleOperationalService_ReturnsTrue(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	services := NewServiceRepository(pool)
	svc := createServiceFixtureForIncidentAI(t, pool)
	if err := services.UpdateStatus(context.Background(), svc.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	incident := createIncidentFixtureForService(t, repo, pool, "single-service incident", svc.ID, true)

	allOperational, err := repo.AllLinkedServicesOperational(context.Background(), incident.ID)
	if err != nil {
		t.Fatalf("AllLinkedServicesOperational() returned unexpected error: %v", err)
	}
	if !allOperational {
		t.Error("allOperational = false, want true")
	}
}

// TestIncidentRepository_AllLinkedServicesOperational_OneServiceStillDown_ReturnsFalse
// covers the multi-service case (incident_services is N:N): an incident
// covering two services where one has recovered but the other hasn't must
// not be reported as fully recovered.
func TestIncidentRepository_AllLinkedServicesOperational_OneServiceStillDown_ReturnsFalse(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	services := NewServiceRepository(pool)
	recovered := createServiceFixtureForIncidentAI(t, pool)
	stillDown := createServiceFixtureForIncidentAI(t, pool)
	if err := services.UpdateStatus(context.Background(), recovered.ID, "operational"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}
	if err := services.UpdateStatus(context.Background(), stillDown.ID, "outage"); err != nil {
		t.Fatalf("setup UpdateStatus() returned unexpected error: %v", err)
	}

	incident := &Incident{Title: "two-service incident", AutoCreated: true}
	if err := repo.Create(context.Background(), incident, []string{recovered.ID, stillDown.ID}); err != nil {
		t.Fatalf("setup Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incident.ID) })

	allOperational, err := repo.AllLinkedServicesOperational(context.Background(), incident.ID)
	if err != nil {
		t.Fatalf("AllLinkedServicesOperational() returned unexpected error: %v", err)
	}
	if allOperational {
		t.Error("allOperational = true, want false (one linked service is still outage)")
	}
}

func TestIncidentRepository_ConfirmPendingClose_HappyPath_ResolvesAppendsAndClears(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident to close")

	if err := repo.SetPendingCloseComment(context.Background(), incident.ID, "Service has recovered."); err != nil {
		t.Fatalf("SetPendingCloseComment() returned unexpected error: %v", err)
	}

	resolved, err := repo.ConfirmPendingClose(context.Background(), incident.ID, "Service has recovered.")
	if err != nil {
		t.Fatalf("ConfirmPendingClose() returned unexpected error: %v", err)
	}
	if resolved.Status != "resolved" {
		t.Errorf("Status = %q, want %q", resolved.Status, "resolved")
	}
	if resolved.ResolvedAt == nil {
		t.Error("ResolvedAt is nil, want set")
	}

	updates, err := repo.ListUpdates(context.Background(), incident.ID)
	if err != nil {
		t.Fatalf("ListUpdates() returned unexpected error: %v", err)
	}
	var foundUpdate bool
	for _, u := range updates {
		if u.Body == "Service has recovered." {
			foundUpdate = true
		}
	}
	if !foundUpdate {
		t.Error("closing comment was not appended as an incident_update")
	}

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	for i := range items {
		if items[i].ID == incident.ID && items[i].PendingCloseComment != nil {
			t.Errorf("PendingCloseComment = %q, want nil after confirm", *items[i].PendingCloseComment)
		}
	}
}

func TestIncidentRepository_ConfirmPendingClose_NoPendingProposal_ErrNoPendingProposal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident without proposal")

	_, err := repo.ConfirmPendingClose(context.Background(), incident.ID, "anything")
	if !errors.Is(err, ErrNoPendingProposal) {
		t.Fatalf("ConfirmPendingClose() error = %v, want ErrNoPendingProposal", err)
	}
}

func TestIncidentRepository_ConfirmPendingClose_UnknownIncident_ErrNotFound(t *testing.T) {
	repo, _ := newIncidentRepoTestPool(t)

	_, err := repo.ConfirmPendingClose(context.Background(), "00000000-0000-0000-0000-000000000000", "anything")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConfirmPendingClose() error = %v, want ErrNotFound", err)
	}
}

// TestIncidentRepository_ConfirmPendingClose_CommentMismatch_ErrCloseProposalChanged
// covers the post-review fix: confirming with stale text (the proposal was
// regenerated by a later poll cycle after the caller last read it) must
// never resolve the incident with text the operator never actually saw.
func TestIncidentRepository_ConfirmPendingClose_CommentMismatch_ErrCloseProposalChanged(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident with regenerated proposal")

	if err := repo.SetPendingCloseComment(context.Background(), incident.ID, "original proposal"); err != nil {
		t.Fatalf("setup SetPendingCloseComment() returned unexpected error: %v", err)
	}
	if err := repo.SetPendingCloseComment(context.Background(), incident.ID, "regenerated proposal"); err != nil {
		t.Fatalf("setup SetPendingCloseComment() (regenerate) returned unexpected error: %v", err)
	}

	_, err := repo.ConfirmPendingClose(context.Background(), incident.ID, "original proposal")
	if !errors.Is(err, ErrCloseProposalChanged) {
		t.Fatalf("ConfirmPendingClose() error = %v, want ErrCloseProposalChanged", err)
	}

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	for i := range items {
		if items[i].ID == incident.ID {
			if items[i].Status == "resolved" {
				t.Error("Status = resolved, want unchanged - a comment mismatch must not resolve the incident")
			}
			if items[i].PendingCloseComment == nil || *items[i].PendingCloseComment != "regenerated proposal" {
				t.Errorf("PendingCloseComment = %v, want the regenerated proposal left untouched", items[i].PendingCloseComment)
			}
		}
	}
}

func TestIncidentRepository_DiscardCloseProposal_HappyPath_ClearsColumnOnly(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident to discard")

	if err := repo.SetPendingCloseComment(context.Background(), incident.ID, "draft comment"); err != nil {
		t.Fatalf("SetPendingCloseComment() returned unexpected error: %v", err)
	}

	if err := repo.DiscardCloseProposal(context.Background(), incident.ID); err != nil {
		t.Fatalf("DiscardCloseProposal() returned unexpected error: %v", err)
	}

	items, _, err := repo.ListPaginated(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	for i := range items {
		if items[i].ID == incident.ID {
			if items[i].PendingCloseComment != nil {
				t.Errorf("PendingCloseComment = %q, want nil after discard", *items[i].PendingCloseComment)
			}
			if items[i].Status != "investigating" {
				t.Errorf("Status = %q, want unchanged %q", items[i].Status, "investigating")
			}
		}
	}
}

func TestIncidentRepository_DiscardCloseProposal_NoPendingProposal_ErrNoPendingProposal(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	incident := createIncidentFixture(t, repo, pool, "incident without proposal to discard")

	err := repo.DiscardCloseProposal(context.Background(), incident.ID)
	if !errors.Is(err, ErrNoPendingProposal) {
		t.Fatalf("DiscardCloseProposal() error = %v, want ErrNoPendingProposal", err)
	}
}

func TestIncidentRepository_DiscardCloseProposal_UnknownIncident_ErrNotFound(t *testing.T) {
	repo, _ := newIncidentRepoTestPool(t)

	err := repo.DiscardCloseProposal(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("DiscardCloseProposal() error = %v, want ErrNotFound", err)
	}
}

// TestIncidentRepository_Create_RepeatedForSameService_NotDedupedByRepository
// confirms the repository layer stays a dumb persistence layer: deduping an
// outage incident per service is SLOAnalyzer's job (via
// HasOpenIncidentForService), not Create's - Create must never silently
// refuse or merge a second incident for a service that already has one
// open.
func TestIncidentRepository_Create_RepeatedForSameService_NotDedupedByRepository(t *testing.T) {
	repo, pool := newIncidentRepoTestPool(t)
	svc := createServiceFixtureForIncidentAI(t, pool)

	first := createIncidentFixtureForService(t, repo, pool, "first outage", svc.ID, true)
	second := createIncidentFixtureForService(t, repo, pool, "second outage", svc.ID, true)

	if first.ID == second.ID {
		t.Fatal("expected two distinct incidents, got the same ID")
	}

	var count int
	row := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM incident_services WHERE service_id = $1", svc.ID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("incident_services rows for service = %d, want 2 (Create must not dedupe)", count)
	}
}
