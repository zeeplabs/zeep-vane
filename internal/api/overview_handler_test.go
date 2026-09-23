//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// newOverviewScratchDatabase creates a brand-new, empty database on the same
// Postgres instance TEST_DATABASE_URL points at, returning a DSN for it. The
// overview aggregates unfiltered counts across the whole tenant, so each test
// needs a database nothing else is writing to - the shared
// TEST_DATABASE_URL database is used concurrently by other packages' suites.
func newOverviewScratchDatabase(t *testing.T) string {
	t.Helper()
	baseDSN := testDatabaseURL(t)

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("failed to parse TEST_DATABASE_URL: %v", err)
	}

	adminCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(adminCtx, baseDSN)
	if err != nil {
		t.Fatalf("failed to connect for scratch database setup: %v", err)
	}
	defer adminPool.Close()

	dbName := fmt.Sprintf("vane_overview_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(adminCtx, fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		t.Fatalf("failed to create scratch database %q: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupPool, err := pgxpool.New(cleanupCtx, baseDSN)
		if err != nil {
			return
		}
		defer cleanupPool.Close()
		_, _ = cleanupPool.Exec(cleanupCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
	})

	u.Path = "/" + dbName
	return u.String()
}

// newOverviewTestEnv returns an OverviewHandler backed by a fresh scratch
// database with one throwaway tenant, plus that tenant's pool.
func newOverviewTestEnv(t *testing.T) (*OverviewHandler, *db.Pool) {
	t.Helper()
	dsn := newOverviewScratchDatabase(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	admin, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() (admin) returned unexpected error: %v", err)
	}

	var tenantID string
	if err := admin.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "overview-fixture-tenant").Scan(&tenantID); err != nil {
		admin.Close()
		t.Fatalf("seeding fixture tenant returned unexpected error: %v", err)
	}
	admin.Close()

	pool, err := db.NewPool(ctx, dbtest.TenantScopedDSN(dsn, tenantID))
	if err != nil {
		t.Fatalf("NewPool() (tenant-scoped) returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	handler := NewOverviewHandler(
		db.NewServiceRepository(pool),
		db.NewStatusIntervalRepository(pool),
		db.NewIncidentRepository(pool),
		db.NewDomainRepository(pool),
		zap.NewNop(),
	)
	return handler, pool
}

func callOverview(t *testing.T, h *OverviewHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	return rec
}

func decodeOverview(t *testing.T, rec *httptest.ResponseRecorder) OverviewResponse {
	t.Helper()
	var resp OverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v; body=%s", err, rec.Body.String())
	}
	return resp
}

// createOverviewService inserts an operational service and returns it.
func createOverviewService(t *testing.T, pool *db.Pool, name string) *db.Service {
	t.Helper()
	repo := db.NewServiceRepository(pool)
	service := &db.Service{Name: name, SLOID: "slo-" + name}
	if err := repo.Create(context.Background(), service); err != nil {
		t.Fatalf("ServiceRepository.Create() returned unexpected error: %v", err)
	}
	return service
}

