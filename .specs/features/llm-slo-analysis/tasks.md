# LLM SLO Analysis Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/llm-slo-analysis/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/email/*_test.go`, `internal/connectors/{resend,sendgrid}/client_test.go`, `internal/poller/*_test.go`, `internal/db/email_providers_migration_test.go`, `internal/api/email_providers_handler_test.go`, `web/src/features/public-status/PublicStatusPage.test.tsx`). Confirm before Execute.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migrations | integration (build-tag) | Table/column exists, constraints enforced, up+down both apply cleanly | `internal/db/*_migration_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| Domain / business logic (`internal/llm`, `internal/poller/analyzer.go`) | unit | All branches; 1:1 to spec ACs; every listed edge case has a test | `internal/llm/*_test.go`, `internal/poller/analyzer_test.go` | `go test ./internal/llm/... ./internal/poller/...` |
| Connector (`internal/connectors/openai`) | unit | Happy path + every documented failure classification (401/403/5xx/timeout/malformed), mirrors `resend`/`sendgrid` depth | `internal/connectors/openai/*_test.go` | `go test ./internal/connectors/openai/...` |
| Repository (`internal/db`) | integration (build-tag) | Key query/write paths + error handling (not-found, constraint violation) | `internal/db/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| API handler (`internal/api`) | unit (httptest) | All routes in scope: happy path + every listed edge case + error/failure paths | `internal/api/*_handler_test.go` | `go test ./internal/api/...` |
| Poller wiring / non-blocking dispatch | unit (fakes, no real network/DB) | Timing-bounded: a hung LLM call must not delay `pollOnce`'s remaining services beyond a fixed bound | `internal/poller/analyzer_test.go`, `internal/poller/poller_test.go` (extended) | `go test ./internal/poller/...` |
| Frontend hooks/lib (`web/src/lib`, `web/src/features/*/hooks.ts`) | unit (vitest) | All branches, matches existing hook test depth | `web/src/**/*.test.ts(x)` | `npm run test` |
| Frontend component (Settings AI section, tooltip/description rendering) | unit (vitest + testing-library) | Happy path + loading + error + empty states, matches `PublicStatusPage.test.tsx` depth | `web/src/**/*.test.tsx` | `npm run test` |
| Entity / config / migration SQL itself | none | build gate only | - | build gate only |

## Gate Check Commands

> Generated from `AGENTS.md` §3. Confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend unit) | After a task touching only non-DB Go code | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full (backend + integration) | After a task touching migrations or `internal/db` | Quick, plus: spin up disposable Postgres (`AGENTS.md` §3 exact commands, port 5433, `max_connections=300`), `TEST_DATABASE_URL=postgres://vane:vane@localhost:5433/vane?sslmode=disable go test -tags=integration ./...`, then `docker stop vane-test-pg` |
| Frontend | After any `web/` task | `npx tsc -b --noEmit && npm run test` |
| Build (phase completion) | After the last task of any phase | Quick + (Full if the phase touched `internal/db`) + Frontend if the phase touched `web/` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Schema

