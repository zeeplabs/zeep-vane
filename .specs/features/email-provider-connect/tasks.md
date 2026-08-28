# Email Provider Connect Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

**Tools policy (user-approved)**: a task's `MCP: NONE` means no anticipated need, not a prohibition. Any worker may reach for Context7 (or WebFetch/WebSearch against official docs) mid-task when it hits a genuine unknown - an undocumented API shape, an unverifiable assumption, anything the Knowledge Verification Chain (SKILL.md) would otherwise flag as `[Incerto]`. T5 has a mandatory Context7/WebFetch step (see its Tools field); every other task's use is discretionary.

---

**Design**: `.specs/features/email-provider-connect/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/connectors/datadog/client_test.go`, `internal/api/integrations_handler_test.go`, `internal/db/integrations_migration_test.go`, `internal/cli/routes_test.go`, `web/src/features/integrations/*`). No `AGENTS.md`/`CONTRIBUTING.md` found - strong defaults applied where the sample didn't already establish a convention.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration / schema (`email_providers`, `email_settings`) | none | build gate only (no `_migration_test.go` pattern required for schema-only tables without business rules to assert - `company_settings`'s migration test exists because of its CHECK/seed row; this migration gets the same minimal seed-assertion treatment, see T1) | `internal/db/migrations/0016_*.sql`, `internal/db/email_providers_migration_test.go` (`//go:build integration`) | `go test -tags=integration ./...` |
| Repository (`EmailProviderRepository`) | integration | Key query paths + error handling: upsert-insert, upsert-overwrite, `Get` not-found, `List` empty/multi, `GetActiveProvider`/`SetActiveProvider` | `internal/db/email_provider_repository_test.go` (`//go:build integration`) | `go test -tags=integration ./...` |
| Leaf types (`Message`, `Provider` interface, typed errors) | none | build gate only - no logic to unit-test in interface/type definitions | `internal/email/provider.go` | `go build ./...` |
| Connectors (`sendgrid.Client`, `resend.Client`) | unit | All branches: valid key, invalid key (401), timeout, 5xx, successful `Send` | `internal/connectors/{sendgrid,resend}/client_test.go` | `go test ./...` |
| Templates (`admin_invite.html.tmpl`/`.txt.tmpl`) | none | Parse-only correctness folded into the Service task that renders them (T8) - a template with no renderer calling it yet is not independently verifiable, so its test is merged forward per the Tasks skill's "merge forward" rule | `internal/email/templates.go` | `go build ./...` |
| Service (`email.Service`) | unit | All branches; 1:1 to `EMAIL-01` through `EMAIL-09`; every listed edge case | `internal/email/service_test.go` | `go test ./...` |
| Handler (`EmailProvidersHandler`) | unit | All routes in scope: happy path + every edge case (404 unknown provider, 422 validation, 422 activate-not-connected) + error paths | `internal/api/email_providers_handler_test.go` | `go test ./...` |
| Routes wiring (`internal/cli/routes.go`) | integration | Role-gated reachability for the 3 new routes, same table-driven pattern as the existing Datadog rows | `internal/cli/routes_test.go` (`//go:build integration`) | `go test -tags=integration ./...` |
| Frontend hooks (`useEmailProviders`, `useConnectEmailProvider`, `useActivateEmailProvider`) | unit (Vitest + MSW) | Success + error path per hook, matching `web/src/features/integrations/hooks.test.ts` depth | `web/src/features/email-providers/hooks.test.ts` | `npm run test` (in `web/`) |
| Frontend page (`EmailProvidersPage`) | unit (Vitest + RTL) | Connect form happy path + `422` inline error, list render (connected/active/invalid), activate flow, role gating | `web/src/features/email-providers/EmailProvidersPage.test.tsx` | `npm run test` (in `web/`) |

## Gate Check Commands