func TestOverviewHandler_Get_RealData_ReturnsAllAggregates(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	ctx := context.Background()
	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	incidents := db.NewIncidentRepository(pool)
	domains := db.NewDomainRepository(pool)
	now := time.Now()

	// svcOK: operational across the whole 30d window -> 100%.
	svcOK := createOverviewService(t, pool, "overview-ok")
	if err := services.UpdateStatus(ctx, svcOK.ID, "operational"); err != nil {
		t.Fatalf("UpdateStatus() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svcOK.ID, "operational", 0, now.AddDate(0, 0, -30)); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svcOK.ID, "operational", 0, now); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}

	// svcBad: one outage day, operational since 9d ago -> denominator clips
	// to 10d, 9d uptime / 10d = 90%.
	svcBad := createOverviewService(t, pool, "overview-bad")
	if err := services.UpdateStatus(ctx, svcBad.ID, "outage"); err != nil {
		t.Fatalf("UpdateStatus() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svcBad.ID, "outage", 0, now.AddDate(0, 0, -10)); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svcBad.ID, "operational", 0, now.AddDate(0, 0, -9)); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svcBad.ID, "operational", 0, now); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}

	// svcDegraded: no intervals, status degraded - exists so the unhealthy
	// count (2) is distinguishable from an operational count (1); with only
	// one of each, flipping OVW-05's `!=` to `==` would yield the same value.
	svcDegraded := createOverviewService(t, pool, "overview-degraded")
	if err := services.UpdateStatus(ctx, svcDegraded.ID, "degraded"); err != nil {
		t.Fatalf("UpdateStatus() returned unexpected error: %v", err)
	}

	// Two domains, one verified.
	verified := &db.Domain{Hostname: "overview-verified.example.com"}
	if err := domains.Create(ctx, verified); err != nil {
		t.Fatalf("DomainRepository.Create() returned unexpected error: %v", err)
	}
	if _, err := domains.SetVerificationResult(ctx, verified.ID, "verified", "active", nil, now); err != nil {
		t.Fatalf("SetVerificationResult() returned unexpected error: %v", err)
	}
	pending := &db.Domain{Hostname: "overview-pending.example.com"}
	if err := domains.Create(ctx, pending); err != nil {
		t.Fatalf("DomainRepository.Create() returned unexpected error: %v", err)
	}

	// Four incidents, ordered oldest to newest; two resolved (oldest). Of
	// the two open ones: inc-2 is critical severity, inc-3 is transitioned
	// to "monitoring" - so OpenIncidentsCritical/Monitoring (OVW-18) are
	// each 1, distinguishable from OpenIncidents (2).
	var created []*db.Incident
	for i, title := range []string{"inc-0", "inc-1", "inc-2", "inc-3"} {
		inc := &db.Incident{Title: title}
		if err := incidents.Create(ctx, inc, nil); err != nil {
			t.Fatalf("IncidentRepository.Create() returned unexpected error: %v", err)
		}
		created = append(created, inc)
		switch i {
		case 0, 1:
			if _, err := incidents.Transition(ctx, inc.ID, "resolved"); err != nil {
				t.Fatalf("Transition() returned unexpected error: %v", err)
			}
		case 2:
			if _, err := incidents.SetSeverity(ctx, inc.ID, "critical"); err != nil {
				t.Fatalf("SetSeverity(inc-2) returned unexpected error: %v", err)
			}
		case 3:
			if _, err := incidents.Transition(ctx, inc.ID, "monitoring"); err != nil {
				t.Fatalf("Transition(inc-3, monitoring) returned unexpected error: %v", err)
			}
		}
		time.Sleep(2 * time.Millisecond)
	}

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	if resp.UptimeAvg30d == nil {
		t.Fatalf("UptimeAvg30d = nil, want 95.0")
	}
	if *resp.UptimeAvg30d != 95.0 {
		t.Errorf("UptimeAvg30d = %v, want 95.0", *resp.UptimeAvg30d)
	}
	if resp.OpenIncidents != 2 {
		t.Errorf("OpenIncidents = %d, want 2", resp.OpenIncidents)
	}
	if resp.UnhealthyServices != 2 {
		t.Errorf("UnhealthyServices = %d, want 2 (outage + degraded)", resp.UnhealthyServices)
	}
	if resp.TotalServices != 3 {
		t.Errorf("TotalServices = %d, want 3 (svcOK + svcBad + svcDegraded)", resp.TotalServices)
	}
	if resp.VerifiedDomains != 1 {
		t.Errorf("VerifiedDomains = %d, want 1", resp.VerifiedDomains)
	}
	if resp.TotalDomains != 2 {
		t.Errorf("TotalDomains = %d, want 2 (one verified, one pending)", resp.TotalDomains)
	}
	if resp.OpenIncidentsCritical != 1 {
		t.Errorf("OpenIncidentsCritical = %d, want 1 (inc-2 only; resolved incidents excluded)", resp.OpenIncidentsCritical)
	}
	if resp.OpenIncidentsMonitoring != 1 {
		t.Errorf("OpenIncidentsMonitoring = %d, want 1 (inc-3 only)", resp.OpenIncidentsMonitoring)
	}
	// UptimeAvg30dPrior not asserted here: svcOK's open-ended interval
	// starts at (test setup's) now-30d, which is fractionally before the
	// handler's own now-30d boundary computed later - it non-deterministically
	// slivers into the prior window depending on wall-clock skew between the
	// two. TestOverviewHandler_Get_UptimePrior_AveragesSixtyToThirtyDaysAgo
	// covers OVW-17 deterministically instead.
	if len(resp.RecentIncidents) != 3 {
		t.Fatalf("len(RecentIncidents) = %d, want 3", len(resp.RecentIncidents))
	}
	if resp.RecentIncidents[0].ID != created[3].ID {
		t.Errorf("RecentIncidents[0].ID = %q, want newest %q", resp.RecentIncidents[0].ID, created[3].ID)
	}
	if resp.RecentIncidents[2].ID != created[1].ID {
		t.Errorf("RecentIncidents[2].ID = %q, want third-newest %q", resp.RecentIncidents[2].ID, created[1].ID)
	}
}

func TestOverviewHandler_Get_ZeroTenant_ReturnsDocumentedEmptyState(t *testing.T) {
	h, _ := newOverviewTestEnv(t)

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	if resp.UptimeAvg30d != nil {
		t.Errorf("UptimeAvg30d = %v, want nil for a tenant with no data", *resp.UptimeAvg30d)
	}
	if resp.OpenIncidents != 0 {
		t.Errorf("OpenIncidents = %d, want 0", resp.OpenIncidents)
	}
	if resp.UnhealthyServices != 0 {
		t.Errorf("UnhealthyServices = %d, want 0", resp.UnhealthyServices)
	}
	if resp.TotalServices != 0 {
		t.Errorf("TotalServices = %d, want 0", resp.TotalServices)
	}
	if resp.VerifiedDomains != 0 {
		t.Errorf("VerifiedDomains = %d, want 0", resp.VerifiedDomains)
	}
	if resp.TotalDomains != 0 {
		t.Errorf("TotalDomains = %d, want 0", resp.TotalDomains)
	}
	if resp.OpenIncidentsCritical != 0 {
		t.Errorf("OpenIncidentsCritical = %d, want 0", resp.OpenIncidentsCritical)
	}
	if resp.OpenIncidentsMonitoring != 0 {
		t.Errorf("OpenIncidentsMonitoring = %d, want 0", resp.OpenIncidentsMonitoring)
	}
	if resp.UptimeAvg30dPrior != nil {
		t.Errorf("UptimeAvg30dPrior = %v, want nil for a tenant with no data", *resp.UptimeAvg30dPrior)
	}
	if len(resp.UptimeSeries) != 14 {
		t.Errorf("len(UptimeSeries) = %d, want 14", len(resp.UptimeSeries))
	}
	for i, bucket := range resp.UptimeSeries {
		if bucket.UptimePercent != nil {
			t.Errorf("UptimeSeries[%d].UptimePercent = %v, want nil", i, *bucket.UptimePercent)
		}
	}
	if resp.RecentIncidents == nil {
		t.Error("RecentIncidents = nil, want an empty (non-nil) slice")
	}
	if len(resp.RecentIncidents) != 0 {
		t.Errorf("len(RecentIncidents) = %d, want 0", len(resp.RecentIncidents))
	}
}

func TestOverviewHandler_Get_ServiceWithoutIntervals_UptimeIsDash(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	createOverviewService(t, pool, "overview-no-data")

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	if resp.UptimeAvg30d != nil {
		t.Errorf("UptimeAvg30d = %v, want nil when the only service has no intervals", *resp.UptimeAvg30d)
	}
}

// TestOverviewHandler_Get_UptimePrior_AveragesSixtyToThirtyDaysAgo covers
// OVW-17 (the uptime card's "vs previous month" trend subtext): the prior
// figure must average the 60d-30d-ago window, not the current 30d window,
// and the two must be independently computable (a service can look
// different in the two windows).
func TestOverviewHandler_Get_UptimePrior_AveragesSixtyToThirtyDaysAgo(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	ctx := context.Background()
	services := db.NewServiceRepository(pool)
	intervals := db.NewStatusIntervalRepository(pool)
	now := time.Now()

	svc := createOverviewService(t, pool, "overview-prior")
	if err := services.UpdateStatus(ctx, svc.ID, "operational"); err != nil {
		t.Fatalf("UpdateStatus() returned unexpected error: %v", err)
	}
	// Prior window (60d-30d ago): outage the whole time -> 0%.
	if _, err := intervals.OpenOrExtend(ctx, svc.ID, "outage", 0, now.AddDate(0, 0, -60)); err != nil {
		t.Fatalf("OpenOrExtend() (prior) returned unexpected error: %v", err)
	}
	// Current window (30d ago-now): operational the whole time -> 100%.
	if _, err := intervals.OpenOrExtend(ctx, svc.ID, "operational", 0, now.AddDate(0, 0, -30)); err != nil {
		t.Fatalf("OpenOrExtend() (current) returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svc.ID, "operational", 0, now); err != nil {
		t.Fatalf("OpenOrExtend() (close) returned unexpected error: %v", err)
	}

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	if resp.UptimeAvg30d == nil || *resp.UptimeAvg30d != 100.0 {
		t.Fatalf("UptimeAvg30d = %v, want 100.0 (current window all-operational)", resp.UptimeAvg30d)
	}
	if resp.UptimeAvg30dPrior == nil {
		t.Fatalf("UptimeAvg30dPrior = nil, want 0.0 (prior window all-outage)")
	}
	if *resp.UptimeAvg30dPrior != 0.0 {
		t.Errorf("UptimeAvg30dPrior = %v, want 0.0 - proves the prior figure reads the 60d-30d window, not the current one", *resp.UptimeAvg30dPrior)
	}
}

func TestOverviewHandler_Get_UptimeSeries_IsFourteenConsecutiveLocalDays(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	ctx := context.Background()
	intervals := db.NewStatusIntervalRepository(pool)
	svc := createOverviewService(t, pool, "overview-series")
	if _, err := intervals.OpenOrExtend(ctx, svc.ID, "operational", 0, time.Now().AddDate(0, 0, -30)); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}
	if _, err := intervals.OpenOrExtend(ctx, svc.ID, "operational", 0, time.Now()); err != nil {
		t.Fatalf("OpenOrExtend() returned unexpected error: %v", err)
	}

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("time.LoadLocation() returned unexpected error: %v", err)
	}
	nowLocal := time.Now().In(loc)
	todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)

	if len(resp.UptimeSeries) != 14 {
		t.Fatalf("len(UptimeSeries) = %d, want 14", len(resp.UptimeSeries))
	}
	for i, bucket := range resp.UptimeSeries {
		wantDate := todayStart.AddDate(0, 0, -(overviewUptimeSeriesDays - 1 - i)).Format("2006-01-02")
		if bucket.Date != wantDate {
			t.Errorf("UptimeSeries[%d].Date = %q, want %q (oldest first)", i, bucket.Date, wantDate)
		}
	}
	if resp.UptimeSeries[overviewUptimeSeriesDays-1].Date != todayStart.Format("2006-01-02") {
		t.Errorf("last bucket date = %q, want today %q", resp.UptimeSeries[overviewUptimeSeriesDays-1].Date, todayStart.Format("2006-01-02"))
	}
}

func TestOverviewHandler_Get_RecentIncidents_IncludesResolvedAndCapsAtThree(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	ctx := context.Background()
	incidents := db.NewIncidentRepository(pool)

	// Oldest two resolved, newest two open.
	resolvedOld := &db.Incident{Title: "overview-resolved-old"}
	if err := incidents.Create(ctx, resolvedOld, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if _, err := incidents.Transition(ctx, resolvedOld.ID, "resolved"); err != nil {
		t.Fatalf("Transition() returned unexpected error: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	resolvedNewer := &db.Incident{Title: "overview-resolved-newer"}
	if err := incidents.Create(ctx, resolvedNewer, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if _, err := incidents.Transition(ctx, resolvedNewer.ID, "resolved"); err != nil {
		t.Fatalf("Transition() returned unexpected error: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	openA := &db.Incident{Title: "overview-open-a"}
	if err := incidents.Create(ctx, openA, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	openB := &db.Incident{Title: "overview-open-b"}
	if err := incidents.Create(ctx, openB, nil); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	rec := callOverview(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeOverview(t, rec)

	if len(resp.RecentIncidents) != 3 {
		t.Fatalf("len(RecentIncidents) = %d, want 3 (capped, oldest dropped)", len(resp.RecentIncidents))
	}
	if resp.RecentIncidents[0].ID != openB.ID {
		t.Errorf("RecentIncidents[0].ID = %q, want newest %q", resp.RecentIncidents[0].ID, openB.ID)
	}
	if resp.RecentIncidents[2].ID != resolvedNewer.ID {
		t.Errorf("RecentIncidents[2].ID = %q, want %q (resolved incidents are included)", resp.RecentIncidents[2].ID, resolvedNewer.ID)
	}
	if resp.RecentIncidents[2].Status != "resolved" {
		t.Errorf("RecentIncidents[2].Status = %q, want %q", resp.RecentIncidents[2].Status, "resolved")
	}
}

func TestOverviewHandler_Get_RepositoryError_ReturnsGeneric500(t *testing.T) {
	h, pool := newOverviewTestEnv(t)
	pool.Close()

	rec := callOverview(t, h)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"error":"internal server error"}` {
		t.Errorf("body = %q, want generic internal error JSON", got)
	}
}