Executed in order T1, T2, T3 (no data dependency between them - all three are independent additive migrations; sequenced only to keep the batch worker's commit history linear). Full arrow-level detail: see the **Phase Execution Map** below, the authoritative diagram this file's `Depends on` fields are cross-checked against.

### Phase 2: LLM provider-agnostic layer

Executed in order T4, T5, T6, T7. Builds on nothing but the schema (T1's `llm_providers`/`llm_settings` tables). T5 and T6 both depend on T4 only (not on each other); T7 depends on both T5 and T6.

### Phase 3: OpenAI connector

Executed in order T8, T9. Independent of Phase 2's `Service` internals, depends only on `llm.Provider`/typed errors from T4.

### Phase 4: Repository layer

Executed in order T10, T11, T12 (no data dependency between them - each extends a different table/repository; sequenced for a linear commit history). Extends `internal/db` for incidents/services; independent of Phases 2-3.

### Phase 5: Poller bridge

Executed in order T13, T14, T15. Needs T7 (`llm.Service.Generate*`), T11, T12 (repository methods it calls).

### Phase 6: Admin/public API

Executed in order T16, T17, T18, T19. Needs T5, T9 (llm.Service + factory), T11, T12 (repository methods).

### Phase 7: Frontend

Executed in order T20, T21, T22, T23, T24. Needs T16, T17, T18 (the endpoints/DTOs it calls).

### Phase 8: Docs sync

T25 only. Needs the full feature implemented (accurate to describe).

---

## Task Breakdown

### T1: `llm_providers`/`llm_settings` migration

**What**: Create migration `0021_llm_providers` (`up`/`down`) with the `llm_providers` and `llm_settings` tables exactly as specified in design.md's Data Models section, plus its migration test.
**Where**: `internal/db/migrations/0021_llm_providers.up.sql`, `internal/db/migrations/0021_llm_providers.down.sql`, `internal/db/llm_providers_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0016_email_providers.up.sql`/`.down.sql` shape; `internal/db/email_providers_migration_test.go` test shape
**Requirement**: AI-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `llm_providers` table exists with `provider` `UNIQUE CHECK (provider IN ('openai'))`, `encrypted_api_key BYTEA NOT NULL`, `model TEXT NOT NULL`, `status`, `last_checked_at`, `last_error`
- [x] `llm_settings` singleton table exists with `active_provider` FK'd to `llm_providers.provider`, seeded row present
- [x] `down` migration cleanly drops both tables
- [x] Migration test confirms both tables exist with the expected columns/constraints after `MigrateUp`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add llm_providers and llm_settings tables`

---

### T2: `incidents` AI-fields migration

**What**: Create migration `0022_incident_ai_fields` adding `description TEXT`, `pending_close_comment TEXT`, `auto_created BOOLEAN NOT NULL DEFAULT false` to `incidents`, plus its migration test.
**Where**: `internal/db/migrations/0022_incident_ai_fields.up.sql`, `internal/db/migrations/0022_incident_ai_fields.down.sql`, `internal/db/incident_ai_fields_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0019_admin_name_phone.up.sql` (additive-column migration shape), `internal/db/incidents_migration_test.go` test shape
**Requirement**: AI-09, AI-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `incidents.description`, `incidents.pending_close_comment`, `incidents.auto_created` exist with correct types/defaults
- [x] `down` migration cleanly drops all three columns
- [x] Migration test confirms columns/defaults after `MigrateUp`, and confirms an existing manually-created incident row (no `description` supplied) has `description IS NULL`, `auto_created = false`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add description, pending_close_comment, auto_created to incidents`

---

### T3: `services.status_analysis` migration

**What**: Create migration `0023_service_status_analysis` adding nullable `status_analysis TEXT` to `services`, plus its migration test.
**Where**: `internal/db/migrations/0023_service_status_analysis.up.sql`, `internal/db/migrations/0023_service_status_analysis.down.sql`, `internal/db/service_status_analysis_migration_test.go`
**Depends on**: None
**Reuses**: Same additive-column migration shape as T2
**Requirement**: AI-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `services.status_analysis` exists, nullable, no default
- [x] `down` migration cleanly drops the column
- [x] Migration test confirms the column exists and defaults to `NULL` on an existing service row

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add status_analysis to services`

---

### T4: `llm.Provider` interface and typed errors

**What**: Define the `llm.Provider` interface (`Complete`, `ValidateCredentials`), `llm.ProviderFactory`, and the package's typed errors (`ErrUnauthorized`, `ErrTimeout`, `ErrServer`), exactly mirroring `internal/email/provider.go`'s shape.
**Where**: `internal/llm/provider.go`
**Depends on**: None
**Reuses**: `internal/email/provider.go` (structure, doc-comment style, typed-error set)
**Requirement**: AI-25

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Provider` interface has `Complete(ctx, systemPrompt, userPrompt string) (string, error)` and `ValidateCredentials(ctx) error`
- [x] `ProviderFactory func(provider, apiKey, model string) (Provider, error)` defined
- [x] `ErrUnauthorized`/`ErrTimeout`/`ErrServer` defined as package-level `errors.New` values
- [x] `go build ./internal/llm/...` succeeds (no implementation yet, interface-only file)

**Tests**: none (interface/entity layer - matrix says none, build gate only)
**Gate**: quick

**Commit**: `feat(llm): add Provider interface and typed errors`

---

### T5: `llm.Service` Connect/SetModel/Activate/List

**What**: Implement `Service` with `Connect`, `SetModel`, `Activate`, `List`, its `LLMProviderStore` narrow interface, and `ListResult`/`ProviderStatus` types - mirroring `internal/email/service.go`'s equivalent methods. `SetModel` validates the requested model against a fixed per-provider allowlist (`openai`: `gpt-4o-mini`, `gpt-4o`, `gpt-4.1-mini`, `gpt-4.1`) before persisting.
**Where**: `internal/llm/service.go`
**Depends on**: T4
**Reuses**: `internal/email/service.go`'s `Connect`/`Activate`/`List` bodies almost verbatim; `internal/crypto.Encrypt`/`Decrypt`
**Requirement**: AI-01, AI-02, AI-03, AI-04, AI-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Connect` validates via `Provider.ValidateCredentials` before persisting (AI-01), defaults `model` to `gpt-4o-mini` when empty (AI-03), returns `ErrInvalidInput`/`ErrValidationFailed` without persisting on failure (AI-02)
- [ ] `SetModel` rejects a model outside the allowlist without touching the stored row, persists a valid model change (AI-04)
- [ ] `Activate` returns `ErrProviderNotConnected` for an unconnected provider
- [ ] `List` never includes any provider's encrypted or decrypted API key in its return value
- [ ] Unit tests: valid connect persists encrypted key + default model; invalid key rejected + nothing persisted; explicit model accepted; `SetModel` rejects unknown model; `SetModel` accepts known model; `Activate` on unconnected provider fails; `List` never leaks key material
- [ ] Gate passes: `go test ./internal/llm/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(llm): implement Connect, SetModel, Activate, List`

---

### T6: `AnalysisInput` and prompt construction

**What**: Define `AnalysisInput` (per design.md's Data Models) and `internal/llm/prompts.go`'s three prompt-builder functions (degraded tooltip, outage description, closing comment), each returning `(systemPrompt, userPrompt string)` built from an `AnalysisInput`.
**Where**: `internal/llm/prompts.go`
**Depends on**: T4
**Reuses**: Nothing existing (new prompt-construction concept); follows the plain-value-object style of `email.Message`
**Requirement**: AI-25, AI-26

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AnalysisInput` struct matches design.md exactly (`ServiceName`, `SLOState`, `SLI`, `Target`, `Timeframe`, `ErrorBudgetRemaining`)
- [ ] Three prompt builders each produce a system prompt instructing a short (1-2 sentence) factual, non-alarmist output, and a user prompt embedding the `AnalysisInput` fields
- [ ] Unit tests confirm each builder includes the service name and SLO state in its output prompt (regression against silently dropping a field)
- [ ] Gate passes: `go test ./internal/llm/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(llm): add AnalysisInput and prompt construction`

---

### T7: `llm.Service.Generate*` methods

**What**: Add `GenerateDegradedAnalysis`, `GenerateOutageDescription`, `GenerateClosingComment` to `Service` - each resolves the active provider (returns `ErrNoActiveProvider` if none), builds the provider client via the factory, calls the matching prompt builder from T6, and returns `Provider.Complete`'s result untouched (empty/malformed-response handling is the caller's - `SLOAnalyzer`'s - responsibility per design's Error Handling Strategy, not duplicated here).
**Where**: `internal/llm/service.go` (extend)
**Depends on**: T5, T6
**Reuses**: `email.Service.SendAdminInvite`'s active-provider-resolution body (decrypt key, build client via factory) almost verbatim
**Requirement**: AI-08 through AI-24 (all three Generate* call sites)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All three methods return `ErrNoActiveProvider` immediately when no provider is active, without attempting any network call
- [ ] All three methods correctly decrypt the stored key and pass the stored model to the factory
- [ ] Unit tests (fake `Provider`/factory): each method returns the fake's output on success, propagates the fake's error on failure, and confirms `ErrNoActiveProvider` short-circuits before touching the factory
- [ ] Gate passes: `go test ./internal/llm/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(llm): add GenerateDegradedAnalysis, GenerateOutageDescription, GenerateClosingComment`

---

### T8: `connectors/openai.Client` - Complete + ValidateCredentials

**What**: Implement `Client` satisfying `llm.Provider`: `Complete` via `POST /v1/chat/completions` (`max_tokens` capped ~200), `ValidateCredentials` via `GET /v1/models`, with the same 401/403→`ErrUnauthorized`, 5xx→`ErrServer`, timeout→`ErrTimeout` classification `resend.Client.do` uses.
**Where**: `internal/connectors/openai/client.go`
**Depends on**: T4
**Reuses**: `internal/connectors/resend/client.go`'s `do`/timeout-classification/`post` shape (pattern, not shared code - different package/domain, same reasoning `resend`/`sendgrid` already don't share code with each other)
**Requirement**: AI-01, AI-02

