# Domain Health Monitoring Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/domain-health-monitoring/design.md`
**Status**: Approved

---

## Requirement IDs

Short codes assigned here for traceability (spec's Acceptance Criteria have no prior IDs of their own):

| ID | Acceptance Criterion (spec.md) |
| --- | --- |
| DHM-01 | P1-AC1: daily RDAP query per domain, expiration/registrar persisted |
| DHM-02 | P1-AC2: notification at 30/15/7-day thresholds, once per crossing |
| DHM-03 | P1-AC3: RDAP failure recorded, no spurious alert, cycle continues for other domains, retried next cycle |
| DHM-04 | P1-AC4: domain detail view shows expiration date, days remaining, registrar |
| DHM-05 | P2-AC1: first check with no baseline records current NS as baseline, no alert |
| DHM-06 | P2-AC2: NS differing from baseline marks drift and notifies |
| DHM-07 | P2-AC3: DNS lookup failure recorded, existing baseline never overwritten/cleared |
| DHM-08 | P2-AC4: domain detail view + domains list visibly indicate NS drift |
| DHM-09 | P3-AC1: every domain in `domains` table included in the cycle, with or without an attached status page |
| DHM-10 | P3-AC2: `AddDomainDrawer` hostname placeholder reads as a root-domain example, not a subdomain |

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/cli/digest_scheduler_test.go`, `internal/db/domain_repository_test.go`, `internal/notify/service_test.go` [inferred from `internal/email/notification_service_sender_test.go` and `notify.Service`'s existing shape], `web/src/features/domains/*`) plus spec ACs. Guidelines found: `AGENTS.md` §3 (backend gate commands, integration-DB hard rule) and §5 (frontend i18n/MSW-parity rule) — both applied directly below. No stricter project-specific coverage-threshold config found beyond `AGENTS.md` itself.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go migrations (`domains` columns, `notification_preferences` CHECK) | none | Build/gate only - schema changes verified by the repository/service tests that exercise them | `internal/db/migrations/` | `go build ./...` (embedded migration compile check) |
| Go repository (`DomainRepository.SetHealthCheckResult`) | integration | Key write path under RLS (tenant-scoped write succeeds, cross-tenant write blocked) + error handling (not-found id) - DHM-01, DHM-05, DHM-06, DHM-07 | `internal/db/domain_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go API (`domains_handler.go` response wiring) | integration | New response fields present and correctly populated for a domain with health data, and gracefully `null` for one without - DHM-04, DHM-08 | `internal/api/domains_handler_test.go` | `go test -tags=integration ./internal/api` |
| Go domain logic (RDAP client) | unit | All branches: success parse, expiration missing from response, unsupported/error response, network timeout - DHM-01, DHM-03 | `internal/rdap/client_test.go` (or colocated if inlined per design.md) | `go test ./internal/rdap` (or `./internal/cli`, matching final file location) |
| Go domain logic (threshold-crossing, NS-diff) | unit | 1:1 to spec ACs: each of 30/15/7-day crossings fires once and not on a later same-crossing check; no-baseline vs. drift vs. no-drift NS comparison; DNS-failure-preserves-baseline - DHM-02, DHM-05, DHM-06, DHM-07 | `internal/cli/domain_health_scheduler_test.go` | `go test ./internal/cli` |
| Go scheduler (`DomainHealthScheduler`) | integration | Leader election (exactly one replica runs, mirroring `TestDigestScheduler_LeaderElection_ExactlyOneReplicaRuns`), zero-domains no-op, full cycle against a local RDAP fixture server persists results and dispatches notifications - DHM-01 through DHM-09 | `internal/cli/domain_health_scheduler_test.go` | `go test -tags=integration ./internal/cli` |
| Go notify service (`notify.Service` new methods) | unit | Both new notification types: enabled-preference sends, disabled-preference skips, unknown-type error path - mirroring existing `notifyIncident` test coverage | `internal/notify/service_test.go` | `go test ./internal/notify` |
| Frontend components (`DomainDetailDrawer`, `DomainsTable`, `AddDomainDrawer`) | unit (vitest + Testing Library) | Health section renders all states (fresh/expiring/drift/error); list badge renders for at-risk domains; placeholder text is the corrected value - DHM-04, DHM-08, DHM-10 | `web/src/features/domains/*.test.tsx` | `npm run test` (in `web/`) |
| Frontend types/i18n | none (typecheck + parity script only) | New `Domain` fields typed; pt-BR/en keys in parity | `web/src/features/domains/`, `web/src/locales/*.json` | `npx tsc -b --noEmit && npm run i18n:check` (in `web/`) |

## Gate Check Commands

> Generated from `Makefile` (`test`, `test-integration` targets) and `AGENTS.md` §3 (backend) / §5 (frontend), matching the commands already established in prior features' `tasks.md`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test - **disposable Postgres container only, never `vane-dev-pg`** (`AGENTS.md` §3) | `make test-integration` |
| Build (backend) | After phase completion | `go build ./... && gofmt -l . && go vet ./...` |
| Frontend (unit) | After a frontend task | `cd web && npm run test` |
| Frontend (full) | After frontend phase completion | `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Schema

Tasks: T1, T2

### Phase 2: Repository and API

Tasks: T3, T4

### Phase 3: Notification Pipeline

Task: T5

### Phase 4: RDAP and Scheduler

Tasks: T6, T7, T8

### Phase 5: Frontend

Tasks: T9, T10, T11, T12

### Phase 6: Docs

Tasks: T13, T14

---

## Task Breakdown

### T1: Migration - `domains` table health columns

**What**: New migration adds seven nullable columns to `domains`: `expires_at timestamptz`, `registrar text`, `expected_ns text[]`, `current_ns text[]`, `ns_drift_detected boolean not null default false`, `last_rdap_check_at timestamptz`, `rdap_last_error text`. No backfill required (all nullable/defaulted).
**Where**: `internal/db/migrations/0041_domain_health_monitoring.up.sql`, `.down.sql`
**Depends on**: None
**Reuses**: existing migration file-pair convention (see `0040_integrations_tenant_scope.up.sql`/`.down.sql`)
**Requirement**: DHM-01, DHM-05 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `.up.sql` adds all seven columns with correct types/defaults; `.down.sql` drops them
- [x] Migration numbered `0041` (next after `0040`), matches naming convention exactly
- [x] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`
- [x] Test count: unchanged (this task adds no Go tests, verified via T3's integration tests exercising the new columns)

**Tests**: none
**Gate**: build

**Commit**: `feat(db): add domain health monitoring columns to domains table`

---

### T2: Migration - `notification_preferences` gains two new types

**What**: New migration alters the `notification_preferences` CHECK constraint to admit `'domain_expiring'` and `'domain_ns_drift'` alongside the existing three types. Corresponding Go constants `NotificationTypeDomainExpiring`/`NotificationTypeDomainNSDrift` added to `internal/db/notification_preference_repository.go`, with both defaulted to `true` in `notificationDefaultEnabled` (matching the two existing incident types' default-on posture - this is safety-relevant, not cosmetic, unlike the weekly digest's default-off).
**Where**: `internal/db/migrations/0042_domain_notification_types.up.sql`, `.down.sql`, `internal/db/notification_preference_repository.go`
**Depends on**: None
**Reuses**: existing CHECK-constraint alter pattern (drop + re-add with the extended `IN (...)` list, since Postgres has no `ALTER CHECK`), existing `notificationDefaultEnabled` map shape
**Requirement**: DHM-02, DHM-06 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] CHECK constraint admits all five types (three existing + two new); a row with an unrecognized sixth value still fails to insert
- [x] `NotificationTypeDomainExpiring = "domain_expiring"`, `NotificationTypeDomainNSDrift = "domain_ns_drift"` constants added
- [x] `notificationDefaultEnabled` includes both new types set `true`
- [x] `NotificationDefaultEnabled("domain_expiring")` and `NotificationDefaultEnabled("domain_ns_drift")` both return `true` in a new/updated unit test alongside the existing ones in `internal/db/notification_preference_repository_test.go`
- [x] Gate check passes: `go test ./internal/db && gofmt -l . && go vet ./...`
- [x] Test count: at least the same as before this task, plus 2+ new (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(db): add domain_expiring and domain_ns_drift notification types`

---

### T3: `DomainRepository.SetHealthCheckResult`

**What**: New repository method persisting one health-check cycle's outcome for a single domain: `SetHealthCheckResult(ctx context.Context, id string, expiresAt *time.Time, registrar *string, currentNS []string, driftDetected bool, rdapErr *string) error`. `Domain` struct gains the seven corresponding fields (mirroring T1's columns). Follows the `TenantTxFromContext`-aware pattern fixed today in `IncidentRepository.Create`/`StatusPageRepository.Create` (2026-09-28) - reuses the caller's tenant transaction when present, opens+wraps one only as fallback, never a bare `pool.Begin(ctx)`.
**Where**: `internal/db/domain_repository.go`, `internal/db/domain_repository_test.go`
**Depends on**: T1
**Reuses**: `TenantTxFromContext`/`WithTenantTx` (`internal/db/pool.go`), the `Create`/`insert` split pattern from `StatusPageRepository`/`IncidentRepository`
**Requirement**: DHM-01, DHM-05, DHM-06, DHM-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Domain` struct has `ExpiresAt *time.Time`, `Registrar *string`, `ExpectedNS []string`, `CurrentNS []string`, `NSDriftDetected bool`, `LastRDAPCheckAt *time.Time`, `RDAPLastError *string`
- [x] `SetHealthCheckResult` writes all seven fields under RLS, correctly scoped when a tenant transaction already exists on `ctx` (proven directly, not just "doesn't error")
- [x] `SetHealthCheckResult` against a nonexistent domain id returns a clear not-found-shaped error, not a silent no-op
- [x] A write attempted with a `ctx` scoped to a different tenant than the domain's owner is rejected by RLS (cross-tenant isolation proven, not assumed)
- [x] Gate check passes: `make test-integration`
- [x] Test count: at least the same as before this task, plus 4+ new (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add SetHealthCheckResult to DomainRepository`

---

### T4: Expose health fields on `GET /api/domains` and `GET /api/domains/{id}`

**What**: `domainResponse` struct and `toDomainResponse` (`internal/api/domains_handler.go`) gain the seven new fields (JSON: `expires_at`, `registrar`, `expected_ns`, `current_ns`, `ns_drift_detected`, `last_rdap_check_at`, `rdap_last_error`), so the admin API surfaces T3's data without a new endpoint.
**Where**: `internal/api/domains_handler.go`, `internal/api/domains_handler_test.go`
**Depends on**: T3
**Reuses**: existing `domainResponse`/`toDomainResponse` mapping pattern, existing list/detail handler tests as the template for the new assertions
**Requirement**: DHM-04, DHM-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A domain with health data populated (via `SetHealthCheckResult` in the test setup) returns all seven fields correctly in both `GET /api/domains` (list) and `GET /api/domains/{id}` (detail) responses (SPEC_DEVIATION: no `GET /api/domains/{id}` route exists and design.md scopes to `GET /api/domains`; fields ride on the list response — see the marker in `domains_handler.go`)
- [x] A domain with no health check run yet returns `null`/zero-value for the new fields without erroring
- [x] Gate check passes: `make test-integration`
- [x] Test count: at least the same as before this task, plus 2+ new (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): expose domain health fields on domains endpoints`

---

### T5: `notify.Service` gains domain health notifications

**What**: `notify.Service` gains `NotifyDomainExpiring(ctx, tenantID string, summary DomainExpiringSummary) error` and `NotifyDomainNSDrift(ctx, tenantID string, summary DomainNSDriftSummary) error`, mirroring `NotifyIncidentOpened`/`NotifyIncidentResolved`'s shape (resolve enabled recipients via `preferenceResolver.ResolveEnabledForUsers` with the new `NotificationTypeDomainExpiring`/`NotificationTypeDomainNSDrift` constants from T2, then `send` through the existing `emailSender`). Two new email templates added alongside the existing ones (`internal/email/templates.go`/`templates/`) with content: hostname, days-remaining/threshold (expiring) or expected-vs-current NS (drift), and a link to the domain's admin detail view.
**Where**: `internal/notify/service.go`, `internal/notify/service_test.go`, `internal/email/templates.go`, `internal/email/templates/` (new template files)
**Depends on**: T2
**Reuses**: `notifyIncident`'s structure (recipient resolution, `send` dispatch, `switch notificationType` in `(s *Service) send`), existing email template conventions
**Requirement**: DHM-02, DHM-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `NotifyDomainExpiring`/`NotifyDomainNSDrift` send to every member with that notification type enabled, and skip members with it disabled - proven directly (not inferred from the incident-notification tests' behavior)
- [x] Both new templates render hostname and their type-specific content (days remaining/threshold for expiring; expected vs. current NS for drift) without placeholder/blank fields
- [x] `(s *Service) send`'s `switch notificationType` handles both new types; an unrecognized type still returns the existing `"notify: unknown notification type %q"` error unchanged
- [x] Gate check passes: `go test ./internal/notify ./internal/email`
- [x] Test count: at least the same as before this task, plus 4+ new (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(notify): add domain expiring and NS drift notifications`

---

### T6: RDAP client

**What**: New `Lookup(ctx context.Context, hostname string) (expiresAt *time.Time, registrar *string, err error)` — HTTP GET against `https://rdap.org/domain/{hostname}` (bootstrap redirect to the correct registry RDAP server handled by `rdap.org` itself, per `net/http`'s default redirect-following), parses the `events` array for `eventAction == "expiration"`'s `eventDate`, and the `entities` array for the registrar's name. Base URL is an injected field (not a package-level constant) so tests point it at a local `httptest.Server` instead of the real network, per `AGENTS.md`'s "never depend on live external services in CI" spirit (matches `domain_verifier.go`'s own testable-via-injection shape).
**Where**: `internal/rdap/client.go` (new package; colocated in `internal/cli/domain_health_scheduler.go` instead if the parsing logic stays under ~100 lines total - final placement decided at implementation time per design.md), `internal/rdap/client_test.go`
**Depends on**: None
**Reuses**: stdlib `net/http` only, no third-party RDAP library (per design.md's confirmed decision)
**Requirement**: DHM-01, DHM-03

**Tools**:
- MCP: `context7` (RDAP JSON response shape is an IETF RFC 9083 structure, not a conventional library API - use to confirm the exact `events`/`entities` field shapes against the spec/reference examples before parsing, rather than guessing)
- Skill: NONE

**Done when**:
- [x] Successful RDAP JSON response (fixture matching a real registry's shape) parses `expiresAt` and `registrar` correctly
- [x] Response missing the expiration event parses to `expiresAt == nil` without erroring (some registries omit it)
- [x] Non-2xx HTTP response, malformed JSON, and network timeout all return a clear error, never a panic or a zero-value success
- [x] Gate check passes: `go test ./internal/rdap` (or `./internal/cli`, matching final placement)
- [x] Test count: at least 4 new tests (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(rdap): add RDAP client for domain expiration lookup`

---

### T7: `DomainHealthScheduler`

**What**: New `DomainHealthScheduler` (modeled on `DigestScheduler`'s exact shape: narrow interfaces for its dependencies, injected `now func() time.Time`, fixed daily fire via a `nextDomainHealthFire(now time.Time) time.Time` helper analogous to `nextDigestFire`, own Postgres advisory lock — new constant `domainHealthLeaderLockKey int64` in the `727200000-727299999` reserved block, value confirmed against `digestLeaderLockKey`/`db.PollerLeaderLockKey` to avoid collision at implementation time). Per tenant, per domain (via `DomainRepository.ListPaginated`, all domains regardless of attached status page - DHM-09): calls the T6 RDAP client and `net.LookupNS(hostname)` (injected as a func field for testability, matching `domain_verifier.go`'s `checkDNS` approach); computes threshold-crossing (30/15/7 days, fires only when the crossing is new since `LastRDAPCheckAt`'s prior state) and NS-diff (no baseline → learn it; baseline exists and differs → mark drift) as pure functions colocated in this file; calls T5's `notify.Service` methods on a crossing/drift; persists via T3's `SetHealthCheckResult` regardless of whether an alert fired. RDAP failure or NS-lookup failure for one domain is logged and does not abort the rest of the cycle (DHM-03, DHM-07).
**Where**: `internal/cli/domain_health_scheduler.go`, `internal/cli/domain_health_scheduler_test.go`
**Depends on**: T3, T5, T6
**Reuses**: `digest_scheduler.go`'s ticker/lock/shutdown skeleton and ticker-fire-helper pattern (`nextDigestFire`), `pglock` advisory lock usage, `poller.TenantTxFunc`-style tenant transaction opening, T6's RDAP client, T5's notification methods, T3's `SetHealthCheckResult`
**Requirement**: DHM-01 through DHM-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Leader election: exactly one of two concurrent scheduler instances runs a given cycle (test mirrors `TestDigestScheduler_LeaderElection_ExactlyOneReplicaRuns` exactly)
- [x] Zero domains registered: cycle completes as a no-op, no error, no notification
- [x] First check for a domain (no baseline): `ExpectedNS` gets set to the resolved NS, `NSDriftDetected` stays `false`, no drift notification fires
- [x] Second check with NS changed from baseline: `NSDriftDetected` becomes `true`, `NotifyDomainNSDrift` is called exactly once
- [x] Expiration crossing 30/15/7-day thresholds: `NotifyDomainExpiring` fires exactly once per threshold crossing, not on every daily check while still within that threshold band
- [x] RDAP failure for one domain in a multi-domain cycle: that domain's `RDAPLastError` is recorded, its `ExpectedNS`/`CurrentNS` untouched if the failure was NS-side, and every other domain in the same cycle still gets processed
- [x] Gate check passes: `make test-integration`
- [x] Test count: at least 6 new tests (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): add DomainHealthScheduler for daily RDAP and NS checks`

---

### T8: Wire `DomainHealthScheduler` into `serve.go`

**What**: `RunE` constructs a `DomainHealthScheduler` (same lifecycle wiring as the existing `DigestScheduler`: started as a goroutine, `Run(ctx)` stopped via the same graceful-shutdown context cancellation already in place) and starts it alongside the existing schedulers.
**Where**: `internal/cli/serve.go`
**Depends on**: T7
**Reuses**: existing scheduler-goroutine wiring pattern already present for `DigestScheduler` in `RunE`
**Requirement**: DHM-01 through DHM-09 (activation)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `DomainHealthScheduler.Run(ctx)` starts as a goroutine in `RunE`, alongside the existing digest scheduler and poller manager wiring
- [x] Graceful shutdown (context cancellation) stops it the same way it stops the existing schedulers - no orphaned goroutine
- [x] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`
- [x] Test count: unchanged (pure wiring, verified by T7's own tests plus the existing boot-wiring test style used for `DigestScheduler`, e.g. `TestNewDigestScheduler_BootWiring_RunStopsOnContextCancel` - add the equivalent for this scheduler if that pattern applies)

**Tests**: none (wiring covered by T7's tests; add a boot-wiring test only if it mirrors an existing one for `DigestScheduler` 1:1)
**Gate**: build

**Commit**: `feat(cli): start DomainHealthScheduler in serve.go`

---

### T9: Extend `Domain` frontend type and query with health fields

**What**: The frontend `Domain` TypeScript type (`web/src/features/domains/`) gains the seven new fields (camelCase, matching the existing mapping convention from the API's snake_case JSON). MSW mock handler (`web/src/test/msw/handlers.ts`) updated to return realistic values for the new fields on the domains list/detail endpoints, per `AGENTS.md` §5's mock-must-mirror-backend-shape rule.
**Where**: `web/src/features/domains/` (type definitions), `web/src/test/msw/handlers.ts`
**Depends on**: T4
**Reuses**: existing `Domain` type/mapping pattern, existing MSW handler structure for `/api/domains`
**Requirement**: DHM-04, DHM-08 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Domain` type includes `expiresAt`, `registrar`, `expectedNs`, `currentNs`, `nsDriftDetected`, `lastRdapCheckAt`, `rdapLastError`, correctly typed as optional/nullable matching the API's `null` behavior for unchecked domains (SPEC_DEVIATION: fields use the backend's snake_case keys - `expires_at`, `registrar`, `expected_ns`, `current_ns`, `ns_drift_detected`, `last_rdap_check_at`, `rdap_last_error` - see the marker in `web/src/types/api.ts`)
- [x] MSW mock returns both a domain with full health data and one with no health data yet, covering both states for T10/T11's tests
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`
- [x] Test count: unchanged (type-only change, verified by typecheck)

**Tests**: none
**Gate**: n/a (typecheck only, see Gate Check Commands)

**Commit**: `feat(web): add domain health fields to Domain type and MSW mocks`

---

### T10: `DomainDetailDrawer` health section

**What**: New "Saúde do domínio" section in `DomainDetailDrawer.tsx`: expiration date + days-remaining badge (color-coded: green >30d, yellow 15-30d, red <15d), registrar, current NS vs. expected NS (visually distinguished when they differ), last RDAP check timestamp, and a discrete warning when `rdapLastError` is set (non-blocking - the rest of the drawer renders normally). All copy through `react-i18next` (`AGENTS.md` §5), pt-BR and en keys added in parity.
**Where**: `web/src/features/domains/DomainDetailDrawer.tsx`, `web/src/features/domains/DomainDetailDrawer.test.tsx`, `web/src/locales/pt-BR.json`, `web/src/locales/en.json`
**Depends on**: T9
**Reuses**: `DomainStatusTag.tsx`'s badge conventions for the expiration/drift indicators
**Requirement**: DHM-04, DHM-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Drawer renders expiration date, days remaining, and registrar for a domain with health data
- [x] Drawer renders NS drift state distinctly (expected vs. current visibly different) when `nsDriftDetected` is `true`
- [x] Drawer shows the RDAP error indicator without breaking/hiding the rest of the drawer when `rdapLastError` is set
- [x] Drawer for a domain with no health data yet (all new fields `null`) renders without error, showing an appropriate "not checked yet" state rather than blank/broken UI
- [x] Gate check passes: `cd web && npm run test`
- [x] Test count: at least 4 new tests (no silent deletions)

**Tests**: unit
**Gate**: quick (frontend)

**Commit**: `feat(web): add domain health section to DomainDetailDrawer`

---

### T11: `DomainsTable` at-risk indicator

**What**: New badge/icon column in `DomainsTable.tsx` shown when a row's domain has `nsDriftDetected === true` or `expiresAt` within the 30-day warning window - visible without opening the detail drawer.
**Where**: `web/src/features/domains/DomainsTable.tsx`, `web/src/features/domains/DomainsTable.test.tsx`
**Depends on**: T9
**Reuses**: existing table row/badge component conventions
**Requirement**: DHM-04, DHM-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A row for a domain with `nsDriftDetected === true` shows the drift indicator
- [x] A row for a domain with `expiresAt` inside 30 days shows the expiring-soon indicator
- [x] A row for a healthy domain (no drift, expiration far out or unchecked) shows neither indicator
- [x] Gate check passes: `cd web && npm run test`
- [x] Test count: at least 3 new tests (no silent deletions)

**Tests**: unit
**Gate**: quick (frontend)

**Commit**: `feat(web): add at-risk indicator to DomainsTable`

---

### T12: Fix `AddDomainDrawer` hostname placeholder

**What**: Placeholder for the hostname input changes from a subdomain example to a root-domain example, since the subdomain used for a status page is created separately at attach time, not at domain registration. pt-BR (`web/src/locales/pt-BR.json:143`): `"status.suaempresa.com"` → `"suaempresa.com"`. en (`web/src/locales/en.json:143`): equivalent change (`"status.yourcompany.com"` → `"yourcompany.com"`).
**Where**: `web/src/locales/pt-BR.json`, `web/src/locales/en.json`
**Depends on**: None
**Reuses**: existing i18n key (`domains.form.hostnamePlaceholder`), no code change beyond the two JSON values
**Requirement**: DHM-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `hostnamePlaceholder` in both locale files reads as a root-domain example
- [ ] `npm run i18n:check` still passes (key parity unaffected, values-only change)
- [ ] Gate check passes: `cd web && npm run i18n:check`
- [ ] Test count: unchanged (copy-only change)

**Tests**: none
**Gate**: n/a (i18n parity check only)

**Commit**: `fix(web): correct AddDomainDrawer hostname placeholder to a root-domain example`

---

### T13: README - document domain health monitoring

**What**: Add a short section (or extend the existing Domains section) describing the new daily RDAP/NS health check: what it checks, the 30/15/7-day thresholds, that alerts flow through the existing notification preferences, and that no new environment variable is required (fixed daily schedule, no configurable interval).
**Where**: `README.md`
**Depends on**: T8
**Reuses**: existing `README.md` section structure/style
**Requirement**: DHM-01 through DHM-09 (documentation)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] New/extended section describes the RDAP expiration check, the NS drift check, and the alert thresholds in plain operator-facing language
- [ ] States explicitly that this covers domains with no attached status page (the root-domain case)
- [ ] Gate check passes: manual review (no automated test for prose)

**Tests**: none
**Gate**: n/a (docs)

**Commit**: `docs(readme): document domain health monitoring`

---

### T14: Record decision in `.specs/STATE.md`

**What**: Append a decision entry to `.specs/STATE.md`'s `## Decisions` section, using the next available `AD-NNN` (confirm the exact number against `STATE.md`'s current state at execution time - `AD-038` is the latest entry as of this tasks.md's writing, so this is expected to be `AD-039`, but another feature may have claimed it first). Records: root-domain health monitoring is passive-only (no `vane`-served-traffic capability added), NS baseline is learned automatically rather than operator-declared, RDAP-only with no WHOIS-text fallback, and the scheduler follows the `digest_scheduler.go` pattern rather than extending `internal/poller`.
**Where**: `.specs/STATE.md`
**Depends on**: T8
**Reuses**: existing `AD-NNN` entry format (see `AD-038` as the most recent example)
**Requirement**: n/a (project bookkeeping, not a spec AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-NNN` entry present with Decision/Reason/Trade-off/Scope/Date/Status fields, same shape as `AD-038`
- [ ] Number confirmed against `STATE.md`'s actual current state at the time this task runs, not assumed from this tasks.md
- [ ] Gate check passes: manual review (no automated test for docs)

**Tests**: none
**Gate**: n/a (docs)

**Commit**: `docs(specs): record AD-NNN for domain-health-monitoring`

---

## Phase Execution Map

This is the authoritative dependency graph - every edge below matches a task's `Depends on` field exactly (see the Diagram-Definition Cross-Check table):

```
T1 -> T3
T2 -> T5
T3 -> T4
T3 -> T7
T5 -> T7
T6 -> T7
T7 -> T8
T4 -> T9
T8 -> T13
T8 -> T14
T9 -> T10
T9 -> T11
```

`T12` has no dependencies and no dependents - it is an isolated copy fix, sequenced into Phase 5 for cohesion with the rest of the frontend work, not for a dependency reason.

Execution is strictly sequential within and across phases in task-number order (T1..T14) - there is no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `domains` columns migration | 1 migration pair | ✅ Granular |
| T2: `notification_preferences` types migration | 1 migration pair + 1 constants file | ✅ Granular |
| T3: `SetHealthCheckResult` | 1 repository method + struct fields | ✅ Granular |
| T4: API response wiring | 1 handler file, 1 DTO | ✅ Granular |
| T5: `notify.Service` methods | 1 service file, 2 methods, templates | ✅ Granular (cohesive - both methods share one mechanism) |
| T6: RDAP client | 1 new package/file, 1 function | ✅ Granular |
| T7: `DomainHealthScheduler` | 1 new file, 1 type | ✅ Granular (largest task - one tight dependency chain per design.md, matches `T4` in `tenant-domain-shared-listener` precedent for a wiring-heavy task) |
| T8: `serve.go` wiring | 1 file, 1 call site | ✅ Granular |
| T9: Frontend type + MSW | 1 type definition, 1 mock file | ✅ Granular |
| T10: `DomainDetailDrawer` section | 1 component | ✅ Granular |
| T11: `DomainsTable` indicator | 1 component | ✅ Granular |
| T12: Placeholder fix | 2 JSON values | ✅ Granular |
| T13: README docs | 1 file, 1 section | ✅ Granular |
| T14: `STATE.md` decision | 1 file, 1 entry | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming edge | ✅ Match |
| T2 | None | No incoming edge | ✅ Match |
| T3 | T1 | T1 -> T3 | ✅ Match |
| T4 | T3 | T3 -> T4 | ✅ Match |
| T5 | T2 | T2 -> T5 | ✅ Match |
| T6 | None | No incoming edge | ✅ Match |
| T7 | T3, T5, T6 | T3 -> T7, T5 -> T7, T6 -> T7 | ✅ Match |
| T8 | T7 | T7 -> T8 | ✅ Match |
| T9 | T4 | T4 -> T9 | ✅ Match |
| T10 | T9 | T9 -> T10 | ✅ Match |
| T11 | T9 | T9 -> T11 | ✅ Match |
| T12 | None | No incoming edge | ✅ Match |
| T13 | T8 | T8 -> T13 | ✅ Match |
| T14 | T8 | T8 -> T14 | ✅ Match |

Every edge in the `## Phase Execution Map` graph corresponds to exactly one `Depends on` entry above, and vice versa. No task depends on a later-phase task - all dependencies point backward or within the same phase (T6 sits in Phase 4 alongside its dependent T7, same as T1/T2 sit ahead of their Phase 2/3 dependents).

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `domains` migration | Go migration | none | none | ✅ OK |
| T2: notification types migration | Go migration + entity/const | none (constants covered by unit test explicitly listed in Done-when) | unit | ✅ OK (stricter than the floor, not a violation) |
| T3: `SetHealthCheckResult` | Go repository | integration | integration | ✅ OK |
| T4: API response wiring | Go API | integration | integration | ✅ OK |
| T5: `notify.Service` methods | Go notify service | unit | unit | ✅ OK |
| T6: RDAP client | Go domain logic | unit | unit | ✅ OK |
| T7: `DomainHealthScheduler` | Go scheduler | integration | integration | ✅ OK |
| T8: `serve.go` wiring | Go wiring (pure wiring) | n/a (covered by T7 + existing boot-wiring precedent) | none | ✅ OK |
| T9: Frontend type + MSW | Frontend types/i18n | none (typecheck only) | none | ✅ OK |
| T10: `DomainDetailDrawer` | Frontend component | unit | unit | ✅ OK |
| T11: `DomainsTable` | Frontend component | unit | unit | ✅ OK |
| T12: Placeholder fix | Frontend types/i18n | none | none | ✅ OK |
| T13: README | Docs | none | none | ✅ OK |
| T14: `STATE.md` | Docs | none | none | ✅ OK |

No violations - every task's `Tests` field matches its layer's matrix requirement.

---

## Tips

- **Phases are ordered** - Each phase completes before the next; tasks run in order within a phase
- **Reuses = Token saver** - Always reference existing code
- **One commit per task** - commit messages listed above
- **T7 is the load-bearing task** - it is where DHM-01 through DHM-09 actually converge (threshold logic, NS-diff logic, alert dispatch, persistence, all in one cycle). Its "RDAP failure for one domain doesn't abort the cycle" assertion is the one most likely to get weakened accidentally (e.g. an early `return` on first error) - test it with at least two domains in the fixture, one failing and one succeeding, and assert the succeeding one's result was actually persisted.
- **T3's cross-tenant RLS assertion is load-bearing** - given today's real incident (`IncidentRepository.Create` silently bypassing RLS via a bare `pool.Begin`), don't settle for "the write didn't error" as proof of correct scoping; prove a different tenant's transaction cannot read/write this domain's row.
- **14 tasks total** - exceeds the ~8-task single-batch threshold; at Execute time, offer sub-agent batching per phase groupings (e.g. Phases 1-3 as one batch, Phases 4-6 as a second) rather than assuming inline execution.
