//go:build integration

package db

import (
	"context"
	"testing"
)

func statusAnalysisColumn(t *testing.T, pool *Pool, serviceID string) *string {
	t.Helper()
	var analysis *string
	row := pool.QueryRow(context.Background(), "SELECT status_analysis FROM services WHERE id = $1", serviceID)
	if err := row.Scan(&analysis); err != nil {
		t.Fatalf("status_analysis query returned unexpected error: %v", err)
	}
	return analysis
}

func TestServiceRepository_UpdateStatusAnalysis_SetsNonNilValue(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	services := seedServiceFixtures(t, repo, pool, "status-analysis-set", 1)
	svc := services[0]

	analysis := "SLO is approaching its error budget limit."
	if err := repo.UpdateStatusAnalysis(context.Background(), svc.ID, &analysis); err != nil {
		t.Fatalf("UpdateStatusAnalysis() returned unexpected error: %v", err)
	}

	got := statusAnalysisColumn(t, pool, svc.ID)
	if got == nil || *got != analysis {
		t.Errorf("status_analysis = %v, want %q", got, analysis)
	}
}

func TestServiceRepository_UpdateStatusAnalysis_NilClearsColumn(t *testing.T) {
	repo, pool := newServiceRepoTestPool(t)
	services := seedServiceFixtures(t, repo, pool, "status-analysis-clear", 1)
	svc := services[0]

	analysis := "previously set analysis"
	if err := repo.UpdateStatusAnalysis(context.Background(), svc.ID, &analysis); err != nil {
		t.Fatalf("setup UpdateStatusAnalysis() returned unexpected error: %v", err)
	}

	if err := repo.UpdateStatusAnalysis(context.Background(), svc.ID, nil); err != nil {
		t.Fatalf("UpdateStatusAnalysis(nil) returned unexpected error: %v", err)
	}

	got := statusAnalysisColumn(t, pool, svc.ID)
	if got != nil {
		t.Errorf("status_analysis = %q, want nil", *got)
	}
}

func TestServiceRepository_UpdateStatusAnalysis_UnknownServiceID_NoError(t *testing.T) {
	repo, _ := newServiceRepoTestPool(t)

	// UpdateStatus's existing single-column-update shape (which
	// UpdateStatusAnalysis mirrors) is a plain UPDATE ... WHERE id = $1
	// with no existence check - an unknown service ID matches zero rows
	// and returns no error, same as UpdateStatus's own behavior. This test
	// documents that shared shape rather than asserting a not-found error
	// UpdateStatusAnalysis was never designed to return.
	analysis := "x"
	if err := repo.UpdateStatusAnalysis(context.Background(), "00000000-0000-0000-0000-000000000000", &analysis); err != nil {
		t.Fatalf("UpdateStatusAnalysis() for unknown service ID returned unexpected error: %v", err)
	}
}