**Tools**:
- MCP: NONE
- Skill: NONE (research already done in design.md's Components section - OpenAI's documented request/response shape for `/v1/chat/completions` and `/v1/models`)

**Done when**:
- [ ] `NewClient(apiKey, model string) *Client` builds a client with a bounded `http.Client` timeout (matches `resend`'s `defaultTimeout = 10 * time.Second`, doubled per design.md's LLM-call-is-slower reasoning if warranted - confirm against real OpenAI latency expectations, document the chosen value in a doc comment)
- [ ] `Complete` sends the correct request shape (`model`, `messages: [{role:"system",...},{role:"user",...}]`, `max_tokens`), parses the first choice's message content, returns it
- [ ] `ValidateCredentials` performs the `GET /v1/models` call and returns `nil` on 2xx, the classified error otherwise
- [ ] Test coverage matches `resend_test.go`'s depth: happy path, 401/403 → `ErrUnauthorized`, 5xx → `ErrServer`, context deadline → `ErrTimeout`, malformed JSON response → a wrapped error (not a panic)
- [ ] Gate passes: `go test ./internal/connectors/openai/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(openai): implement Complete and ValidateCredentials`

---

### T9: `openai` factory wiring

**What**: Add the `llm.ProviderFactory`-shaped function (`func(provider, apiKey, model string) (llm.Provider, error)`) that switches on `provider` and constructs `openai.NewClient` for `"openai"`, erroring for anything else - the single switch statement design.md's P2 story requires stay a one-case-per-provider addition.
**Where**: `internal/cli/routes.go` (new small function, alongside the existing `email.ProviderFactory` wiring for `sendgrid`/`resend`)
**Depends on**: T8
**Reuses**: The existing email-provider factory switch in `internal/cli/routes.go` (exact same shape, one `case` per known provider)
**Requirement**: AI-25, AI-26

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Factory function returns a working `*openai.Client` for `"openai"`, a clear error for any other provider name
- [ ] `go build ./...` succeeds with the new function wired (not yet called by anything until T16 wires the handler/service - acceptable per design's phase ordering, confirmed compiling standalone)

**Tests**: none (thin wiring function, matrix: entity/config layer - covered indirectly once T16's handler tests exercise it)
**Gate**: quick

**Commit**: `feat(cli): wire openai provider factory`

---

### T10: `LLMProviderRepository`

**What**: Implement `internal/db.LLMProviderRepository` with `UpsertProvider`, `Get`, `ListPaginated`, `GetActiveProvider`, `SetActiveProvider`, `UpdateModel`, `MarkInvalid`, `MarkChecked` - mirroring `EmailProviderRepository`'s method set, adding `UpdateModel` (email providers have no model concept to update).
**Where**: `internal/db/llm_provider_repository.go`
**Depends on**: T1
**Reuses**: `internal/db/email_provider_repository.go` almost verbatim, with `model` column added

**Tools**:
- MCP: NONE
- Skill: NONE
**Requirement**: AI-01, AI-03, AI-04, AI-06

**Done when**:
- [ ] Every method matches `EmailProviderRepository`'s error-wrapping/`ErrNotFound` conventions
- [ ] `UpdateModel` updates only `model`, leaves everything else (including `status`) untouched
- [ ] Integration tests (disposable Postgres): upsert-then-get round-trips correctly including `model`; `GetActiveProvider` on an empty `llm_settings` row returns `""`/no error; `SetActiveProvider` then `GetActiveProvider` round-trips; `UpdateModel` changes only the model column
- [ ] Gate passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add LLMProviderRepository`

---

### T11: `IncidentRepository` AI-fields methods

**What**: Add `SetDescription`, `HasOpenIncidentForService`, `SetPendingCloseComment`, `ConfirmPendingClose`, `DiscardCloseProposal` to `IncidentRepository`, and extend `Create`/scanning to populate the three new fields (`description`, `pending_close_comment`, `auto_created`) everywhere an `Incident` is read.
**Where**: `internal/db/incident_repository.go` (extend)
**Depends on**: T2
**Reuses**: Existing `IncidentRepository.Create`/`Transition`/`mustExist` transaction and error-wrapping conventions
**Requirement**: AI-09, AI-10, AI-11, AI-12, AI-19, AI-20, AI-21, AI-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `SetDescription(ctx, incidentID, description string) error` updates the column, returns `ErrNotFound` if missing
- [ ] `HasOpenIncidentForService(ctx, serviceID string) (incidentID string, found bool, err error)` queries `incidents` joined to `incident_services` for `status <> 'resolved'`
- [ ] `SetPendingCloseComment(ctx, incidentID, comment string) error`
- [ ] `ConfirmPendingClose(ctx, incidentID string) (*Incident, error)` - single transaction: reads `pending_close_comment` (0 rows / NULL → a distinct `ErrNoPendingProposal`), appends it as an `incident_update`, transitions to `resolved`, clears the column; all-or-nothing
- [ ] `DiscardCloseProposal(ctx, incidentID string) error` - clears the column only, `ErrNotFound` if missing, `ErrNoPendingProposal` if already NULL
- [ ] `Create` accepts an optional `Description string`/`AutoCreated bool` on the `Incident` passed in, persists both
- [ ] Every existing read path (`ListPaginated`, `ListPublic`, `ListPublicForStatusPage`, `scanIncidentRows`, `scanIncidentRowsWithTotal`) is updated to also scan `description`, `auto_created` (never `pending_close_comment` into the public-facing `IncidentPublic` shape - that stays admin-only, read via a separate accessor if needed by T17's handler)
- [ ] Integration tests: `SetDescription` round-trip; `HasOpenIncidentForService` true/false cases; `ConfirmPendingClose` happy path (incident resolved, update appended, column cleared) and no-pending-proposal case; `DiscardCloseProposal` happy path and no-pending case; a repeat `Create` call for a service with an existing open incident is NOT itself deduped by the repository (that's `SLOAnalyzer`'s job via `HasOpenIncidentForService` - confirm the repository layer stays a dumb persistence layer, not business logic)
- [ ] Gate passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add AI-incident fields and confirm/discard-close methods to IncidentRepository`

---

### T12: `ServiceRepository.UpdateStatusAnalysis`

**What**: Add `UpdateStatusAnalysis(ctx, serviceID string, analysis *string) error` to `ServiceRepository` - a nullable-set update (passing `nil` clears the column).
**Where**: `internal/db/service_repository.go` (extend)
**Depends on**: T3
**Reuses**: `ServiceRepository.UpdateStatus`'s existing single-column-update shape
**Requirement**: AI-14, AI-15, AI-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `UpdateStatusAnalysis(ctx, serviceID, nil)` sets the column to `NULL`
- [ ] `UpdateStatusAnalysis(ctx, serviceID, &text)` sets the column to `text`
- [ ] Integration tests cover both directions plus a not-found service ID
- [ ] Gate passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add ServiceRepository.UpdateStatusAnalysis`

---

### T13: `SLOAnalyzer` core - synchronous half

**What**: Implement `internal/poller/analyzer.go`'s `SLOAnalyzer` and `HandleTransition`'s fully synchronous behavior: outage-transition incident creation with the generic fallback description (skipped via `HasOpenIncidentForService` if one is already open), degraded-transition/away-from-degraded `status_analysis` clearing, and detecting when a recovery (`operational` with an open incident) should propose a closing comment. The async dispatch itself (T14) is a separate task so this task's tests can assert the synchronous DB state without needing to wait on or fake network calls.
**Where**: `internal/poller/analyzer.go`
**Depends on**: T7, T11, T12
**Reuses**: `Poller`'s existing narrow-interface convention (`serviceLister`, etc.) for `SLOAnalyzer`'s own dependencies
**Requirement**: AI-08, AI-09, AI-12, AI-14, AI-15, AI-17, AI-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `outage` transition with no existing open incident: creates an incident (generic description, `auto_created = true`) synchronously
- [ ] `outage` transition with an already-open incident for that service: creates nothing (AI-12)
- [ ] `degraded` transition (into): clears `status_analysis` to `NULL` synchronously (fresh state before any async write - AI-15/AI-16)
- [ ] Transition away from `degraded` (to anything): clears `status_analysis` to `NULL` synchronously (AI-17)
- [ ] `operational` transition with an open incident for that service: identified as needing a closing-comment proposal (method returns/signals this; actual LLM call is T14's job)
- [ ] Unit tests (fake repositories, no real LLM/network) cover all five bullets above plus the "no transition" no-op case (`previousStatus == newStatus` never calls anything)
- [ ] Gate passes: `go test ./internal/poller/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): add SLOAnalyzer synchronous transition handling`

---

### T14: `SLOAnalyzer` async enrichment dispatch

**What**: Add the detached-goroutine enrichment half: after T13's synchronous work, `HandleTransition` kicks off a `context.WithTimeout(context.WithoutCancel(ctx), analysisTimeout)`-bounded goroutine that calls the matching `llm.Service.Generate*` method and writes the result (`SetDescription`, `UpdateStatusAnalysis`, or `SetPendingCloseComment`) - logging and discarding on any error/timeout, per design's Error Handling Strategy table. `analysisTimeout` is a named constant (design default: document the chosen value and its rationale in the same doc-comment style as `poller.go`'s existing tuned constants).
**Where**: `internal/poller/analyzer.go` (extend)
**Depends on**: T13
**Reuses**: `AD-014`'s detached-dispatch pattern (locate and confirm the exact `context.WithoutCancel` call site in the password-reset handler before implementing, so the shape genuinely matches - not just described in design.md)
**Requirement**: AI-08, AI-10, AI-11, AI-13, AI-16, AI-18, AI-20, AI-23

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Successful `Generate*` call writes its result via the matching repository method after `HandleTransition` has already returned
- [ ] Failed/timed-out `Generate*` call writes nothing, logs the error, does not panic or leak the goroutine past `analysisTimeout`
- [ ] `HandleTransition` itself returns before the goroutine's result is known (asserted via a fake `Provider` that blocks on a channel the test controls)
- [ ] Unit tests (fake `llm.Service`/`Provider` with controllable delay/error/success): successful enrichment updates the row; failed enrichment leaves the fallback/NULL state; `HandleTransition`'s own call returns well before the fake unblocks
- [ ] Gate passes: `go test ./internal/poller/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): add SLOAnalyzer async LLM enrichment dispatch`

---

### T15: Wire `SLOAnalyzer` into `pollService` + non-blocking timing test

**What**: Call `p.analyzer.HandleTransition(...)` from `pollService` at the point design.md specifies (after computing `current`, before `OpenOrExtend`/`UpdateStatus`), threading `svc.CurrentStatus` as `previousStatus` and the fetched `datadog.SLOStatus` as the `AnalysisInput` source. Add the Risks-section-mandated timing-bounded integration test: force one service's LLM call to hang well past `analysisTimeout`, assert `pollOnce` still completes processing the *other* services in the same cycle within a fixed short wall-clock bound.
**Where**: `internal/poller/poller.go` (extend `pollService`, extend `NewPoller`'s constructor to accept the analyzer), `internal/poller/poller_test.go` (extend)
**Depends on**: T14
**Reuses**: `poller_test.go`'s existing fake-writer/fake-provider test scaffolding
**Requirement**: AI-08 through AI-24 (integration point for the whole feature)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `pollService` calls `HandleTransition` exactly once per service per cycle, only when a transition actually occurred (never on an unchanged status - AI-06/AI-18)
- [ ] A service whose status doesn't change makes zero calls into `SLOAnalyzer` (existing behavior for every other service in `poller_test.go`'s fixtures stays green - regression guard)
- [ ] New timing test: with `N` services in a cycle, one configured with a fake LLM provider that blocks indefinitely, `pollOnce` completes within a bound well under the fake's block duration (proves the hang doesn't propagate into the synchronous loop) - this is the Risks-section-flagged test from design.md, not optional
- [ ] Gate passes: `go test ./internal/poller/...` (full existing + new suite green, no regression in poll-cycle behavior)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): wire SLOAnalyzer into pollService`

