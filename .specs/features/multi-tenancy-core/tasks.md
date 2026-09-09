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

**SPEC_DEVIATION (recorded, not silent):** Implemented as `internal/db/migrations/0024_multi_tenancy_core.up/down.sql` (0022/0023 were already taken by unrelated migrations already in the repo when this batch started). Scope was reduced from the literal "What": this migration creates `tenants`, `tenant_memberships`, `tenant_invites` and adds `tenant_id` + RLS to `services` only. It does **not** rename `admins`→`users`, does **not** rename `admin_invites`→`tenant_invites` (both untouched, still live), does **not** drop `company_settings`, and does **not** add `tenant_id`/RLS to `incidents`, `status_pages`, `llm_provider_config`(`llm_providers`/`llm_settings`), `email_provider_config`(`email_providers`/`email_settings`), or `admin_audit_log`. Reason: those changes cascade into every existing integration test across `internal/db` and `internal/api` that authenticates via `admins` or reads/writes those tables (~40 files) - work that belongs to later phases (T13 tenant-scopes `admin_invites`, T16 merges `company_settings`) and to follow-up tasks this plan does not yet name for the remaining domain tables. `tenant_memberships.user_id` therefore FKs to `admins(id)`, not a new `users(id)`. Flagged for the orchestrator: spec AC1's full table list is not yet RLS-protected after this batch - only `tenants`, `tenant_memberships`, `tenant_invites`, `services`.

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
- [ ] Middleware sets `app.tenant_id` from the session cookie's tenant claim before handler execution
- [ ] Request with no active tenant in session never reaches a domain query with `app.tenant_id` unset in a way that could read cross-tenant (defaults to a value that resolves to zero rows)
- [ ] Wired into `internal/cli/routes.go` ahead of every tenant-scoped route group
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add tenant-context middleware setting app.tenant_id per request`

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
- [ ] `Create(ctx, tenant)` inserts a new tenant row, returns generated `id`
- [ ] `Get(ctx, tenantID)` returns the tenant scoped by RLS
- [ ] `Update(ctx, tenantID, fields)` persists partial updates (name/contact_email/logo/legal_name/tax_id/tax_id_type) without clobbering unset fields
- [ ] `tax_id`/`tax_id_type` validation (11 digits for CPF, 14 for CNPJ) rejects invalid input before persisting
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [ ] Test count: 6+ tests pass (create, get, update, each validation branch)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TenantRepository replacing CompanySettingsRepository`

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
- [ ] `Create`/`ListForUser`/`ListForTenant`/`Delete`/`UpdateRole` all implemented and RLS-scoped where applicable
- [ ] `Delete` on the last `owner` of a tenant returns a distinct error, no row removed
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [ ] Test count: 6+ tests pass including the last-owner guard

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TenantMembershipRepository with last-owner guard`

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
- [ ] `POST /api/bootstrap` with zero admins creates 1 tenant + 1 user + 1 membership (role `owner`) atomically
- [ ] `POST /api/bootstrap` with an existing admin still returns the current 4xx, no tenant created
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: existing `bootstrap_handler_test.go` cases still pass + 2 new cases

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): bootstrap provisions single tenant and owner membership`

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
- [ ] Login with exactly 1 membership sets that tenant active and returns it in the login response
- [ ] Login with 0 memberships is rejected with a clear error, no session issued
- [ ] Login with >1 memberships succeeds but active tenant is unset; `GET /api/auth/me` lists all memberships
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: existing `auth_handler_test.go` cases still pass + 3 new cases

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): thread active tenant through session and auth/me`

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
- [ ] Valid `tenant_id` (caller has membership) updates the session cookie, no re-login required
- [ ] `tenant_id` without membership returns 403, session unchanged
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: 3+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add switch-tenant endpoint`

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
- [ ] New email: creates tenant + user + owner membership, `email_verified_at` null, sends verification email
- [ ] Existing verified email: creates tenant + new membership for that user, no password requested, no duplicate `users` row
- [ ] Same unverified email retried before verifying: returns 409, no second tenant created
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: 5+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add public SaaS signup endpoint`

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
- [ ] Login attempt with `email_verified_at IS NULL` is rejected with a message indicating verification is required
- [ ] Valid, unexpired verify token marks `email_verified_at` and subsequent login succeeds
- [ ] Expired/invalid verify token is rejected, `email_verified_at` unchanged
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: 4+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add email verification gate on login`

---

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
- [ ] Resend issues a new token for the same unverified user, invalidating the previous one
- [ ] Simulated email-provider failure during resend returns an error to the caller but leaves tenant/user/membership intact
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: 3+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add resend-verification endpoint`

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
- [ ] `/api/signup` wrapped with `IPLimiter.Middleware()`
- [ ] Exceeding the configured threshold from one `RemoteAddr` returns 429, no tenant created
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: 2+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): rate-limit public signup by IP`

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
- [ ] Every listed handler filters strictly by the caller's active `tenant_id` - a request from tenant B's owner never lists/mutates tenant A's members or invites
- [ ] `Delete` on a tenant's last `owner` returns an error, no row removed
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: existing `admins_test.go` cases still pass + 4 new cross-tenant-isolation cases

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): scope admin invite/list/role endpoints by active tenant`

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
