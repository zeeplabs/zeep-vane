//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

// tenantScopedTables is spec.md AC1's list, resolved to the real table
// names in this schema: llm_provider_config is llm_providers + llm_settings
// and email_provider_config is email_providers + email_settings (each is a
// provider table plus the row recording which provider is active).
var tenantScopedTables = []string{
	"tenants",
	"tenant_memberships",
	"tenant_invites",
	"services",
	"incidents",
	"status_pages",
	"domains",
	"admin_audit_log",
	"email_providers",
	"email_settings",
	"llm_providers",
	"llm_settings",
}

// TestMultiTenancyMigration_EveryTenantScopedTableIsRLSProtected covers
// TENANT-01: every table in spec.md AC1's list carries exactly one RLS
// policy, with RLS both enabled and forced. FORCE matters specifically:
// without it the policy does not bind the table's own owner, which is the
// role a plain deployment connects as.
func TestMultiTenancyMigration_EveryTenantScopedTableIsRLSProtected(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	for _, table := range tenantScopedTables {
		var rlsEnabled, rlsForced bool
		if err := pool.QueryRow(ctx,
			"SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass", table,
		).Scan(&rlsEnabled, &rlsForced); err != nil {
			t.Fatalf("%s: querying pg_class returned unexpected error: %v", table, err)
		}
		if !rlsEnabled {
			t.Errorf("%s: row level security is not enabled", table)
		}
		if !rlsForced {
			t.Errorf("%s: row level security is not forced - the table owner would bypass the policy", table)
		}

		var policyCount int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = $1", table,
		).Scan(&policyCount); err != nil {
			t.Fatalf("%s: querying pg_policies returned unexpected error: %v", table, err)
		}
		if policyCount != 1 {
			t.Errorf("%s: has %d RLS policies, want exactly 1", table, policyCount)
		}

		var usingExpr string
		if err := pool.QueryRow(ctx,
			"SELECT qual FROM pg_policies WHERE schemaname = 'public' AND tablename = $1", table,
		).Scan(&usingExpr); err != nil {
			t.Fatalf("%s: reading policy expression returned unexpected error: %v", table, err)
		}
		if !strings.Contains(usingExpr, "app.tenant_id") {
			t.Errorf("%s: policy expression %q does not reference app.tenant_id", table, usingExpr)
		}
	}
}

// TestMultiTenancyMigration_DroppedSingleTenantTables covers the other half
// of T1: the pre-multi-tenancy identity and company tables are gone, not
// merely unused. A leftover `admins` table would let a stale code path keep
// authenticating against rows no tenant owns.
func TestMultiTenancyMigration_DroppedSingleTenantTables(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	for _, table := range []string{"admins", "admin_invites", "company_settings"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)", table,
		).Scan(&exists); err != nil {
			t.Fatalf("%s: existence query returned unexpected error: %v", table, err)
		}
		if exists {
			t.Errorf("%s still exists, want it dropped by the multi-tenancy migration", table)
		}
	}
}