---

### T16: `LLMProvidersHandler`

**What**: Implement `internal/api/llm_providers_handler.go`'s `Connect`, `SetModel`, `Activate`, `List`, wired to `/api/integrations/llm/{provider}`, `/api/integrations/llm/{provider}/model`, `/api/integrations/llm/{provider}/activate`, `/api/integrations/llm` in `internal/cli/routes.go` with the same `writeRoles`/`anyRole` split as `/api/integrations/email/*`.
**Where**: `internal/api/llm_providers_handler.go`, `internal/cli/routes.go` (route registration)
**Depends on**: T5, T9
**Reuses**: `internal/api/email_providers_handler.go` almost verbatim (known-provider allowlist of one, error-body helpers, response shapes)
**Requirement**: AI-01, AI-02, AI-03, AI-04, AI-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Connect` returns 404 for an unknown provider, 422 for invalid input/failed validation, 201 + `{"status":"connected"}` on success, never echoes the API key
- [ ] `SetModel` returns 422 for a model outside the allowlist, 200 on success
- [ ] `Activate` returns 422 for an unconnected provider, 200 on success
- [ ] `List` returns the paginated envelope, empty list + null active when nothing connected
- [ ] `writeRoles`-gated endpoints reject a `viewer` with 403 (AI-06)
- [ ] Handler unit tests (httptest) mirror `email_providers_handler_test.go`'s depth for every route above
- [ ] Gate passes: `go test ./internal/api/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(api): add LLMProvidersHandler and routes`

---

### T17: `IncidentsHandler` confirm/discard endpoints

**What**: Add `ConfirmClose`/`DiscardCloseProposal` methods to `IncidentsHandler`, wired to `POST /api/incidents/{id}/confirm-close` and `POST /api/incidents/{id}/discard-close-proposal` (`writeRoles` - AI-24), plus exposing `pending_close_comment`/`description`/`auto_created` on the existing admin incident response DTO (never on the public one - that's T18).
**Where**: `internal/api/incidents_handler.go` (extend), `internal/cli/routes.go` (route registration)
**Depends on**: T11
**Reuses**: Existing `incidents_handler.go` role-gating/error-body conventions
**Requirement**: AI-19, AI-20, AI-21, AI-22, AI-24

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ConfirmClose`: 404 unknown incident, 422 no pending proposal, 200 + incident now `resolved` with the proposal appended as its final update on success
- [ ] `DiscardCloseProposal`: 404 unknown incident, 422 no pending proposal, 200 + incident unchanged otherwise on success
- [ ] `viewer` role gets 403 on both (AI-24)
- [ ] Admin incident response DTO includes `description`, `pending_close_comment`, `auto_created`
- [ ] Handler unit tests cover every bullet above
- [ ] Gate passes: `go test ./internal/api/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(api): add incident confirm-close and discard-close-proposal endpoints`