> Generated from `.github/workflows/ci.yml` and `Makefile`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go unit) | After tasks touching only unit-tested Go layers (connectors, service, handler, leaf types) | `gofmt -l . && go vet ./... && go test ./...` |
| Full (Go + DB) | After tasks touching the repository, migration, or routes wiring | `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` (requires `TEST_DATABASE_URL`) |
| Web quick | After frontend hook/page tasks | `cd web && npx tsc -b --noEmit && npm run test` |
| Build (phase close) | After each phase completes | Full (above) **and** Web quick (above) **and** `cd web && npm run build` **and** `make build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation (schema + repository + contract types)

Tasks: T1, T2, T3, executed in that order. Full dependency edges for this phase are listed in the Phase Execution Map below.

### Phase 2: Provider connectors

Tasks: T4, T5, executed in that order.

### Phase 3: Business logic (Service + templates)

Tasks: T6, T7, T8, executed in that order.

### Phase 4: HTTP surface + wiring

Tasks: T9, T10, executed in that order.

### Phase 5: Admin UI (P2)

Tasks: T11, T12, executed in that order.

---

## Task Breakdown

### T1: Create `email_providers` + `email_settings` migration

**What**: `golang-migrate` pair `0016_email_providers.{up,down}.sql` creating both tables per design's Data Models section (CHECK-constrained `provider`/`status`, singleton `email_settings` seeded with `id=1`), plus a minimal migration test asserting the seed row exists and the CHECK constraints reject bad values.
**Where**: `internal/db/migrations/0016_email_providers.up.sql`, `internal/db/migrations/0016_email_providers.down.sql`, `internal/db/email_providers_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0012_company_settings.up.sql` (singleton pattern), `internal/db/migrations/0003_integrations.up.sql` (per-provider unique row pattern), `internal/db/company_settings_migration_test.go` (seed/CHECK assertion style)
**Requirement**: EMAIL-04 (singleton `active_provider` mechanism)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `email_providers` table exists with `provider UNIQUE CHECK IN ('sendgrid','resend')`, `status CHECK IN ('connected','invalid')` DEFAULT `'connected'`
- [x] `email_settings` singleton exists (`id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id=1)`), seeded, `active_provider` nullable FK to `email_providers(provider)` `ON DELETE SET NULL`
- [x] `down.sql` drops both tables cleanly
- [x] Migration test confirms seed row + both CHECK constraints reject invalid values
- [x] Gate passes: `gofmt -l . && go vet ./... && go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add email_providers and email_settings tables`

**Status**: ✅ Complete

---

### T2: Implement `EmailProviderRepository`

**What**: `internal/db/email_provider_repository.go` with `EmailProvider` struct and `UpsertProvider`/`Get`/`List`/`GetActiveProvider`/`SetActiveProvider`, per design's Components section.
**Where**: `internal/db/email_provider_repository.go`, `internal/db/email_provider_repository_test.go`
**Depends on**: T1
**Reuses**: `internal/db/integration_repository.go` (query/scan/error-wrapping shape, `ErrNotFound` convention)
**Requirement**: EMAIL-01, EMAIL-03, EMAIL-04, EMAIL-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `UpsertProvider` inserts on first connect, overwrites on reconnect (never a second row for the same `provider`)
- [x] `Get` returns `ErrNotFound` for a never-connected provider
- [x] `List` returns all connected providers ordered by `provider`, and an empty slice (not an error) when none exist
- [x] `GetActiveProvider` returns `""` (not an error) when `active_provider IS NULL`
- [x] `SetActiveProvider` updates the singleton row
- [x] Gate passes: `gofmt -l . && go vet ./... && go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add EmailProviderRepository`

**Status**: ✅ Complete

---

### T3: Define `internal/email` leaf contract types

**What**: `Message` struct, `Provider` interface (`Send`, `ValidateCredentials`), `ProviderFactory` type, shared typed errors (`ErrUnauthorized`, `ErrTimeout`, `ErrServer`), `Sender` interface, `AdminInviteEmailData` struct - exactly as specified in design's Components/Data Models sections. No logic, just the contract connectors and the Service build against.
**Where**: `internal/email/provider.go`
**Depends on**: None
**Reuses**: `internal/connectors/datadog/client.go`'s typed-error naming convention (`ErrUnauthorized`, `ErrTimeout`, `ErrServer`)
**Requirement**: EMAIL-07 (interface existence)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Message`, `Provider`, `ProviderFactory`, `Sender`, `AdminInviteEmailData` all defined per design
- [x] `go build ./...` succeeds (no consumers yet, so this is a compile-only check)

**Tests**: none
**Gate**: quick

**Commit**: `feat(email): define provider-agnostic contract types`

**Status**: ✅ Complete

---

### T4: Implement `internal/connectors/sendgrid.Client`

**What**: `NewClient(apiKey)`, `Send(ctx, email.Message) error` (`POST /v3/mail/send`), `ValidateCredentials(ctx) error` (`GET /v3/scopes`, 401 -> `email.ErrUnauthorized`), mirroring `datadog.Client`'s request/error-classification shape.
**Where**: `internal/connectors/sendgrid/client.go`, `internal/connectors/sendgrid/client_test.go`
**Depends on**: T3
**Reuses**: `internal/connectors/datadog/client.go` (`isTimeout` helper, status-code classification switch, `httptest.Server`-based test style from `client_test.go`)
**Requirement**: EMAIL-01, EMAIL-08

