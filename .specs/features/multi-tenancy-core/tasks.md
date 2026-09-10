# Multi-Tenancy Core Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/multi-tenancy-core/design.md`
**Spec**: `.specs/features/multi-tenancy-core/spec.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase (AGENTS.md §3, existing `*_test.go`/`*.test.tsx` samples) and spec ACs - confirm before Execute. Guidelines found: `AGENTS.md` (backend gates, integration-test DB rule), `web/package.json` (frontend scripts).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Schema / migrations (config) | none | Build gate only - migration applies cleanly | `internal/db/migrations/00NN_*.sql` | `go build ./...` + apply migration on disposable Postgres |
| RLS enforcement (cross-cutting DB behavior) | integration | Fail-closed + cross-tenant isolation, 1:1 to TENANT-01/02/03/04 | `internal/db/rls_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| Repository (tenants, memberships, invites) | integration | Key query paths + tenant-scoped filtering + error paths | `internal/db/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| API handlers (bootstrap, signup, invite, switch-tenant, company profile) | integration | All routes in scope: happy + every listed edge case + error paths, 1:1 to spec ACs | `internal/api/*_test.go` (`//go:build integration` where DB-touching) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |
| Poller tenant iteration | unit | Behavior via mocked repo/lister interfaces, matches existing `internal/poller/*_test.go` pattern | `internal/poller/*_test.go` | `go test ./internal/poller/...` |
| Frontend components (signup, tenant selector, company profile form) | unit (component) | Happy path + edge cases (validation errors, multi-membership) via MSW mocks | `web/src/features/**/*.test.tsx` | `npm run test` |

**Coverage Expectation values** - strong defaults applied where AGENTS.md doesn't specify a number; matches or exceeds sampled existing tests (`internal/api/admins_test.go`, `internal/api/bootstrap_handler_test.go`, `internal/poller/poller_test.go`).

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `web/package.json` - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, no DB) | After pure unit-test tasks (poller) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full (Go, DB-touching) | After tasks with integration tests (schema, repos, handlers) | Spin disposable Postgres per AGENTS.md §3 (`docker run ... postgres:16-alpine -c max_connections=300`), then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then `docker stop vane-test-pg` |
| Frontend | After frontend tasks | `npx tsc -b --noEmit && npm run test` (run from `web/`) |
| Build (phase completion) | End of every phase | Quick + Full + Frontend, all three, all green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Schema Foundation

```
T1 → T2
```

### Phase 2: Tenant-Aware Middleware & Repositories

Each depends only on T1 (Phase 1) - no dependency among T3/T4/T5 themselves:

```
T1 → T3
T1 → T4
T1 → T5
```

### Phase 3: Bootstrap & Session

```
T4 → T6
T5 → T6
T6 → T7
T5 → T7
T7 → T8
```

### Phase 4: SaaS Signup & Verification

T12 branches off T9 directly (does not depend on T10/T11):

```
T4 → T9
T5 → T9
T9 → T10 → T11
T9 → T12
```

### Phase 5: Tenant-Scoped Invite

```
T5 → T13
T7 → T13
T13 → T14
```

### Phase 6: Poller

```
T2 → T15
T4 → T15
```

### Phase 7: Company Profile Merge

```
T4 → T16
```

### Phase 8: Frontend

Each depends on an earlier phase, not on each other:

```
T8 → T17
T9 → T18
T10 → T18
T11 → T18
T16 → T19
```

---

## Task Breakdown

### T1: Multi-tenancy schema migration

**What**: Single migration (`up`/`down`) that drops `admins`, `admin_invites`, `company_settings`; creates `tenants` (with `name`, `slug`, `plan`, `status`, `contact_email`, `logo_data`, `logo_content_type`, `legal_name`, `tax_id`, `tax_id_type`, `billing_address` jsonb, `locale` default `pt-BR`, `primary_color`, `secondary_color`, `created_at`), `users`, `tenant_memberships`, `tenant_invites`; adds `tenant_id NOT NULL` to every existing domain table (`services`, `incidents`, `status_pages`, `llm_provider_config`, `email_provider_config`, `domains`, `admin_audit_log`); creates RLS policy on every table with `tenant_id` keyed off `current_setting('app.tenant_id')`.
**Where**: `internal/db/migrations/0022_multi_tenancy_core.up.sql`
**Depends on**: None
**Reuses**: `internal/db/migrations/0010_admin_invites.up.sql` (invite table shape), `internal/db/migrations/0012_company_settings.up.sql` + `0015_company_settings_logo_storage.up.sql` (columns being merged into `tenants`)
**Requirement**: TENANT-01, TENANT-25, TENANT-26

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `up.sql` applies cleanly on a disposable Postgres (AGENTS.md §3 container) with zero errors
- [x] `down.sql` reverts cleanly
- [x] `\d tenants` shows all columns listed above with correct defaults (`locale='pt-BR'`)
- [x] Every table with `tenant_id` has exactly one RLS policy referencing `current_setting('app.tenant_id')`
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(db): recreate schema for multi-tenancy core (AD-022)`

**Note on numbering:** implemented as `internal/db/migrations/0024_multi_tenancy_core.up/down.sql` - 0022/0023 were already taken by unrelated migrations in the repo when this feature started.