---

### T18: Public status DTO additions

**What**: Add `status_analysis` (nullable, `publicServiceResponse`) and `description` (nullable, `publicIncidentResponse`) to `internal/api/public_status_handler.go` and `public_status_preview_handler.go`'s shared `composeResponse`, populated from `db.Service.StatusAnalysis`/`db.Incident.Description` - `status_analysis` only ever non-null when `current_status == "degraded"` (matches AI-15/AI-16 at the DTO boundary, defense-in-depth even though `SLOAnalyzer` already clears it on any other transition).
**Where**: `internal/api/public_status_handler.go`, `internal/api/public_status_preview_handler.go`
**Depends on**: T11, T12
**Reuses**: `AD-008` parity convention (both endpoints share `composeResponse`) already established for the time-range-selector feature
**Requirement**: AI-16, AI-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `publicServiceResponse.StatusAnalysis *string json:"status_analysis,omitempty"` populated only for `degraded` services with a non-nil `db.Service.StatusAnalysis`
- [ ] `publicIncidentResponse.Description *string json:"description,omitempty"` populated whenever `db.Incident.Description` is non-nil
- [ ] Both production and preview endpoints expose identically (AD-008 parity)
- [ ] `pending_close_comment` is confirmed absent from both response shapes (admin-only, negative test)
- [ ] Handler unit tests cover: degraded service with analysis present, degraded service with analysis still pending (field omitted), non-degraded service (field always omitted even if a stale value existed), incident with/without description
- [ ] Gate passes: `go test ./internal/api/...`