**Tools**:
- MCP: Context7 if `POST /v3/mail/send` request-body shape needs confirming beyond what's already verified (`GET /v3/scopes` behavior already confirmed via web research during Design)
- Skill: NONE

**Done when**:
- [x] `ValidateCredentials` returns nil on 200, `email.ErrUnauthorized` on 401
- [x] `Send` posts the message to `/v3/mail/send` with correct auth header and body shape
- [x] Timeout and 5xx classified as `email.ErrTimeout`/`email.ErrServer` (test via `httptest.Server` with delayed/5xx handlers, same pattern as `datadog/client_test.go`)
- [x] Gate passes: `gofmt -l . && go vet ./... && go test ./...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(connectors): add sendgrid email client`

**Status**: ✅ Complete

---

### T5: Implement `internal/connectors/resend.Client`

**What**: `NewClient(apiKey)`, `Send(ctx, email.Message) error` (`POST https://api.resend.com/emails`), `ValidateCredentials(ctx) error` (`GET https://api.resend.com/api-keys`, 401 -> `email.ErrUnauthorized`).
**Where**: `internal/connectors/resend/client.go`, `internal/connectors/resend/client_test.go`
**Depends on**: T3
**Reuses**: Same shape as T4 (`internal/connectors/sendgrid/client.go` once written, or `datadog/client.go` directly)
**Requirement**: EMAIL-01, EMAIL-08

**Tools**:
- MCP: Context7 (and/or WebFetch against `resend.com/docs/api-reference`) - REQUIRED before finalizing `ValidateCredentials`, to close the `[Provável]` gap on Resend's 401-on-invalid-key behavior and confirm `POST /emails`'s exact request body shape
- Skill: NONE

**Done when**:
- [x] `ValidateCredentials`/`Send` implemented against the endpoints above, request/response shapes confirmed via Context7/docs (not left as inferred)
- [x] Same test shape as T4 (`httptest.Server`-backed: valid, invalid, timeout, 5xx, successful send)
- [x] If live/doc verification confirms a different status code or shape than assumed in design.md, update design.md's Risks & Concerns row accordingly (SPEC_DEVIATION-style correction, same convention as `datadog/client.go`'s documented corrections)
- [x] Gate passes: `gofmt -l . && go vet ./... && go test ./...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(connectors): add resend email client`

**Status**: ✅ Complete

**Research finding**: Resend's docs (`introduction` + `errors` reference pages) state a genuinely invalid/unrecognized API key returns **403** ("The API key used was invalid"), not 401 as design.md originally assumed - 401 is reserved for a missing `Authorization` header or a send-only key hitting a non-send endpoint. `ValidateCredentials`/`Send` already classify both 401 and 403 as unauthorized (same pattern as the SendGrid/Datadog clients), so no code changed as a result - `design.md`'s Risks & Concerns row and `spec.md`'s Assumptions table were corrected to record the confirmed behavior instead of the stale `[Provável]` guess.

---

### T6: Add admin-invite email templates