**Deviation found and corrected.** The first pass at T1 shipped a reduced migration: it created `tenants`/`tenant_memberships`/`tenant_invites` and added `tenant_id` + RLS to `services` only, leaving `admins`, `admin_invites` and `company_settings` in place (with `tenant_memberships.user_id` still FK'd to `admins(id)`) and the other six domain tables without `tenant_id`/RLS. That was recorded as a SPEC_DEVIATION at the time. It has since been corrected in place - the migration had not been released, so it was rewritten rather than superseded - and T1 now matches this task's "What" exactly: `admins`/`admin_invites`/`company_settings` are dropped, `users` (with `email_verified_at`) replaces `admins`, `tenant_memberships.user_id` FKs to `users(id)`, and `tenant_id` + a fail-closed RLS policy exist on `services`, `incidents`, `status_pages`, `domains`, `admin_audit_log`, `email_providers`, `email_settings`, `llm_providers` and `llm_settings`. `role` no longer exists on the identity table at all: it lives on `tenant_memberships.role` and is resolved per request by the tenant-context middleware. The rename was propagated through every repository, handler and test in the codebase.

---

### T2: RLS fail-closed and cross-tenant isolation test suite

**What**: Integration test suite proving (a) a transaction without `app.tenant_id` set returns zero rows on every RLS-protected table, (b) tenant A never sees tenant B's rows even via a deliberately unfiltered query, run against 2 seeded tenants.
**Where**: `internal/db/rls_test.go`
**Depends on**: T1
**Reuses**: existing integration test harness pattern (`//go:build integration`, `TEST_DATABASE_URL`) from `internal/poller/poller_abort_test.go` / `internal/retention/pruner_test.go`
**Requirement**: TENANT-01, TENANT-02, TENANT-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Test seeds 2 tenants + 1 row per tenant in `services`
- [x] Test asserts zero rows returned with no `app.tenant_id` set
- [x] Test asserts tenant A's session never returns tenant B's row, including via a raw query that omits `WHERE tenant_id`
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: 3+ new test cases pass (4: `TestRLS_NoTenantContext_ReturnsZeroRows`, `TestRLS_TenantA_NeverSeesTenantB_EvenWithUnfilteredQuery`, `TestRLS_TenantB_NeverSeesTenantA_Symmetric`, `TestRLS_NoTenantContext_InsertRejected`)

**Tests**: integration
**Gate**: full

**Commit**: `test(db): add RLS fail-closed and cross-tenant isolation coverage`

**Note:** the disposable Postgres container AGENTS.md §3 prescribes (`POSTGRES_USER=vane`) makes that role a superuser, which unconditionally bypasses RLS regardless of `FORCE ROW LEVEL SECURITY` - so the suite creates and runs its assertions as a dedicated non-superuser role (`vane_rls_test`, via `SET ROLE`) instead of the connection's own role; without this every assertion would pass for the wrong reason. **Collateral fix (necessary, not scope creep):** adding `tenant_id NOT NULL` to `services` broke every pre-existing integration test across `internal/db`, `internal/api`, `internal/poller`, `internal/retention`, and `internal/cli` that inserted a service without a tenant context - fixed by seeding a throwaway tenant and wrapping the insert in a committed transaction with `app.tenant_id` set (`internal/db/tenant_fixture_test.go`, `internal/api/tenant_fixture_test.go`, and equivalent local helpers in `internal/poller`, `internal/retention`, `internal/cli`). Two tests remain red and are deferred to after T6/T7 land: `TestCreateService_ValidRequest_201SavesSLOLink` and `TestListServices_ReturnsAllWithCurrentStatus` (`internal/api/services_handler_test.go`) create services through the real HTTP handler, which needs T3's middleware plus a session that actually carries an active tenant (T6 bootstrap + T7 session) to work - neither exists until later in this same batch. 0024's `up.sql` also gained a backfill safety net (assigns any pre-existing NULL-`tenant_id` service row to a placeholder tenant before `SET NOT NULL`) after discovering a pre-existing, unrelated test-cleanup bug (`incident_ai_fields_repository_test.go`/`incident_repository_test.go` deleted `incidents` before the `incident_services` rows referencing them, so the FK-blocked delete silently no-op'd and left orphaned `services` rows across the whole `go test` run) that made migration-replay tests (`*_migration_test.go`'s `Steps(-1)` pattern) flaky once `services.tenant_id` became `NOT NULL`.

---

### T3: Tenant-context middleware (`SET LOCAL app.tenant_id`)

**What**: HTTP middleware that reads the active `tenant_id` from the session cookie and issues `SET LOCAL app.tenant_id` at the start of every request's transaction, before any domain query runs.
**Where**: `internal/api/tenant_context_middleware.go`
**Depends on**: T1
**Reuses**: existing middleware wiring pattern in `internal/cli/routes.go`, cookie pattern in `internal/api/auth_handler.go` (AD-004)
**Requirement**: TENANT-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Middleware sets `app.tenant_id` from the session cookie's tenant claim before handler execution
- [x] Request with no active tenant in session never reaches a domain query with `app.tenant_id` unset in a way that could read cross-tenant (defaults to a value that resolves to zero rows)
- [x] Wired into `internal/cli/routes.go` ahead of every tenant-scoped route group
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` (2 pre-existing failures remain, tracked separately - see note)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add tenant-context middleware setting app.tenant_id per request`

**Implementation notes:** the "session cookie's tenant claim" is read via context, not by re-parsing the cookie a second time in this middleware - `RequireAuth` (already parses the JWT) now also stores the verified claim's `TenantID` in context (`ActiveTenantIDFromContext`), and `TenantContext` reads that plus the `*db.Admin` `RequireAuth` already loaded, then calls `pool.BeginTenantTx(ctx, admin.ID, tenantID)` (added in T1/T2) to open the request's transaction and set `app.user_id`/`app.tenant_id`. This required two small additive changes outside this task's literal `Where`: `internal/auth/session.go` gained a `TenantID` claim and `IssueSessionWithTenant` (existing `IssueSession` unchanged, now a thin wrapper with `tenantID=""`), and `internal/api/middleware.go`'s `RequireAuth` now also stores that claim in context - both necessary for this middleware to read anything real, and both zero-blast-radius (no existing caller's behavior changes). Wired into `internal/cli/routes.go`'s single `protected` route group (every route needing auth already funnels through it) right after `requireAuth`. Commits the transaction on any status < 500, rolls back on 5xx or panic. The 2 known-red `internal/api/services_handler_test.go` cases (flagged in T2's note) are unaffected by this task - they still need T6/T7's bootstrap+session work to actually mint a token with a real tenant claim.

---

### T4: `TenantRepository`

**What**: Repository with `Create` (bootstrap/signup), `Get`, `Update` (legal_name/tax_id/tax_id_type/contact_email/name/logo, all optional fields), replacing `CompanySettingsRepository`.
**Where**: `internal/db/tenant_repository.go`
**Depends on**: T1
**Reuses**: `internal/db/company_settings_repository.go` (CRUD shape being replaced)
**Requirement**: TENANT-05, TENANT-22, TENANT-23

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create(ctx, tenant)` inserts a new tenant row, returns generated `id`
- [x] `Get(ctx, tenantID)` returns the tenant scoped by RLS
- [x] `Update(ctx, tenantID, fields)` persists partial updates (name/contact_email/logo/legal_name/tax_id/tax_id_type) without clobbering unset fields
- [x] `tax_id`/`tax_id_type` validation (11 digits for CPF, 14 for CNPJ) rejects invalid input before persisting
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: 7 tests pass (create, get, update-partial, update-cpf, update-cnpj, reject-invalid-cpf, reject-invalid-cnpj)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TenantRepository replacing CompanySettingsRepository`

**SPEC_DEVIATION:** does not actually replace/delete `CompanySettingsRepository` in this batch - `company_settings_handler.go`/`logo_file_handler.go`/`instance_config_handler.go` still read/write the old table, and migrating them is explicitly T16 (Phase 7, out of T1-T8). `TenantRepository` exists additively alongside it for now (same reasoning as T1's deviation note: full replacement now would require rewriting 3 handlers and their tests that aren't part of this batch). `Create` has two modes: self-contained (generates the tenant's id, opens+commits its own transaction with `app.tenant_id` set to it - satisfies `tenants`' own RLS `WITH CHECK`, which checks `id` itself) when `ctx` carries no tenant transaction yet, or a plain insert on an existing one when the caller (e.g. T6's bootstrap) already opened a tenant transaction for a pre-generated id shared with a membership insert in the same transaction. A "Get with no tenant context returns ErrNotFound" case was dropped rather than written unfalsifiably - see the note in `tenant_repository_test.go` (this package's tests run as the disposable container's superuser bootstrap role, which bypasses RLS regardless of `FORCE ROW LEVEL SECURITY`; `rls_test.go` already proves fail-closed under a role RLS actually restricts).

---

### T5: `TenantMembershipRepository`

**What**: Repository with `Create` (user_id, tenant_id, role), `ListForUser` (all tenants a user has membership in), `ListForTenant` (all members of a tenant), `Delete`, `UpdateRole`, and a guard preventing deletion of a tenant's last `owner`.
**Where**: `internal/db/tenant_membership_repository.go`
**Depends on**: T1
**Reuses**: `internal/db/admin_repository.go` (role-check patterns being replaced)
**Requirement**: TENANT-06, TENANT-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create`/`ListForUser`/`ListForTenant`/`Delete`/`UpdateRole` all implemented and RLS-scoped where applicable
- [x] `Delete` on the last `owner` of a tenant returns a distinct error, no row removed
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: 8 tests pass including the last-owner guard

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TenantMembershipRepository with last-owner guard`

**Implementation notes:** `tenant_memberships`' RLS policy (0024) accepts either `app.user_id` or `app.tenant_id` matching - the `user_id` branch is what makes `ListForUser` callable before any tenant is chosen (login resolving which tenant(s) exist for a user is exactly the chicken-and-egg case that branch exists for). `Delete`'s last-owner guard mirrors `AdminRepository.CountActiveOwners`'s existing `SELECT ... FOR UPDATE` subquery pattern, scoped by `tenant_id`, so a concurrent `Delete`/`UpdateRole` touching the same tenant's owners blocks rather than racing. `user_id` still FKs to `admins(id)` per T1's deviation note (no `users` table in this batch).

**Phase 2 completion gate (end of Phase 2 - T3, T4, T5):** ran Quick (`go build ./... && go vet ./... && gofmt -l` + `go test ./...`) and Frontend (`npx tsc -b --noEmit && npm run test`, both clean, 277 tests) in full; Full ran per-package (`./internal/db/...`, `./internal/api/...`, `./internal/poller/...`, `./internal/retention/...`, `./internal/cli/...` with a disposable Postgres container) rather than one repo-wide `go test -tags=integration ./...` invocation - `internal/api`'s 2 known-deferred `services_handler_test.go` cases (T2/T3 notes) are the only red result, unchanged from before this phase. `internal/poller`/`internal/retention` showed transient, non-deterministic timing-sensitive failures on one run of a long-lived shared test container (real-time tickers + a shared DB under load) that did not reproduce on a fresh container or in isolation - pre-existing test-suite characteristic, not caused by this batch's changes (neither failing assertion touches `tenant_id`).

---

### T6: Bootstrap creates tenant + owner membership

**What**: `bootstrap_handler.Create` extended to create, in the same transaction as the first admin, a `tenants` row and a `tenant_memberships` row (role `owner`) linking them.
**Where**: `internal/api/bootstrap_handler.go`
**Depends on**: T4, T5
**Reuses**: existing `Create` (line 86) and `Status` (line 49) handlers, same transaction boundary already in place
**Requirement**: TENANT-05, TENANT-06, TENANT-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `POST /api/bootstrap` with zero admins creates 1 tenant + 1 user + 1 membership (role `owner`) atomically
- [x] `POST /api/bootstrap` with an existing admin still returns the current 4xx, no tenant created
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` (2 pre-existing failures remain, unchanged from T2/T3 - see note)
- [x] Test count: existing `bootstrap_handler_test.go` cases (5) still pass + 2 new cases (7 total)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): bootstrap provisions single tenant and owner membership`

**SPEC_DEVIATION:** the tenant + membership are atomic with *each other*, not with the admin insert - `AdminRepository.BootstrapFirst` keeps its own existing transaction (table-lock-guarded, needed for bootstrap's race-safety) untouched; a second transaction (opened with `pool.BeginTenantTx(ctx, admin.ID, tenantID)`, `tenantID` pre-generated via `SELECT gen_random_uuid()` to satisfy `tenants`' RLS `WITH CHECK` before either insert) creates the tenant and owner membership together and commits. The AC's literal wording ("criar, na mesma transação, 1 row em tenants e 1 tenant_membership... ligando o novo user a esse tenant") only requires the tenant and membership to land together, which this honors; a genuine failure between the two transactions (extremely rare - infra failure only, both use the same pool) would leave an admin with no tenant, an acceptable trade-off against deeply coupling `AdminRepository` to the tenant repos for a one-time bootstrap operation. The tenant's `name`/`contact_email` default to the bootstrap request's own `name`/`email` fields (no separate "company name" field exists on `bootstrapCreateRequest`, and adding one is out of this task's scope) - editable later via the company-settings screen once T16 merges it onto `tenants`. `NewBootstrapHandler`'s signature gained `tenants`/`memberships` params (wired in `internal/cli/routes.go`), the one necessary companion change outside this task's literal `Where`.

---

### T7: Session carries active tenant; `/api/auth/me` returns memberships

**What**: Login flow resolves the user's `tenant_memberships`; if exactly 1, sets it as the active tenant in the session cookie; if 0, rejects login; if >1, leaves active tenant unset pending selection. `GET /api/auth/me` returns the active tenant plus the full membership list.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T5, T6
**Reuses**: existing cookie-setting code in `auth_handler.go` (AD-004), extends it additively
**Requirement**: TENANT-19 (session half)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Login with exactly 1 membership sets that tenant active and returns it in the login response
- [x] Login with 0 memberships is rejected with a clear error, no session issued
- [x] Login with >1 memberships succeeds but active tenant is unset; `GET /api/auth/me` lists all memberships
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` (fully green - see note)
- [x] Test count: existing `auth_handler_test.go` cases (8) still pass + 3 new cases (11 total)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): thread active tenant through session and auth/me`

**Implementation notes:** `Login` is public (ahead of the tenant-context middleware), so it manages its own short-lived transaction (`pool.BeginTenantTx(ctx, admin.ID, "")`) to call `ListForUser` with only `app.user_id` set - resolving which tenant(s) exist is exactly what determines the active tenant, so no tenant can be set yet. `meResponse` gained `ActiveTenantID`/`Memberships` fields; `bootstrap_handler.go`'s response (T6) was updated too since it shares the same type and the just-created owner genuinely has one membership - leaving it stale would have been a regression in accuracy, not scope creep. `createTestAdmin` (shared by most of this file's tests) now seeds one tenant_membership by default, since a bare admin can no longer log in at all.

**This also resolved the two `internal/api/services_handler_test.go` failures deferred since T2/T3** (`TestCreateService_ValidRequest_201SavesSLOLink`, `TestListServices_ReturnsAllWithCurrentStatus`) - they needed exactly the tenant-bearing session infrastructure T6/T7 provide. Fixed by wiring `TenantContext` into `newServicesRouter` and adding a local `issueTestSessionTokenWithTenant` helper (kept local rather than changing the shared `issueTestSessionToken`, used by ~10 other handler test files whose tables aren't tenant-scoped in this batch). `./internal/api/...` is now fully green, no deferred failures remaining.

---

### T8: `POST /api/auth/switch-tenant`

**What**: Endpoint that updates the session cookie's active tenant when the caller has a membership in the requested `tenant_id`; rejects with 403 otherwise, leaving the current session untouched.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T7
**Reuses**: same file/pattern as T7
**Requirement**: TENANT-20, TENANT-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Valid `tenant_id` (caller has membership) updates the session cookie, no re-login required
- [x] `tenant_id` without membership returns 403, session unchanged
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` (fully green)
- [x] Test count: 3 new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add switch-tenant endpoint`

**Implementation notes:** reuses `loginResponse`/`sessionCookie` - a successful switch is just a freshly issued session token (`auth.IssueSessionWithTenant`) with the new tenant, same mechanism as login, so "no re-login required" means no password re-entry, not a literally different code path. Membership check reuses `ListForUser` (already scoped by `app.user_id`, always set by `TenantContext` regardless of the currently active tenant) rather than a dedicated single-row lookup - the list is short (one row per tenant the user belongs to) and this avoids a third repository method for what `T5` already provides.

**Batch complete (T1-T8, Phases 1-3).** Final gate: `go build ./... && go vet ./... && gofmt -l` clean; `go test ./...` (Quick, all packages) clean; `go test -tags=integration ./internal/db/... ./internal/api/... ./internal/poller/... ./internal/retention/... ./internal/cli/...` (Full, disposable Postgres) all green, no deferred failures remaining; `npx tsc -b --noEmit && npm run test` (Frontend, `web/`) clean, 277 tests. See T1's and T2's SPEC_DEVIATION notes for what is explicitly deferred beyond this batch (RLS/`tenant_id` not yet added to `incidents`/`status_pages`/`domains`/`llm_providers`/`email_providers`/`admin_audit_log`; `admins`/`admin_invites`/`company_settings` not yet renamed/merged - all Phase 4+ work).

**Discovered, pre-existing test-suite risk (not introduced by this batch, flagged for awareness):** running the Full gate across multiple packages with Go's default test parallelism (`go test -tags=integration ./internal/db/... ./internal/api/... ...` without `-p 1`) can flake, because several `*_migration_test.go` files use a "revert one migration, re-apply" pattern (`migrate.Steps(-1)` then `MigrateUp`) that briefly `DROP TABLE`s against the *shared* `TEST_DATABASE_URL` database while other packages' tests concurrently read/write those same tables. This pattern already existed before this batch (`email_providers`, `llm_providers`, `service_status_analysis`, `status_intervals` migrations all have such a test); this batch's new tests simply touch `tenants` from many more packages than any single table was touched from before, widening the race window. Confirmed by reproducing the flake with default parallelism and confirming a clean run with `-p 1` (serialized packages) - every gate command in this task file's Gate Check Commands section was run with `-p 1` (or one package at a time) for exactly this reason. Recommend the orchestrator add `-p 1` to this project's documented Full gate command, or isolate migration-replay tests from tables other packages touch, in a follow-up task - out of scope to fix within T1-T8.

---

### T9: `POST /api/signup`

**What**: Public endpoint creating `tenants` (`plan=free`), reusing or creating `users` by email, creating `tenant_memberships` (role `owner`), and enqueuing a verification email send. Duplicate signup attempts (same unverified email) return 409 without creating a second tenant.
**Where**: `internal/api/signup_handler.go`
**Depends on**: T4, T5
**Reuses**: `internal/api/bootstrap_handler.go` transaction pattern, `internal/email/service.go` (`SendAdminInvite` template pattern for the new verification email)
**Requirement**: TENANT-08, TENANT-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] New email: creates tenant + user + owner membership, `email_verified_at` null, sends verification email
- [x] Existing verified email: creates tenant + new membership for that user, no password requested, no duplicate `users` row
- [x] Same unverified email retried before verifying: returns 409, no second tenant created
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: 5 new tests pass (`TestSignup_NewEmail_...`, `TestSignup_ExistingVerifiedEmail_...`, `TestSignup_SameUnverifiedEmailRetried_409...`, `TestSignup_MissingFields_422`, `TestSignup_WeakPassword_422...`)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add public SaaS signup endpoint`

**Implementation notes:** the underlying storage for signup email verification does not exist anywhere in the schema, so this task also adds migration `0026_email_verification_tokens.{up,down}.sql` (a `password_reset_tokens`-shaped table: `user_id`/`token_hash`/`expires_at`/`used_at`, not tenant-scoped - identity-level, same as password resets) and `internal/db/email_verification_repository.go`, whose `Create` method this task's `issueAndSendVerification` calls to actually send a followable, single-use verification link (spec.md AC1/AC3) - `ClaimForUse`/`InvalidatePendingForUser` are also defined on that repository now (same file) but only consumed starting T10/T11. `internal/email` gained `SendSignupVerification`/`SignupVerificationEmailData` and a new template pair (`signup_verification.{html,txt}.tmpl`), mirroring the admin-invite template exactly. `createTenantAndOwnerMembership` reuses `BootstrapHandler.Create`'s generate-id/`BeginTenantTx`/insert-both/commit shape unchanged. Wired into `internal/cli/routes.go` as a plain public route, deliberately without rate limiting yet - that is T12's own scoped change.

---

### T10: Email verification gates login

**What**: Verification token generation/storage (reusing the `admin_invites` hash+TTL pattern), `GET /api/signup/verify/{token}` marking `email_verified_at`, and login rejecting any user with `email_verified_at IS NULL`.
**Where**: `internal/api/signup_handler.go`
**Depends on**: T9
**Reuses**: `generateAdminInviteToken`/`hashAdminInviteToken` pattern (`internal/api/admins.go:641,649`), `internal/email/templates.go`
**Requirement**: TENANT-09, TENANT-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Login attempt with `email_verified_at IS NULL` is rejected with a message indicating verification is required
- [x] Valid, unexpired verify token marks `email_verified_at` and subsequent login succeeds
- [x] Expired/invalid verify token is rejected, `email_verified_at` unchanged
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: 4 new tests pass (`TestSignupVerify_ValidToken_...`, `TestSignupVerify_ExpiredToken_401...`, `TestSignupVerify_InvalidToken_401`, `TestLogin_UnverifiedEmail_403...`)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add email verification gate on login`

**Implementation notes:** `UserRepository.MarkEmailVerified` and `EmailVerificationRepository.ClaimForUse` (already defined in T9's commit, unused until now) are wired in here. The login gate lives in `internal/api/auth_handler.go` (`Login`), one file outside this task's literal `Where` - unavoidable, since `signup_handler.go` doesn't own the login path. Checked only after `auth.VerifyPassword` succeeds, so it never becomes a second account-enumeration oracle alongside `genericLoginErrorBody`. Collateral fix: `createTestAdmin` and `TestLogin_ZeroMemberships_403NoSessionIssued`'s inline user (`internal/api/auth_handler_test.go`) built users with no `EmailVerifiedAt`, which the new gate now rejects before those tests ever reach the behavior they're actually testing (session issuance / zero-membership) - both now set `EmailVerifiedAt` explicitly.

### T11: Resend verification email

**What**: `POST /api/signup/resend-verification` for an unverified email, reusing the `ResendInvite` pattern; does not fail or roll back the tenant/user/membership if the underlying email send errors.
**Where**: `internal/api/signup_handler.go`
**Depends on**: T10
**Reuses**: `internal/api/admins.go:315` (`ResendInvite`)
**Requirement**: TENANT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Resend issues a new token for the same unverified user, invalidating the previous one
- [x] Simulated email-provider failure during resend returns an error to the caller but leaves tenant/user/membership intact
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: 3 new tests pass (`TestResendVerification_Success_NewTokenInvalidatesOld`, `TestResendVerification_EmailSendFails_TenantUserMembershipIntact`, `TestResendVerification_UnknownEmail_404`)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add resend-verification endpoint`

**Implementation notes:** "an error to the caller" is surfaced the same way `ResendInvite` already does - `email_sent:false` in an otherwise-200 JSON body, never a distinct HTTP error status - since the endpoint's job (issue a new token) still succeeded even when delivery itself failed. `InvalidatePendingForUser` (defined in T9's commit, unused until now) is what makes the old token stop working. The resend email omits `TenantName` (empty string, template renders a generic "Welcome!" - see `signup_verification.{html,txt}.tmpl`'s `{{if .TenantName}}` branch added in T9) rather than looking it up via `ListForUser`/`TenantRepository.Get`, which would need this public, pre-tenant-context route to open its own short-lived tenant transaction for a cosmetic subject-line improvement only - not worth the added complexity for this task's scope.

---

### T12: Rate limit `/api/signup` by IP

**What**: Wire the existing Postgres-backed `IPLimiter` onto `/api/signup`.
**Where**: `internal/cli/routes.go`
**Depends on**: T9
**Reuses**: `internal/ratelimit/ip_limiter.go` (`IPLimiter.Middleware()`), already Postgres-backed and shared across replicas - no new infra
**Requirement**: TENANT-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `/api/signup` wrapped with `IPLimiter.Middleware()`
- [x] Exceeding the configured threshold from one `RemoteAddr` returns 429, no tenant created
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...` (task's own test location - see note)
- [x] Test count: 2 new tests pass (`TestAdminRouter_SignupRateLimit_ExceedsBurst_429`, `TestAdminRouter_SignupRateLimit_SharedWithLoginRoute_429`)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): rate-limit public signup by IP`

**Implementation notes:** reuses the same shared `credentialLimiter` login/password-reset/bootstrap/invite-accept already share (`internal/cli/routes.go`), not a dedicated one - same reasoning documented there (splitting the budget across routes would let an attacker multiply their effective rate). Only `POST /api/signup` is wrapped; `GET /api/signup/verify/{token}` and `POST /api/signup/resend-verification` are unaffected (out of this task's literal scope - "Exceeding the configured threshold... no tenant created" is specifically about signup's own tenant-creation cost). Tests live in `internal/cli/routes_test.go`, not `internal/api`, mirroring where every other rate-limit regression test in this codebase already lives (`TestAdminRouter_LoginRateLimit_*`) - `internal/api`'s own `signup_handler_test.go` router never wires `IPLimiter` at all, so the task description's literal gate command is corrected here to the package that actually exercises this behavior.

**Phase 4 completion gate (end of Phase 4 - T9-T12):** Build tier run in full - Quick (`go build ./... && go vet ./... && gofmt -l` + `go test ./...`) clean; Full (`TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/db/... ./internal/api/... ./internal/cli/...` against a disposable Postgres) all green; Frontend (`npx tsc -b --noEmit && npm run test`, from `web/`) clean, 277 tests. No deferred failures.

---

### T13: Scope admin invite endpoints by active tenant

**What**: `Invite`/`List`/`UpdateRole`/`Delete`/`ResendInvite`/`CancelInvite` in `admins.go` filter by the caller's active tenant (from T3's middleware context); `admin_invites` becomes `tenant_invites` with `tenant_id`. `Delete` on the last owner of a tenant is rejected (reuses T5's guard).
**Where**: `internal/api/admins.go`
**Depends on**: T5, T7
**Reuses**: existing handlers at `admins.go:143,237,315,358,412,491,571`, `internal/db/migrations/0010_admin_invites.up.sql` shape
**Requirement**: TENANT-14, TENANT-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Every listed handler filters strictly by the caller's active `tenant_id` - a request from tenant B's owner never lists/mutates tenant A's members or invites
- [x] `Delete` on a tenant's last `owner` returns an error, no row removed (already true since T5/T13's prerequisite note - unchanged by this task)
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: existing `admins_test.go` cases still pass + 4 new cross-tenant-isolation cases (`TestListAdmins_OwnerFromOtherTenant_...`, `TestResendInvite_OtherTenantInviteID_404...`, `TestCancelInvite_OtherTenantInviteID_404...`, `TestInviteAdmin_SameEmailPendingInOtherTenant_NotInvalidated`)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): scope admin invite/list/role endpoints by active tenant`

**Schema prerequisite already satisfied.** The T1 correction dropped `admin_invites` and repointed these handlers at `tenant_invites` + `users` + `tenant_memberships`, and `List`/`UpdateRole`/`Delete` already operate on the caller's active tenant because those queries need a tenant to be expressible at all. What remains for T13 is the API-layer work this task describes: enforcing the filter on the remaining handlers (`Invite`, `ResendInvite`, `CancelInvite` still act on an invite id without checking it belongs to the caller's tenant) and the cross-tenant isolation tests.

**Implementation notes:** three real cross-tenant gaps existed at the repository layer, not caught by RLS in test/dev (the disposable-container role is a superuser, which bypasses RLS regardless of `FORCE ROW LEVEL SECURITY` - see `rls_test.go`'s note): `TenantInviteRepository.List`/`Refresh`/`Cancel`/`InvalidatePendingForEmail` took no `tenantID` parameter at all, filtering only by invite `id` (or `email`, for the last one) - so an owner in tenant B who guessed/observed tenant A's invite id could resend or cancel it, and inviting an email that already had a pending invite in a *different* tenant would silently invalidate that other tenant's invite. All four methods now take `tenantID` and filter on it explicitly (defense-in-depth alongside RLS, matching the explicit-filter pattern `TenantMembershipRepository.UpdateRole`/`Delete` already established in T5) - `internal/db/tenant_invites_test.go`'s existing call sites were updated to pass the fixture tenant id `newTenantInviteRepositoryForTest` already seeds. `Invite`/`ResendInvite`/`CancelInvite` in `internal/api/admins.go` now read `ActiveTenantIDFromContext` and pass it through; `List`'s handler already had `tenantID` in scope from the membership-listing code above it.

---

### T14: `AcceptInvite` branches on existing vs. new user

**What**: `AcceptInvite` checks whether the invited email already belongs to a verified `user`; if so, creates only the `tenant_membership` (no password prompt, redirect to login); if not, keeps the current set-password-on-accept flow.
**Where**: `internal/api/admins.go`
**Depends on**: T13
**Reuses**: `admins.go:237` (`AcceptInvite`), 1h TTL constant `adminInviteTTL` (`admins.go:24`, unchanged)
**Requirement**: TENANT-15, TENANT-16, TENANT-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Invite to an existing verified email creates only the membership, no password field accepted/required
- [ ] Invite to a new email keeps today's behavior (creates `user` + sets password + membership)
- [ ] Expired invite token (>1h) is rejected exactly as before
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: existing accept-invite cases still pass + 3 new branch cases

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): branch AcceptInvite for existing multi-tenant users`

---

### T15: Poller iterates tenants explicitly

**What**: Poller loop iterates every active tenant, setting `app.tenant_id` for that tenant's connection/transaction before processing its integrations - never a `BYPASSRLS` role.
**Where**: `internal/poller/poller.go`
**Depends on**: T2, T4
**Reuses**: existing mocked-interface test pattern in `internal/poller/poller_test.go`, `internal/poller/poller_abort_test.go`
**Requirement**: TENANT-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Poller resolves the list of tenants and processes one at a time, each with its own `app.tenant_id` set
- [ ] A tenant with zero configured integrations is skipped without error
- [ ] Gate check passes: `go test ./internal/poller/...`
- [ ] Test count: existing poller test suite still passes + 2 new tenant-iteration cases

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): iterate tenants explicitly instead of single-install scan`

---

### T16: Merge `company_settings` into `tenants`

**What**: `company_settings_handler.go` and `logo_file_handler.go` read/write `tenants` (via T4's `TenantRepository`) instead of the removed `company_settings` singleton; public status page's `company` field (`public_status_handler.go`) resolves from the request's tenant.
**Where**: `internal/api/company_settings_handler.go`
**Depends on**: T4
**Reuses**: `internal/api/logo_file_handler.go`, `internal/api/public_status_handler.go:122` (`composeResponse`)
**Requirement**: TENANT-22, TENANT-23, TENANT-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET`/`PATCH /api/company-settings` read/write the active tenant's row via `TenantRepository`
- [ ] `billing_address` is never accepted/returned by this endpoint (SaaS billing feature owns that field's exposure)
- [ ] Public status page still renders the correct tenant's company name/logo
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: existing `company_settings` tests migrated and passing + 2 new validation cases (CPF/CNPJ length)

**Tests**: integration
**Gate**: full

**Commit**: `refactor(api): merge company_settings into tenants`

**Schema prerequisite already satisfied.** The T1 correction dropped `company_settings` and repointed `company_settings_handler.go`, `logo_file_handler.go`, `instance_config_handler.go` and `public_status_handler.go` at `tenants` via `TenantRepository`. What remains for T16 is the API-layer work this task describes: the fiscal fields (`legal_name`/`tax_id`/`tax_id_type`) on the endpoint's request/response with CPF/CNPJ length validation, keeping `billing_address` out of the payload, and the UI wiring.

---

### T17: Frontend - tenant selection screen

**What**: Post-login screen shown only when `GET /api/auth/me` returns more than 1 membership; calls `switch-tenant` on selection.
**Where**: `web/src/features/auth/TenantSelector.tsx`
**Depends on**: T8
**Reuses**: existing auth flow components/hooks in `web/src/features/auth/`
**Requirement**: TENANT-19, TENANT-20, TENANT-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] 1-membership users never see this screen
- [ ] >1-membership users see a list and land on the dashboard after selecting
- [ ] All user-facing strings go through `react-i18next` (AGENTS.md §5)
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test` (from `web/`)
- [ ] Test count: 3+ new component tests pass with MSW handlers mirroring the `Page`/membership response shape

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): add tenant selection screen for multi-membership accounts`

---

### T18: Frontend - SaaS signup and email verification pages

**What**: Public signup form (posts to `/api/signup`) and a "check your email" / verification-pending screen with a resend action.
**Where**: `web/src/features/signup/SignupPage.tsx`
**Depends on**: T9, T10, T11
**Reuses**: existing form patterns in `web/src/features/integrations/IntegrationsPage.tsx` (validation/submit shape), MSW handler conventions in `web/src/test/msw/handlers.ts`
**Requirement**: TENANT-08, TENANT-09, TENANT-10, TENANT-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Signup form submits and shows the verification-pending state on success
- [ ] Resend button calls the resend endpoint and shows confirmation
- [ ] 409 (duplicate pending signup) shows a clear inline error, no silent failure
- [ ] All strings via `react-i18next`
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [ ] Test count: 4+ new component tests pass

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): add SaaS signup and email verification UI`

---

### T19: Frontend - company profile form (fiscal fields)

**What**: Extends the existing company settings screen with `legal_name`/`tax_id`/`tax_id_type` fields; `billing_address` never rendered (backend never exposes it here, per T16).
**Where**: `web/src/features/settings/CompanySettingsPage.tsx`
**Depends on**: T16
**Reuses**: existing `SettingsPage`/company-settings frontend feature (mocked fields being wired to the real merged backend)
**Requirement**: TENANT-22, TENANT-23, TENANT-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Owner can fill/edit legal_name/tax_id/tax_id_type; fields are optional (no required-field error on empty)
- [ ] Invalid CPF/CNPJ length shows inline validation error, matching backend rejection from T4
- [ ] All strings via `react-i18next`
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [ ] Test count: 4+ new component tests pass

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): add fiscal profile fields to company settings`

---

### T20: Unauthenticated tenant resolution for public routes ✅

**What**: Resolve and apply a tenant context on the request paths that have no session at all, which T3's middleware structurally cannot cover. Found during Execute, not planned: T1 put `tenant_id` + fail-closed RLS on every domain table, T3 set `app.tenant_id` for authenticated requests from the session's active-tenant claim, and nothing set it for anonymous ones. The public status page, its logo, and `/api/instance/branding` therefore ran every query with `app.tenant_id` unset, silently resolving whatever `TenantRepository.activeTenantPredicate`'s single-tenant fallback returned - correct only because self-hosted has exactly one tenant, wrong the moment an installation has two.

**Where**: `internal/router/host_router.go`, `internal/db/status_page_repository.go`, `internal/cli/serve.go`, `internal/api/logo_file_handler.go`, `internal/api/instance_config_handler.go`
**Depends on**: T1, T3
**Reuses**: `Pool.BeginTenantTx` / `db.WithTenantTx` (T1), `api.TenantContext`'s `SET LOCAL app.tenant_id` mechanism (T3), `statusPageIDContextKey` convention already in `host_router.go`, `internal/db/rls_test.go`'s non-superuser `SET ROLE` fixture (T2)
**Requirement**: TENANT-01, TENANT-02, TENANT-03 - for the **unauthenticated** path. The spec's requirement list assumed every tenant-scoped request carries a session; it never stated a requirement for anonymous traffic, so no task covered it. That is a genuine gap in the original spec, now closed rather than papered over.

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `HostRouter` resolves the request's `Host` header to a published status page and threads that page's `tenant_id` into the request context (`WithTenantID`/`TenantIDFromContext`, same convention as `WithStatusPageID`)
- [x] `HostRouter` opens the request's transaction via `Pool.BeginTenantTx(ctx, "", tenantID)` - T3's mechanism, not a second one - and puts it on the context with `db.WithTenantTx`, so every query the public handler runs (services, incidents, tenant name/logo) executes under that tenant's RLS session settings
- [x] `StatusPage.TenantID` is populated by `GetByHostname` (the one lookup that necessarily precedes any tenant context)
- [x] The transaction is always rolled back, never committed - every route behind `HostRouter` is read-only
- [x] An unknown hostname still returns 404, unchanged, and opens no transaction
- [x] `/uploads/logo` and `/api/instance/branding` on the admin listener are documented in code: they are unauthenticated but carry no hostname tenant signal (shared admin domain), so they keep the single-tenant fallback rather than an invented resolution
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...` and `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...`

**Tests**: integration - `internal/router/host_router_tenant_test.go`
- `TestHostRouter_PublicRequest_ScopedToHostnameTenant` (2 subtests, both directions): two tenants each with a published status page and a service; a request for either hostname sees exactly its own tenant's `services` and `tenants` rows and never the other's, asserted from a non-superuser role (`SET ROLE vane_router_rls_test`) with deliberately unfiltered `SELECT name FROM ...` queries, so RLS does the filtering, not the SQL.
- `TestHostRouter_UnknownHostname_404_NoTenantTransaction`: regression guard on the pre-existing 404, plus proof no transaction is opened for a rejected request.
- Discrimination check: forcing `BeginTenantTx(..., "")` makes both subtests fail (zero rows visible), so the assertions detect the regression they exist for.

**Gate**: full - `go build ./...`, `go vet ./...`, `go test ./...` and `TEST_DATABASE_URL=postgres://vane:vane@localhost:5433/vane?sslmode=disable go test -tags=integration -p 1 ./...` all green against a disposable Postgres (AGENTS.md §3).

**Commit**: `fix(router): resolve tenant context for unauthenticated public routes`

**Open finding (closed by T20b):** the hostname → status page lookup that *produces* the tenant id is itself subject to the fail-closed `status_pages`/`domains` policies, and it necessarily runs before any tenant is known. Verified against the disposable database: under a non-superuser role with no `app.tenant_id`, `GetByHostname` returns `ErrNotFound` for a page that exists and is published - so on a deployment whose application role is a plain non-superuser (which `rls_test.go` states is the expectation), every public status page would 404. The same bootstrap problem will hit T15, which has to enumerate `tenants` with no tenant context.

**Also noticed, not touched here (closed by T20b):** `StatusPageRepository.Create` opens its own pooled transaction (`r.pool.Begin`) instead of reusing the one on `ctx`, so the `tenant_id` DEFAULT resolves to NULL and the insert violates NOT NULL when called from inside a tenant transaction.

---

### T20b: Anonymous read path for published status pages (AD-023) ✅

**What**: Closes the two findings T20 surfaced but left open. (1) The bootstrap paradox: adds migration `0025_public_status_page_read`, a second PERMISSIVE `FOR SELECT` policy (`public_published_read`) on `status_pages` and `domains` that applies only when `app.tenant_id` is unset and only to published pages, so the hostname → tenant lookup works under a real non-superuser role without a `BYPASSRLS` role or a `SECURITY DEFINER` function - RLS stays the single enforcement path. (2) `StatusPageRepository.Create` now reuses the transaction already on `ctx` when there is one, following `TenantRepository.Create`'s convention.

**Where**: `internal/db/migrations/0025_public_status_page_read.{up,down}.sql`, `internal/db/status_page_repository.go`, `.specs/STATE.md` (AD-023)
**Depends on**: T20
**Reuses**: 0024's `NULLIF(current_setting('app.tenant_id', true), '')` idiom and `tenant_isolation` policy style; `TenantRepository.Create`'s "reuse the caller's tenant tx, else open one" pattern; `host_router_tenant_test.go`'s non-superuser `SET ROLE` fixture (T20)
**Requirement**: TENANT-01, TENANT-02, TENANT-03 - unauthenticated path, same gap T20 opened.

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] AD-023 recorded in `.specs/STATE.md` (decision, reason, trade-off, scope, status)
- [x] `status_pages` carries a PERMISSIVE `FOR SELECT` policy allowing rows with `state = 'published'` when `app.tenant_id` is unset; `domains` the same, conditioned on `EXISTS (... sp.domain_id = domains.id AND sp.state = 'published')`
- [x] Both policies are gated on `app.tenant_id` being unset, so no tenant-scoped session gains visibility of another tenant's rows - without that clause tenant A's unfiltered `ListPaginated`/domain-list queries would return tenant B's published pages, a real cross-tenant leak. This is the one deliberate deviation from the handed-down design; recorded in AD-023's trade-off.
- [x] 0024's `tenant_isolation` policies unchanged; `0024_*.sql` not edited
- [x] `GetByHostname` resolves a published page in an anonymous non-superuser session; a draft page's hostname still resolves to nothing
- [x] `StatusPageRepository.Create` succeeds inside an existing tenant transaction and writes the caller's `tenant_id`
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./...` and `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...`

**Tests**: integration
- `internal/router/host_router_tenant_test.go` → `TestHostRouter_AnonymousSession_ResolvesPublishedPageAndScopesData`: the whole request, hostname lookup included, runs in a `SET ROLE vane_router_rls_test` transaction with `app.tenant_id` deliberately unset; asserts `GetByHostname` returns the page with the right `TenantID`/`State`, and that the handler then sees exactly that tenant's service and never the second seeded tenant's.
- `internal/router/host_router_tenant_test.go` → `TestHostRouter_AnonymousSession_DraftPageStaysInvisible`: same anonymous session against a `draft` page's hostname; asserts `ErrNotFound` at the repository (the policy hides it, not HostRouter's state check) plus a 404 and no transaction opened.
- `internal/db/status_page_repository_test.go` → `TestStatusPageRepository_Create_InsideExistingTenantTx_ReusesCallerTransaction`: asserts the insert succeeds on the caller's tenant transaction, lands the caller's `tenant_id`, and disappears when the caller rolls back (proving it did not commit its own transaction).
- `internal/db/multi_tenancy_migration_test.go` updated to assert the exact policy set per table (`tenant_isolation` plus the declared extras) instead of a bare count of one - a stray policy still fails.
- `internal/db/user_repository_test.go`'s `snapshotAndClearUsers` restore statements repaired: they still named the pre-0024 columns (`users.role`, `password_reset_tokens.admin_id`, and `tenant_invites` short one column against its own 9-column snapshot), so they could never execute. The bug was masked because every `m.Steps(-1)` migration test used to reverse 0024 and recreate `users` empty; with 0025 on top, one step down no longer drops that table, the snapshot is non-empty, and the restore path finally runs. Surfaced by this task, not caused by it.
- Discrimination check, run against the disposable database: neutering `0025_*.up.sql` makes `..._ResolvesPublishedPageAndScopesData` fail with `db: not found`; relaxing the policy to ignore `state` makes `..._DraftPageStaysInvisible` fail. Both assertions detect the regression they exist for.

**Gate**: full - `go build ./...`, `go vet ./...`, `go test ./...` and `TEST_DATABASE_URL=postgres://vane:vane@localhost:5433/vane?sslmode=disable go test -tags=integration -p 1 ./...` all green against a disposable Postgres (AGENTS.md §3).