**Tests**: unit
**Gate**: quick

**Commit**: `feat(api): expose status_analysis and incident description on public status endpoints`

---

### T19: Phase 6 build gate

**What**: No new code - run the full backend build gate across everything Phases 1-6 touched, confirming no cross-package regression before frontend work starts.
**Where**: N/A (verification task)
**Depends on**: T16, T17, T18
**Reuses**: N/A
**Requirement**: N/A (gate task)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l` (repo-wide) all clean
- [ ] `go test ./...` green
- [ ] Disposable-Postgres integration gate green (`AGENTS.md` §3 exact commands), container torn down after

**Tests**: none (gate-only task)
**Gate**: full

**Commit**: none (no code change - if the gate surfaces a fix, that fix gets its own atomic commit before this task is marked done)

---

### T20: Frontend types + `useLLMProviders`/settings API client

**What**: Add `LLMProvider`/`RangeKey`-sibling types to `web/src/lib/llmProviders.ts` (new file) and a `useLLMProviders` hook (`web/src/features/settings/hooks.ts` or the existing settings hooks file) for connect/set-model/activate/list, mirroring the existing email-providers frontend hook shape.
**Where**: `web/src/lib/llmProviders.ts`, `web/src/features/settings/hooks.ts` (extend or create, matching wherever the email-providers equivalent lives)
**Depends on**: T16
**Reuses**: The existing email-providers hook/types file as the direct template (locate it first - referenced only indirectly in this design, confirm exact path during implementation)

**Tools**:
- MCP: NONE
- Skill: NONE
**Requirement**: AI-01, AI-02, AI-03, AI-04

**Done when**:
- [ ] Types match the backend DTOs from T16 exactly (no `api_key` field ever modeled client-side)
- [ ] Hook exposes connect/set-model/activate/list, each a React Query mutation/query following the codebase's existing pattern (`queryKey` conventions per `AGENTS.md` §5)
- [ ] Unit tests (MSW-mocked) cover success and error-response handling for each operation
- [ ] Gate passes: `npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): add LLM provider types and settings hook`

---

### T21: Settings "IA" section component

**What**: Add a new Settings section (own page/tab per the existing Settings navigation pattern) with a connect form (API key, optional model dropdown from the T5 allowlist), activate button, and connected/active status display - structurally identical to the existing email-providers settings section.
**Where**: `web/src/features/settings/AISettings.tsx` (or matching existing naming convention - confirm exact directory during implementation), route/nav wiring in the Settings shell
**Depends on**: T20
**Reuses**: The existing email-providers settings section component (layout, form, status badges) as the direct template
**Requirement**: AI-01, AI-02, AI-03, AI-04, AI-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Connect form submits API key (+ optional model), shows a validation error on 422 without navigating away
- [ ] Model dropdown lets an already-connected provider switch models without re-entering the key
- [ ] `viewer`-role admin sees the section read-only (no connect/activate controls) - confirm against however the existing email-providers section already gates this client-side (role is still enforced server-side per T16; client-side hiding is UX only)
- [ ] User-facing strings go through `react-i18next` (`AGENTS.md` §5) - both pt-BR and English entries added
- [ ] Component tests (testing-library) cover: empty state, connect success, connect error, model change, activate
- [ ] Manually exercised in a running dev server (`AGENTS.md` UI-change requirement): connect a real or stubbed OpenAI key, confirm the status badge updates
- [ ] Gate passes: `npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): add AI provider settings section`

---

### T22: Public status page - degraded tooltip

**What**: Update `PublicStatusPage.tsx` to show `status_analysis` (when present) as a native tooltip (`title`/`tabIndex`, matching `hourlyTooltip`'s accessibility pattern) on a `degraded` service's status badge; show the plain label with no tooltip when `status_analysis` is absent.
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`, `web/src/lib/publicStatus.ts` (extend `PublicServiceResponse`-equivalent type with `status_analysis`)
**Depends on**: T18, T20 (type conventions established)
**Reuses**: `hourlyTooltip`'s native-tooltip accessibility pattern directly
**Requirement**: AI-16, AI-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `degraded` service with `status_analysis` present shows it as a hover/focus tooltip on the status badge
- [ ] `degraded` service with `status_analysis` absent shows the plain "Degradado" label, no tooltip attribute rendered, no error text
- [ ] Non-degraded services are unaffected (regression guard)
- [ ] Component tests cover both presence/absence cases
- [ ] Manually exercised in a running dev server against MSW-mocked data
- [ ] Gate passes: `npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): show status_analysis tooltip on degraded services`

---

### T23: Public/admin incident description rendering