**What**: `templates/admin_invite.html.tmpl` and `templates/admin_invite.txt.tmpl` (both taking `AdminInviteEmailData`: `CompanyName`, `Role`, `AcceptURL`), embedded and parsed once via `//go:embed` in `templates.go`. No test here - rendering correctness is verified in T8 where a `Service` actually calls the parsed templates (Tasks skill "merge forward" rule: an unrendered template isn't independently verifiable).
**Where**: `internal/email/templates/admin_invite.html.tmpl`, `internal/email/templates/admin_invite.txt.tmpl`, `internal/email/templates.go`
**Depends on**: T3
**Reuses**: Go stdlib `html/template`/`text/template`, `embed.FS`
**Requirement**: EMAIL-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Both templates exist, include `AcceptURL`, `Role`, `CompanyName` placeholders (spec EMAIL-09)
- [x] `templates.go` parses both via `//go:embed` + `template.Must(template.ParseFS(...))` (or non-panicking equivalent returning an error, per design's `NewService` fail-fast-at-boot decision)
- [x] `go build ./...` succeeds

**Tests**: none
**Gate**: quick

**Commit**: `feat(email): add admin-invite email templates`

**Status**: ✅ Complete

---

### T7: Implement `email.Service` - Connect / Activate / List

**What**: `NewService(repo, factory, masterKey, logger) (*Service, error)` plus `Connect`, `Activate`, `List`, per design's Components and Error Handling Strategy sections (encrypt-then-persist only after `ValidateCredentials` succeeds, `net/mail.ParseAddress` for `from_email`, typed `ErrInvalidInput`/`ErrValidationFailed`/`ErrProviderNotConnected`).
**Where**: `internal/email/service.go`, `internal/email/service_test.go`
**Depends on**: T2, T3
**Reuses**: `internal/crypto.Encrypt`/`Decrypt`, `internal/api/integrations_handler.go`'s "validate before persist, never persist on failure" flow as the behavioral template
**Requirement**: EMAIL-01, EMAIL-02, EMAIL-03, EMAIL-04, EMAIL-05, EMAIL-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Connect`: calls `factory(provider, apiKey)` then `ValidateCredentials`; on failure returns `ErrValidationFailed`, persists nothing; on success encrypts + upserts (EMAIL-01, EMAIL-03)
- [ ] `Connect`: missing/malformed input (`ErrInvalidInput`) never reaches the factory/network call (EMAIL-02)
- [ ] `Connect`: reconnecting a provider with a bad key leaves the previously-stored valid row untouched (edge case from spec)
- [ ] `Activate`: succeeds only when the target provider's stored `status == "connected"`; otherwise `ErrProviderNotConnected` (EMAIL-04, EMAIL-05)
- [ ] `List`: returns every connected provider + current `active_provider` (possibly `""`/none), never includes the encrypted key in any returned struct exposed beyond this package (EMAIL-06)
- [ ] Unit tests use a fake `EmailProviderStore` and fake `ProviderFactory` (no real DB, no real HTTP) - 1:1 coverage of every `Done when` item above plus every EMAIL-01..06 acceptance criterion and the two edge cases in spec's Edge Cases section that apply to Connect/Activate
- [ ] Gate passes: `gofmt -l . && go vet ./... && go test ./...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(email): implement Service connect/activate/list`

---

### T8: Implement `email.Service.SendAdminInvite`

**What**: `SendAdminInvite(ctx, to, data) error` - looks up the active provider, builds a `Provider` via the factory using the decrypted key, renders both templates from T6, calls `Provider.Send`; returns `ErrNoActiveProvider` with zero network calls when nothing is active, and propagates a send failure unmodified (no retry), per design's Error Handling Strategy.
**Where**: `internal/email/service.go` (extends T7's file), `internal/email/service_test.go` (extends T7's tests)
**Depends on**: T6, T7
**Reuses**: `internal/crypto.Decrypt`, the templates parsed in T6
**Requirement**: EMAIL-07, EMAIL-08, EMAIL-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] No active provider -> `ErrNoActiveProvider`, fake `Provider.Send` never called (EMAIL-08's "no active provider" AC)
- [ ] Active provider connected -> template rendered with `data` and passed to `Provider.Send` via the decrypted key + stored `from_email`/`from_name` (EMAIL-07, EMAIL-09)
- [ ] `Provider.Send` failure -> error returned to caller unmodified, exactly one call made (no retry) (EMAIL-08's failure-propagation AC)
- [ ] Rendered content (both HTML and text bodies) contains the `AcceptURL`, `Role`, and `CompanyName` substitutions - the actual template-correctness test deferred from T6
- [ ] Gate passes: `gofmt -l . && go vet ./... && go test ./...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(email): implement Service.SendAdminInvite`

---

### T9: Implement `EmailProvidersHandler`

**What**: `NewEmailProvidersHandler(svc, logger)` + `Connect`/`List`/`Activate` HTTP handlers per design (404 for unknown `provider` path segment, JSON response shapes from spec's ACs, never echoing `api_key`).
**Where**: `internal/api/email_providers_handler.go`, `internal/api/email_providers_handler_test.go`
**Depends on**: T7, T8
**Reuses**: `internal/api/integrations_handler.go` (handler shape, JSON response helpers `writeInternalError`, narrow-interface pattern, `422`/`404` response style)
**Requirement**: EMAIL-01, EMAIL-02, EMAIL-04, EMAIL-05, EMAIL-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `POST /api/integrations/email/{provider}`: unknown provider -> `404`; validation failure -> `422`; success -> `201` with no key material in the body
- [ ] `GET /api/integrations/email`: returns `{active_provider, providers: [...]}` shape, empty list (not `404`) when nothing connected, no key material
- [ ] `POST /api/integrations/email/{provider}/activate`: not-connected provider -> `422`; success -> `200`
- [ ] Handler tests use a fake `emailProviderService` (no real DB) - happy path + every edge case above
- [ ] Gate passes: `gofmt -l . && go vet ./... && go test ./...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(api): add email providers handler`

---

### T10: Wire routes and `ProviderFactory` in `internal/cli/routes.go`

**What**: Construct `email.ProviderFactory` (switches `sendgrid`/`resend` to the T4/T5 clients), `email.NewService(...)`, `api.NewEmailProvidersHandler(...)`, and register the 3 routes with the same `writeRoles`/`anyRole` split as the existing Datadog integration routes. Add the 3 new routes to `routes_test.go`'s existing role-auth table (same table `POST /api/integrations/datadog` already lives in).
**Where**: `internal/cli/routes.go`, `internal/cli/routes_test.go`
**Depends on**: T4, T5, T9
**Reuses**: `internal/cli/routes.go`'s existing Datadog wiring block (lines around `integrationsHandler := api.NewIntegrationsHandler(...)`) as the direct template; `routes_test.go`'s existing table-driven role-auth test

**Requirement**: EMAIL-01, EMAIL-04, EMAIL-06 (auth boundary assumption from spec)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `POST /api/integrations/email/{provider}` and `POST /api/integrations/email/{provider}/activate` gated by `writeRoles`; `GET /api/integrations/email` gated by `anyRole` - matching the Datadog split exactly
- [ ] `routes_test.go`'s role-auth table includes all 3 new routes and passes for owner/operator/viewer per the existing pattern
- [ ] Full admin router boots successfully with the new handler wired (no nil-dependency panic)
- [ ] Gate passes: `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): wire email provider routes`

---

### T11: Add `web/src/features/email-providers/hooks.ts`

**What**: `useEmailProviders()` (query), `useConnectEmailProvider(provider)`, `useActivateEmailProvider()` (mutations) against the T10 endpoints, matching `web/src/features/integrations/hooks.ts`'s shape and cache-update behavior exactly.
**Where**: `web/src/features/email-providers/hooks.ts`, `web/src/features/email-providers/hooks.test.ts`, `web/src/test/msw/handlers.ts` (add the 3 new MSW handlers)
**Depends on**: T10
**Reuses**: `web/src/features/integrations/hooks.ts` (query/mutation shape, `apiClient` usage, cache invalidation pattern)
**Requirement**: EMAIL-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useEmailProviders` fetches and exposes `active_provider` + `providers` list
- [ ] `useConnectEmailProvider`/`useActivateEmailProvider` mutate and invalidate/update the `useEmailProviders` cache on success
- [ ] Error paths (`422`) surface via `ApiError`, matching `useConnectDatadog`'s existing error-propagation shape
- [ ] Tests pass via MSW-mocked fetch (no real network), same depth as `web/src/features/integrations/hooks.test.ts`
- [ ] Gate passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: web quick

**Commit**: `feat(web): add email provider hooks`

---

### T12: Add `EmailProvidersPage` admin UI

**What**: Page component listing SendGrid/Resend connection state, a connect form per provider (`api_key`, `from_email`, `from_name`), and an activate action - role-gated (`hasRole(["owner","operator"])`) exactly like `IntegrationsPage.tsx`.
**Where**: `web/src/features/email-providers/EmailProvidersPage.tsx`, `web/src/features/email-providers/EmailProvidersPage.test.tsx`, plus routing entry wherever `IntegrationsPage` is currently mounted (sidebar/router config)
**Depends on**: T11
**Reuses**: `web/src/features/integrations/IntegrationsPage.tsx` (form open/submit/error pattern, `Card`/`Field`/`Button`/`Tag` components, `formatTimestamp` helper, role gate)
**Requirement**: EMAIL-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Page displays both providers' state (not connected / connected / invalid) and which is active
- [ ] Connect form submits and shows inline `422` error on failure (matching `IntegrationsPage`'s `ApiError` handling)
- [ ] Activate action updates displayed active provider without a full reload
- [ ] Non-`writeRoles` users see the page in read-only form (no connect/activate controls), matching `IntegrationsPage`'s `canManage` gate
- [ ] Tests pass via MSW-mocked fetch, same depth as `web/src/features/integrations/IntegrationsPage.test.tsx`
- [ ] Gate passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: web quick

**Commit**: `feat(web): add email providers admin page`

---

## Phase Execution Map

Full dependency graph (every edge below matches a task's `Depends on` field exactly):

```
T1  -> T2
T2  -> T7
T3  -> T4
T3  -> T5
T3  -> T6
T3  -> T7
T4  -> T10
T5  -> T10
T6  -> T8
T7  -> T8
T7  -> T9
T8  -> T9
T9  -> T10
T10 -> T11
T11 -> T12
```

Phase grouping (for readability only, not a second dependency graph): Phase 1 = T1, T2, T3. Phase 2 = T4, T5. Phase 3 = T6, T7, T8. Phase 4 = T9, T10. Phase 5 = T11, T12.

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

**Reading the cross-phase edges**: T4/T5 depend only on T3 (connectors never touch the database, so T1/T2 aren't prerequisites). T7 depends on T2 and T3 but not on T4/T5 - its tests use a fake `Provider`/`ProviderFactory`, never a real connector. T9 needs T7 and T8 (the complete `Service`). T10 needs T4, T5 (concrete connectors) and T9 (the handler) together.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration + minimal seed/CHECK test | 1 migration pair + 1 test file | ✅ Granular |
| T2: `EmailProviderRepository` | 1 file (5 cohesive methods on 1 repository) | ✅ Granular |
| T3: `internal/email` leaf types | 1 file, pure type/interface definitions | ✅ Granular |
| T4: SendGrid client | 1 component (1 connector) | ✅ Granular |
| T5: Resend client | 1 component (1 connector) | ✅ Granular |
| T6: Admin-invite templates | 2 template files + 1 embed/parse file, cohesive single concern | ✅ Granular |
| T7: `Service` connect/activate/list | 1 file, 3 cohesive methods sharing 1 constructor/state | ✅ Granular |
| T8: `Service.SendAdminInvite` | 1 method, extends T7's file | ✅ Granular |
| T9: `EmailProvidersHandler` | 1 file, 3 cohesive HTTP handlers | ✅ Granular |
| T10: Routes wiring | 1 file modification (routes.go) + 1 test table extension | ✅ Granular |
| T11: Frontend hooks | 1 file (3 cohesive hooks) + MSW handlers | ✅ Granular |
| T12: Frontend page | 1 component | ✅ Granular |

**Granularity check**: every task is 1 component / 1 cohesive file, or 2-3 tightly related things in the same file (T1's migration pair, T6's template pair) - none crosses into "multiple unrelated files/components."

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (no incoming edge) | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | None | (no incoming edge) | ✅ Match |
| T4 | T3 | T3 -> T4 | ✅ Match |
| T5 | T3 | T3 -> T5 | ✅ Match |
| T6 | T3 | T3 -> T6 | ✅ Match |
| T7 | T2, T3 | T2 -> T7, T3 -> T7 | ✅ Match |
| T8 | T6, T7 | T6 -> T8, T7 -> T8 | ✅ Match |
| T9 | T7, T8 | T7 -> T9, T8 -> T9 | ✅ Match |
| T10 | T4, T5, T9 | T4 -> T10, T5 -> T10, T9 -> T10 | ✅ Match |
| T11 | T10 | T10 -> T11 | ✅ Match |
| T12 | T11 | T11 -> T12 | ✅ Match |

T3 has no dependency (its `Depends on: None` is correct - it's independent of T1/T2, pure type definitions) and no incoming diagram edge either, so nothing to reconcile there.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migration | Migration / schema | none (build gate only) | none | ✅ OK |
| T2: `EmailProviderRepository` | Repository | integration | integration | ✅ OK |
| T3: Leaf types | Leaf types (entity/config) | none | none | ✅ OK |
| T4: SendGrid client | Connector | unit | unit | ✅ OK |
| T5: Resend client | Connector | unit | unit | ✅ OK |
| T6: Templates | Templates | none (merged forward to T8) | none | ✅ OK |
| T7: Service (connect/activate/list) | Service | unit | unit | ✅ OK |
| T8: Service (send) | Service | unit | unit | ✅ OK |
| T9: Handler | Handler | unit | unit | ✅ OK |
| T10: Routes wiring | Routes wiring | integration | integration | ✅ OK |
| T11: Frontend hooks | Frontend hooks | unit | unit | ✅ OK |
| T12: Frontend page | Frontend page | unit | unit | ✅ OK |

No violations. T6's `Tests: none` is not test deferral in the prohibited sense - the matrix itself designates templates as `none` with rendering correctness explicitly assigned to T8 (documented in both the matrix row and T6's own `Done when`), matching the Tasks skill's sanctioned "merge forward" resolution for code that can't be verified until a later task completes.

---