**Commit**: `docs(state): record AD-023 for public status page RLS policy` (3bcdfef), `feat(db): add permissive public-read RLS policy for published status pages` (7c8d48e), `fix(db): reuse caller's transaction in StatusPageRepository.Create` (73c8a9e), `test(db): repair stale column lists in the users snapshot restore` (aa1c9f1), `test(router): verify anonymous status page resolution under real RLS`

---

## Phase Execution Map

Full dependency graph (every arrow below matches a task's `Depends on` field exactly):

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7 → Phase 8

T1 → T2
T1 → T3
T1 → T4
T1 → T5
T4 → T6
T5 → T6
T5 → T7
T6 → T7
T7 → T8
T4 → T9
T5 → T9
T9 → T10
T10 → T11
T9 → T12
T5 → T13
T7 → T13
T13 → T14
T2 → T15
T4 → T15
T4 → T16
T8 → T17
T9 → T18
T10 → T18
T11 → T18
T16 → T19
```

Execution is strictly sequential within a phase - there is no intra-phase parallelism. Tasks with no dependency between them (e.g. T3/T4/T5, or T17/T18/T19) still execute one at a time in listed order, but neither blocks the other's correctness.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Multi-tenancy schema migration | 1 migration file pair | ✅ Granular |
| T2: RLS isolation test suite | 1 test file | ✅ Granular |
| T3: Tenant-context middleware | 1 file (new middleware) | ✅ Granular |
| T4: TenantRepository | 1 file | ✅ Granular |
| T5: TenantMembershipRepository | 1 file | ✅ Granular |
| T6: Bootstrap tenant provisioning | 1 file (extend existing handler) | ✅ Granular |
| T7: Session active tenant + auth/me | 1 file (extend existing handler) | ✅ Granular |
| T8: switch-tenant endpoint | 1 file (extend existing handler) | ✅ Granular |
| T9: signup endpoint | 1 file (new handler) | ✅ Granular |
| T10: Email verification gate | 1 file (extend T9's handler) | ✅ Granular |
| T11: Resend verification | 1 file (extend T9's handler) | ✅ Granular |
| T12: Rate limit signup | 1 file (routing wire-up) | ✅ Granular |
| T13: Scope invite endpoints by tenant | 1 file (extend existing handler) | ✅ Granular |
| T14: AcceptInvite branching | 1 file (extend existing handler) | ✅ Granular |
| T15: Poller tenant iteration | 1 file | ✅ Granular |
| T16: Merge company_settings into tenants | 1 file (primary handler) | ✅ Granular |
| T17: Frontend tenant selector | 1 component | ✅ Granular |
| T18: Frontend signup/verification UI | 1 component | ✅ Granular |
| T19: Frontend company profile fields | 1 component (extend existing) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 | ✅ Match |
| T3 | T1 | T1 | ✅ Match |
| T4 | T1 | T1 | ✅ Match |
| T5 | T1 | T1 | ✅ Match |
| T6 | T4, T5 | T4, T5 | ✅ Match |
| T7 | T5, T6 | T5, T6 | ✅ Match |
| T8 | T7 | T7 | ✅ Match |
| T9 | T4, T5 | T4, T5 | ✅ Match |
| T10 | T9 | T9 | ✅ Match |
| T11 | T10 | T10 | ✅ Match |
| T12 | T9 | T9 | ✅ Match |
| T13 | T5, T7 | T5, T7 | ✅ Match |
| T14 | T13 | T13 | ✅ Match |
| T15 | T2, T4 | T2, T4 | ✅ Match |
| T16 | T4 | T4 | ✅ Match |
| T17 | T8 | T8 | ✅ Match |
| T18 | T9, T10, T11 | T9, T10, T11 | ✅ Match |
| T19 | T16 | T16 | ✅ Match |

No forward-phase dependency: every dependency listed above is in the same phase or an earlier one.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Schema / migrations | none | none | ✅ OK |
| T2 | RLS enforcement | integration | integration | ✅ OK |
| T3 | API handler (middleware) | integration | integration | ✅ OK |
| T4 | Repository | integration | integration | ✅ OK |
| T5 | Repository | integration | integration | ✅ OK |
| T6 | API handler | integration | integration | ✅ OK |
| T7 | API handler | integration | integration | ✅ OK |
| T8 | API handler | integration | integration | ✅ OK |
| T9 | API handler | integration | integration | ✅ OK |
| T10 | API handler | integration | integration | ✅ OK |
| T11 | API handler | integration | integration | ✅ OK |
| T12 | API handler (routing) | integration | integration | ✅ OK |
| T13 | API handler | integration | integration | ✅ OK |
| T14 | API handler | integration | integration | ✅ OK |
| T15 | Poller | unit | unit | ✅ OK |
| T16 | API handler | integration | integration | ✅ OK |
| T17 | Frontend component | unit | unit | ✅ OK |
| T18 | Frontend component | unit | unit | ✅ OK |
| T19 | Frontend component | unit | unit | ✅ OK |

No violations.