**What**: Render `description` (when present) on both the public incidents list (`PublicStatusPage.tsx`'s incident section) and the admin incidents dashboard, plus the admin dashboard's pending-close-comment confirm/discard UI (banner with the proposed text, confirm/discard buttons calling T17's endpoints).
**Where**: `web/src/features/public-status/PublicStatusPage.tsx` (extend), `web/src/features/incidents/*` (extend - confirm exact existing file names during implementation)
**Depends on**: T17, T18, T20
**Reuses**: Existing incident list rendering, existing mutation-hook patterns (`useMutation` + `queryClient.invalidateQueries`) already used elsewhere in the admin dashboard
**Requirement**: AI-19, AI-20, AI-21, AI-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Public incident card shows `description` when present, falls back to today's title-only rendering when absent (manually-created incidents, or an auto-created one still on its generic text)
- [ ] Admin incidents dashboard shows an `auto_created` badge
- [ ] Admin incident detail shows a pending-close-comment banner when `pending_close_comment` is non-null, with working confirm/discard buttons wired to T17's endpoints, disappearing after either action
- [ ] `viewer` role sees the banner but no confirm/discard buttons (server still enforces 403 regardless)
- [ ] Component tests cover: description present/absent, pending-proposal banner present/absent, confirm success, discard success, confirm/discard error handling
- [ ] Manually exercised in a running dev server against MSW-mocked data
- [ ] Gate passes: `npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: frontend

**Commit**: `feat(web): render incident description and closing-comment proposal UI`

---

### T24: MSW handlers for all new endpoints

**What**: Add MSW mock handlers for `/api/integrations/llm/*`, `/api/incidents/{id}/confirm-close`, `/api/incidents/{id}/discard-close-proposal`, and extend the existing public-status/incidents mock fixtures with `status_analysis`/`description`/`pending_close_comment`/`auto_created`, mirroring the real backend response shapes exactly (`AGENTS.md` §5's MSW-shape-parity rule).
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: T16, T17, T18 (real response shapes to mirror)
**Reuses**: Existing MSW handler patterns for `/api/integrations/email/*` and incidents
**Requirement**: N/A (test infrastructure supporting T20-T23's tests)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Every new endpoint has a handler returning the exact real backend shape (including the `Page<T>` envelope where applicable)
- [ ] Existing public-status/incidents fixtures gain the new fields with realistic values for at least one degraded/one auto-created-with-pending-proposal fixture, so T22/T23's tests have real data to assert against
- [ ] `npm run test` (full suite, not just this feature's new tests) stays green - confirms no existing test's fixture shape broke
- [ ] Gate passes: `npx tsc -b --noEmit && npm run test`

**Tests**: none (test-infrastructure task; its correctness is proven by T20-T23's tests passing against it, not by tests of the mocks themselves)
**Gate**: frontend

**Commit**: `test(web): add MSW handlers and fixtures for LLM SLO analysis`

---

### T25: Documentation sync

**What**: Per `AGENTS.md` §6: add an `AD-NNN` entry to `.specs/STATE.md` for the `llm_providers`/`llm_settings` + adapter-interface + detached-goroutine-with-timeout patterns (per design.md's "Project-level decision" note), update `README.md`'s feature table (new AI-analysis row) and Configuration table (any new env var - confirm whether one is actually needed, e.g. a configurable `analysisTimeout` override, or whether it stays a hardcoded constant; document only what's real), update `CHANGELOG.md`'s `[Unreleased]` section, and move `spec.md`'s Requirement Traceability statuses from `Pending` to `Verified` (done at the end of Execute, alongside the Verifier's pass, per the skill's own convention - this task drafts the entries, the Verifier's pass confirms/finalizes AC-by-AC).
**Where**: `.specs/STATE.md`, `README.md`, `CHANGELOG.md`, `.specs/features/llm-slo-analysis/spec.md` (traceability table)
**Depends on**: T21, T22, T23, T24
**Reuses**: `AD-020`'s entry as a structural template (Decision/Reason/Trade-off/Scope shape)
**Requirement**: N/A (process requirement, not a spec AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] New `AD-NNN` entry in `.specs/STATE.md` `## Decisions` describing the `llm_providers` schema pattern, the adapter interface, and the detached-goroutine timeout pattern, with real commit hashes once available
- [ ] `README.md` feature table has a new row; Configuration table updated only if a real new env var exists
- [ ] `CHANGELOG.md` `[Unreleased]` has `### Added` entries for the feature
- [ ] `spec.md`'s Requirement Traceability table's `Status` column updated (`Pending` → `In Tasks`/`Implementing` as appropriate at this point in Execute - final `Verified` stamp is the Verifier's job, not this task's)

**Tests**: none (docs-only task)
**Gate**: quick (confirm nothing broke - `go build ./...` after any accidental code touch, though none is expected)

**Commit**: `docs: sync STATE.md, README.md, CHANGELOG.md for llm-slo-analysis`

---

## Phase Execution Map

Intra-phase chains (execution order) plus every cross-phase dependency arrow, so this diagram matches the Task Breakdown's `Depends on` fields exactly (see Diagram-Definition Cross-Check below).

Every arrow below is a real data/build dependency, matching a task's `Depends on` field one-to-one (see the Cross-Check table further down). Tasks with no dependency between them (e.g. T1/T2/T3) are still executed in listed order for a linear commit history, but that ordering is not itself a dependency and is deliberately not drawn as an arrow here.

```
Phase 1 (no inter-task edges): T1, T2, T3

Phase 2:
T4 -> T5
T4 -> T6
T5 -> T7
T6 -> T7

Phase 3:
T4 -> T8
T8 -> T9

Phase 4 (no inter-task edges): T10, T11, T12
T1 -> T10
T2 -> T11
T3 -> T12

Phase 5:
T7 -> T13
T11 -> T13
T12 -> T13
T13 -> T14
T14 -> T15

Phase 6:
T5 -> T16
T9 -> T16
T11 -> T17
T11 -> T18
T12 -> T18
T16 -> T19
T17 -> T19
T18 -> T19

Phase 7:
T16 -> T20
T20 -> T21
T18 -> T22
T20 -> T22
T17 -> T23
T18 -> T23
T20 -> T23
T16 -> T24
T17 -> T24
T18 -> T24

Phase 8:
T21 -> T25
T22 -> T25
T23 -> T25
T24 -> T25
```

Execution is strictly sequential within a phase. Phases 2-4 have no cross-dependency on each other (only on Phase 1's schema), but are still executed in listed order per the skill's "batches run sequentially" rule - a future optimization could parallelize Phases 2-4 as independent batches, not adopted here to keep the execution plan simple and match the skill's default sequential-batch model.

**Total: 25 tasks across 8 phases.** Packed into ~7-task batches: **Batch 1** = Phases 1-3 (T1-T9, 9 tasks - slightly over budget, kept together because Phase 3 depends on Phase 2's T4 and splitting mid-way would strand a worker on an incomplete `internal/llm` package), **Batch 2** = Phases 4-5 (T10-T15, 6 tasks), **Batch 3** = Phase 6 (T16-T19, 4 tasks), **Batch 4** = Phases 7-8 (T20-T25, 6 tasks). 4 batches - sub-agent delegation offer applies (> ~8 tasks total).

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1-T3 | 1 migration pair + 1 test file each | ✅ Granular |
| T4 | 1 interface file | ✅ Granular |
| T5-T7 | 1 file extended per task, 1 cohesive method group each | ✅ Granular |
| T8 | 1 connector file, 2 cohesive methods | ✅ Granular |
| T9 | 1 small wiring function | ✅ Granular |
| T10-T12 | 1 repository file each | ✅ Granular |
| T13-T14 | 1 file, split into sync/async halves (genuine dependency seam - T14 needs T13's synchronous state to exist first) | ✅ Granular |
| T15 | 1 wiring change + 1 new test in existing file | ✅ Granular |
| T16-T18 | 1 handler/DTO concern each | ✅ Granular |
| T19 | Gate-only, no code | ✅ Granular (verification task) |
| T20-T24 | 1 frontend concern each (types+hook, settings component, tooltip, incident UI, mocks) | ✅ Granular |
| T25 | Docs-only, 4 files but one cohesive "sync docs" concern (established precedent: prior features' doc-sync task also touches STATE.md+README+CHANGELOG+spec.md together) | ✅ Granular |

No task spans multiple unrelated components. Every task producing testable code includes its own tests in the same task (no test-deferral).

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | None | None | ✅ Match |
| T3 | None | None | ✅ Match |
| T4 | None | None | ✅ Match |
| T5 | T4 | T4→T5 | ✅ Match |
| T6 | T4 | T4→T6 (via T5→T6 sequential order) | ✅ Match |
| T7 | T5, T6 | T6→T7 (T5 already sequenced before T6) | ✅ Match |
| T8 | T4 | T4→T8 (cross-phase, Phase 3 arrow) | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |
| T10 | T1 | T1→T10 (cross-phase) | ✅ Match |
| T11 | T2 | T2→T11 (cross-phase) | ✅ Match |
| T12 | T3 | T3→T12 (cross-phase) | ✅ Match |
| T13 | T7, T9 (factory pattern), T11, T12 | Phase 5 depends on Phases 2-4 per prose; T13 is Phase 5's first task | ✅ Match |
| T14 | T13 | T13→T14 | ✅ Match |
| T15 | T14 | T14→T15 | ✅ Match |
| T16 | T5, T9 | Phase 6 depends on Phase 2 (T5) and Phase 3 (T9) per prose | ✅ Match |
| T17 | T11 | Phase 6 depends on Phase 4 (T11) per prose | ✅ Match |
| T18 | T11, T12 | Phase 6 depends on Phase 4 (T11, T12) per prose | ✅ Match |
| T19 | T16, T17, T18 | Sequential within Phase 6 | ✅ Match |
| T20 | T16 | Phase 7 depends on Phase 6 (T16) per prose | ✅ Match |
| T21 | T20 | T20→T21 | ✅ Match |
| T22 | T18, T20 | T18/T20→T22 | ✅ Match |
| T23 | T17, T18, T20 | T17/T18/T20→T23 | ✅ Match |
| T24 | T16, T17, T18 | Phase 7 depends on Phase 6 per prose | ✅ Match |
| T25 | T1-T24 | Phase 8 depends on everything per prose | ✅ Match |

No task depends on a later-phase task. All dependencies point backward or within-phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Migration | integration | integration | ✅ OK |
| T2 | Migration | integration | integration | ✅ OK |
| T3 | Migration | integration | integration | ✅ OK |
| T4 | Domain (interface only) | none (entity/interface layer) | none | ✅ OK |
| T5 | Domain (`internal/llm`) | unit | unit | ✅ OK |
| T6 | Domain (`internal/llm`) | unit | unit | ✅ OK |
| T7 | Domain (`internal/llm`) | unit | unit | ✅ OK |
| T8 | Connector | unit | unit | ✅ OK |
| T9 | Config/wiring | none | none | ✅ OK |
| T10 | Repository | integration | integration | ✅ OK |
| T11 | Repository | integration | integration | ✅ OK |
| T12 | Repository | integration | integration | ✅ OK |
| T13 | Domain (`internal/poller`) | unit | unit | ✅ OK |
| T14 | Domain (`internal/poller`) | unit | unit | ✅ OK |
| T15 | Domain (`internal/poller`, poller wiring) | unit | unit | ✅ OK |
| T16 | API handler | unit | unit | ✅ OK |
| T17 | API handler | unit | unit | ✅ OK |
| T18 | API handler | unit | unit | ✅ OK |
| T19 | Gate-only | none (verification task, not a code layer) | none | ✅ OK |
| T20 | Frontend hooks/lib | unit | unit | ✅ OK |
| T21 | Frontend component | unit | unit | ✅ OK |
| T22 | Frontend component | unit | unit | ✅ OK |
| T23 | Frontend component | unit | unit | ✅ OK |
| T24 | Test infrastructure (mocks) | none (infra, proven by consuming tasks' tests) | none | ✅ OK |
| T25 | Docs | none | none | ✅ OK |

No `Tests: none` appears where the matrix requires a real test type. No violation.

---

## Tips

(carried from design.md's own "Tips carried into Tasks" section - not repeated here, see `design.md`)
