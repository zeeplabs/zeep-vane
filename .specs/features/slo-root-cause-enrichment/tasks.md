# SLO Root-Cause Enrichment Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/slo-root-cause-enrichment/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate: `go build`/`go test`/`go vet`/`gofmt`; frontend gate: `tsc -b --noEmit`/`npm run test`), `AGENTS.md`'s integration-test hard rule (disposable Postgres only, never `vane-dev-pg`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Datadog connector (`internal/connectors/datadog`) | unit | New methods: happy path, empty/not-found, `ErrUnauthorized`, `ErrTimeout`/`ErrServer` — same pattern as `FetchSLOStatus`'s existing test coverage | `internal/connectors/datadog/client_test.go` | `go test ./internal/connectors/datadog/...` |
| Repository (`internal/db` — `services`, `llm_settings`) | unit + integration | Key read/write paths for the new columns + error paths; migration applies/reverts cleanly against a disposable Postgres | `internal/db/service_repository_test.go`, `internal/db/llm_provider_repository_test.go` (unit); `//go:build integration` files in the same package | `go test ./internal/db/...` (unit); `make test-integration` (integration) |
| Domain logic (`internal/poller`, `internal/llm`) | unit | All branches 1:1 to RCA-01..06 (toggle off, no `datadog_service_tag`, `slo_type != metric`, call error, empty result, success) | `internal/poller/analyzer_test.go`, `internal/llm/prompts_test.go` | `go test ./internal/poller/... ./internal/llm/...` |
| API handlers (`internal/api`) | unit (handler-level, `httptest` — this repo's existing convention, not full e2e) | New/changed endpoints: happy path + every edge case (missing tag, unauthorized role, invalid body) | `internal/api/services_handler_test.go`, `internal/api/llm_providers_handler_test.go` | `go test ./internal/api/...` |
| Migration (`internal/db/migrations/0039_*.sql`) | none (build/integration gate only) | Verified indirectly: `make test-integration` applies every migration from zero | `internal/db/migrations/` | `make test-integration` |
| Frontend components (`web/src/features/services`, `web/src/features/integrations`) | unit (vitest + testing-library) | New/changed UI: render + interaction + one "renders in English" smoke test per this repo's i18n testing convention | `web/src/features/services/*.test.tsx`, `web/src/features/integrations/*.test.tsx` | `npm run test` (in `web/`) |
| Frontend types/config | none | build gate only | - | `npx tsc -b --noEmit` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `web/package.json` scripts.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend) | After a Go-only task with unit tests | `go build ./... && go test ./... && go vet ./... && gofmt -l <changed .go files>` |
| Quick (frontend) | After a frontend-only task with unit tests | `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check` |
| Full | After a task touching `internal/db` (migration or repository) | Quick (backend) + `make test-integration` |
| Build | After the last task in each phase | Quick (backend) + Quick (frontend) + `cd web && npm run build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Datadog connector foundation

T1 and T2 both edit `client.go` but touch unrelated methods - no data dependency between them, executed in listed order per this phase's own sequencing (not a `Depends on` edge).

```
T1   T2   (no dependency edge - independent methods, same file)
```

### Phase 2: Data layer

```
T3 → T4
T3 → T5
```

### Phase 3: API wiring

```
T4 → T6
T5 → T7
T7 → T8
```

### Phase 4: Core enrichment logic

```
T1 → T10
T2 → T10
T9 → T10
T10 → T11
T4 → T12
T5 → T12
T10 → T12
```

### Phase 5: Frontend

```
T6 → T13
T8 → T14
```

---

## Task Breakdown

### T1: Add `CauseHint` type + `SearchErrorTrackingIssues` method to `datadog.Client`

**What**: New `post` helper (sibling to `Client.get`, same header/timeout/error-classification, POST + JSON body) and `SearchErrorTrackingIssues(ctx, service, env string, from, to time.Time) (CauseHint, bool, error)`, hitting the verified `POST /api/v2/error-tracking/issues/search` shape (design.md's Components section). `CauseHint{ErrorType, ErrorMessage string}`, `ErrorMessage` truncated to 500 chars before returning.
**Where**: `internal/connectors/datadog/client.go`
**Depends on**: None
**Reuses**: `Client.get`'s header/timeout/error-classification pattern; `ErrUnauthorized`/`ErrTimeout`/`ErrServer`
**Requirement**: RCA-01, RCA-02, RCA-03

**Tools**:
- MCP: NONE (Datadog request/response shape already verified live this session, in design.md)
- Skill: NONE

**Done when**:
- [x] `post` helper added, mirrors `get`'s error classification
- [x] `SearchErrorTrackingIssues` builds the request body exactly as verified in design.md (`query: "service:<service> AND env:<env>"`, `track: "trace"`, `order_by: "TOTAL_COUNT"`)
- [x] Empty `data[]` returns `(CauseHint{}, false, nil)` — not an error, mirrors `SearchSLOs`'s empty-is-normal convention
- [x] `ErrorMessage` truncated to 500 chars
- [x] Gate check passes: `go test ./internal/connectors/datadog/...`
- [x] Test count: existing count + at least 4 new tests (happy path, empty result, unauthorized, timeout/server error)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(datadog): add Error Tracking Issues search for root-cause enrichment`

---

### T2: Extend `SLOSummary` with `SLOType`/`ServiceTag`, decode in `SearchSLOs`

**What**: Add `SLOType string` and `ServiceTag string` fields to `datadog.SLOSummary`; `SearchSLOs` decodes `slo_type` and `service_tags` from the same `/slo/search` response it already parses. `ServiceTag` is `""` unless `service_tags` has exactly one entry (per this session's decision: flow-type/multi-service SLOs get no `ServiceTag`, no attempt to parse the query string).
**Where**: `internal/connectors/datadog/client.go`
**Depends on**: None
**Reuses**: `sloSearchResponse`'s existing decode struct (extend it, same pattern as the existing `Name` field's `[Likely]` confidence-marker comment)
**Requirement**: RCA-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `SLOSummary.SLOType`/`ServiceTag` added
- [x] `sloSearchResponse`'s nested struct decodes `slo_type` and `service_tags`
- [x] `ServiceTag` is `""` when `service_tags` has 0 or 2+ entries; set only for exactly 1
- [x] Gate check passes: `go test ./internal/connectors/datadog/...`
- [x] Test count: existing count + at least 3 new tests (single service tag, zero tags, multiple tags)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(datadog): decode SLO type and single-service tag from slo/search`

---

### T3: Migration `0039` — `services.slo_type`/`datadog_service_tag`, `llm_settings.root_cause_enrichment_enabled`

**What**: New migration adding two nullable `TEXT` columns to `services` and one `NOT NULL DEFAULT false BOOLEAN` column to `llm_settings`, per design.md's Data Models section. No new `CHECK` constraint (both `services` columns are optional metadata, not required for any `monitor_mode`).
**Where**: `internal/db/migrations/0039_slo_root_cause_enrichment.up.sql`, `internal/db/migrations/0039_slo_root_cause_enrichment.down.sql`
**Depends on**: None
**Reuses**: `0034_service_polling_mode.up.sql`'s `ALTER TABLE services ADD COLUMN` style
**Requirement**: RCA-01, RCA-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `up.sql` adds all three columns exactly as specified in design.md
- [x] `down.sql` drops them cleanly (reverse order)
- [x] Gate check passes: `make test-integration` (proves the migration applies from zero against a disposable Postgres)

**Tests**: none (migration — integration gate only, per Test Coverage Matrix)
**Gate**: full

**Commit**: `feat(db): add slo_type/datadog_service_tag and root_cause_enrichment_enabled columns`

---

### T4: `db.Service` struct + `ServiceRepository` read/write for `slo_type`/`datadog_service_tag`

**What**: Add `SLOType string`/`DatadogServiceTag string` to `db.Service` (same `""`-means-absent convention as `SLOName`). `ServiceRepository`'s Create/Update/Get/List scan and persist the two new columns.
**Where**: `internal/db/service_repository.go`
**Depends on**: T3
**Reuses**: `SLOName`'s exact precedent (`internal/db/service_repository.go:35-38`) — same nullability handling, same doc-comment style explaining the `""`-not-`*string` choice
**Requirement**: RCA-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Service.SLOType`/`DatadogServiceTag` added with a doc comment mirroring `SLOName`'s
- [x] Create/Update/Get/List all read and write the two new columns correctly
- [x] Gate check passes: `go test ./internal/db/...` (unit) and `make test-integration` (integration)
- [x] Test count: existing count + at least 3 new tests (create with both set, update clearing them, read-back roundtrip)

**Tests**: unit + integration
**Gate**: full

**Commit**: `feat(db): persist slo_type/datadog_service_tag on services`

---

### T5: LLM settings repository — `RootCauseEnrichmentEnabled`/`SetRootCauseEnrichmentEnabled`

**What**: Two new methods on the LLM provider/settings repository (whichever already owns `llm_settings` reads/writes — same repository `active_provider` lives on): `RootCauseEnrichmentEnabled(ctx) (bool, error)`, `SetRootCauseEnrichmentEnabled(ctx, bool) error`.
**Where**: `internal/db/llm_provider_repository.go` (or the file that already implements `LLMProviderStore`'s `GetActiveProvider`/`SetActiveProvider` — same table, same tenant-scoping)
**Depends on**: T3
**Reuses**: `GetActiveProvider`/`SetActiveProvider`'s existing tenant-scoped query pattern on `llm_settings`
**Requirement**: RCA-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Both methods added, tenant-scoped via the same `app.tenant_id` mechanism every other repository method on this table uses
- [x] Default read (no explicit write yet) returns `false`, matching the column's `DEFAULT false`
- [x] Gate check passes: `go test ./internal/db/...` (unit) and `make test-integration` (integration)
- [x] Test count: existing count + at least 2 new tests (default false, set-then-read roundtrip)

**Tests**: unit + integration
**Gate**: full

**Commit**: `feat(db): add root-cause enrichment toggle read/write to llm settings repository`

---

### T6: `services_handler.go` — accept/return `slo_type`/`datadog_service_tag`

**What**: `CreateService`/`UpdateService` request structs accept `slo_type`/`datadog_service_tag` (both optional, sent by the frontend alongside `slo_name` — see T13), persisted via T4's repository methods. Response structs include them back, mirroring `slo_name`'s existing round-trip.
**Where**: `internal/api/services_handler.go`
**Depends on**: T4
**Reuses**: The exact existing `SLOName`/`SLOID` request/response field pattern (`internal/api/services_handler.go:80,106,172,293`)
**Requirement**: RCA-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Create/Update accept and persist the two new optional fields
- [ ] Get/List responses include them
- [ ] Gate check passes: `go test ./internal/api/...`
- [ ] Test count: existing count + at least 2 new tests (create with fields set, update clearing them)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(api): accept and return slo_type/datadog_service_tag on services endpoints`

---

### T7: `llm.Service.SetRootCauseEnrichmentEnabled`

**What**: Thin service-layer wrapper over T5's repository method, following `llm.Service`'s existing method shape (`Activate`/`SetModel`'s pattern — resolve/validate, delegate to repo, wrap errors).
**Where**: `internal/llm/service.go`
**Depends on**: T5
**Reuses**: `Service.Activate`'s error-wrapping convention
**Requirement**: RCA-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Method added, delegates to repository, wraps errors the same way every other `Service` method does
- [ ] Gate check passes: `go test ./internal/llm/...`
- [ ] Test count: existing count + at least 1 new test

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(llm): add SetRootCauseEnrichmentEnabled to LLM service`

---

### T8: `LLMProvidersHandler.UpdateSettings` (`PATCH /api/integrations/llm/settings`)

**What**: New handler method + route, `{"root_cause_enrichment_enabled": bool}` body, calls T7's service method, audit-logs the change (same pattern `Connect`/`Disconnect` already use in this handler).
**Where**: `internal/api/llm_providers_handler.go`, `internal/cli/routes.go`
**Depends on**: T7
**Reuses**: `writeRoles`/`anyRole` route-group convention already applied to every other `/api/integrations/llm/*` route; the audit-log call shape already in `Connect`/`Disconnect`
**Requirement**: RCA-07, RCA-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Handler added, wired at `PATCH /api/integrations/llm/settings` under `writeRoles`
- [ ] Audit log entry written on successful change
- [ ] Invalid body / wrong role return the correct error status, matching this handler's existing error-mapping convention
- [ ] Gate check passes: `go test ./internal/api/...`
- [ ] Test count: existing count + at least 3 new tests (happy path, invalid body, wrong role)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(api): add PATCH /api/integrations/llm/settings for root-cause enrichment toggle`

---

### T9: `AnalysisInput.CauseType`/`CauseMessage` + prompt changes + stale doc-comment fix

**What**: Add `CauseType`/`CauseMessage string` to `llm.AnalysisInput`. Extend `buildDegradedTooltipPrompt`/`buildOutageDescriptionPrompt` (not `buildClosingCommentPrompt` — spec scopes cause data to degraded/outage only) to append one extra sentence to the user prompt when both fields are non-empty; system prompt's jargon/tone constraints (`analysisSystemPrompt`) stay word-for-word unchanged. Fix `AnalysisInput`'s doc comment (currently states "no new external call is made to gather it") to reflect that the caller (`SLOAnalyzer`) may now make one, while `internal/llm` itself still makes none — the risk flagged in design.md.
**Where**: `internal/llm/prompts.go`
**Depends on**: None (pure prompt-construction change, no dependency on the DB/connector tasks)
**Reuses**: The existing `Translator`-free plain-string builder pattern already in this file (RCA-06's constraint is "extend the user prompt", not restructure the builders)
**Requirement**: RCA-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AnalysisInput.CauseType`/`CauseMessage` added
- [ ] `buildDegradedTooltipPrompt`/`buildOutageDescriptionPrompt` append the cause sentence only when both fields are non-empty; identical output to today when empty
- [ ] `buildClosingCommentPrompt` unchanged
- [ ] Doc comment at `internal/llm/prompts.go:5-7` corrected per design.md's Risks & Concerns row
- [ ] Gate check passes: `go test ./internal/llm/...`
- [ ] Test count: existing count + at least 4 new tests (degraded/outage with cause, degraded/outage without cause — 1:1 to RCA-06)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(llm): extend AnalysisInput and prompts with optional root-cause data`

---

### T10: `SLOAnalyzer` — `errorCauseProvider`/`enrichmentSettingsReader` interfaces + `SetErrorCauseEnrichment` + `resolveCauseHint`

**What**: New narrow interfaces `errorCauseProvider` (`FindTopErrorCause`) and `enrichmentSettingsReader` (`RootCauseEnrichmentEnabled`) in `internal/poller/analyzer.go`. New optional setter `SetErrorCauseEnrichment(settings, provider)`, same shape as `SetNotifier`. New unexported `resolveCauseHint(ctx, svc) (causeType, causeMessage string)` — the single funnel implementing every early-out from RCA-03/RCA-04 (settings unset → `("","")`; toggle off → `("","")`; `svc.SLOType != "metric"` or `svc.DatadogServiceTag == ""` → `("","")`; provider error/not-found → `("","")`; success → populated).
**Where**: `internal/poller/analyzer.go`
**Depends on**: T1, T2 (interfaces reference `datadog.CauseHint`), T9 (`resolveCauseHint`'s return feeds `AnalysisInput`)
**Reuses**: `SetNotifier`'s exact optional-setter shape (`analyzer.go:145-147`); `incidentNotifier`'s narrow-interface style
**Requirement**: RCA-01, RCA-02, RCA-03, RCA-04, RCA-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Both interfaces added, matching T1/T2's real method signatures exactly (no adapter needed — `*datadog.Client` satisfies `errorCauseProvider` directly)
- [ ] `SetErrorCauseEnrichment` added, nil by default (unset = feature fully off, zero behavior change for any caller/test that doesn't call it)
- [ ] `resolveCauseHint` implements every early-out from RCA-01..04, unit-testable in isolation
- [ ] Gate check passes: `go test ./internal/poller/...`
- [ ] Test count: existing count + at least 6 new tests (1:1 to RCA-01 through RCA-04's branches: unset, toggle off, wrong slo_type, no service tag, provider error, success)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(poller): add error-cause-enrichment interfaces and resolution to SLOAnalyzer`

---

### T11: Wire `resolveCauseHint` into `dispatchDegradedEnrichment`/`dispatchOutageEnrichment`

**What**: Both dispatch methods call `a.resolveCauseHint(dctx, svc)` before `buildAnalysisInput`, using the *same* bounded `dctx` already created in each goroutine (design.md: no second timeout context). Populate `AnalysisInput.CauseType`/`CauseMessage` from the result. `dispatchClosingCommentEnrichment` untouched.
**Where**: `internal/poller/analyzer.go`
**Depends on**: T10
**Reuses**: The existing `dctx, cancel := context.WithTimeout(...)` already in both methods
**Requirement**: RCA-01, RCA-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Both dispatch methods call `resolveCauseHint` before building `AnalysisInput`, using the existing `dctx`
- [ ] No new `context.WithTimeout` call added — same bound governs both the cause lookup and the LLM call
- [ ] `dispatchClosingCommentEnrichment` has zero changes
- [ ] Gate check passes: `go test ./internal/poller/...`
- [ ] Test count: existing count + at least 3 new tests (degraded transition with cause populated, outage transition with cause populated, cooldown/concurrency bounds still apply — regression check against `maxConcurrentEnrichments`/`enrichmentCooldown`)

**Tests**: unit
**Gate**: quick (backend)

**Commit**: `feat(poller): plumb root-cause data into degraded/outage enrichment dispatch`

---

### T12: Boot wiring — `internal/cli/serve.go`

**What**: Construct the concrete `*datadog.Client` as `errorCauseProvider` and the LLM settings repository as `enrichmentSettingsReader`; call `SLOAnalyzer.SetErrorCauseEnrichment` unconditionally at boot (the runtime toggle, not the wiring, gates behavior — same reasoning `SetNotifier` already uses).
**Where**: `internal/cli/serve.go`
**Depends on**: T4, T5, T10
**Reuses**: The existing `SetNotifier` boot-wiring call site as the pattern to follow
**Requirement**: RCA-01, RCA-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `SetErrorCauseEnrichment` called at boot, right alongside the existing `SetNotifier` call
- [ ] Gate check passes: `go build ./... && go test ./...` (full backend suite, confirms nothing broke at the wiring seam) and `make test-integration`
- [ ] No new test count requirement (pure wiring — covered by the full backend suite + integration gate, not a new unit test)

**Tests**: none (wiring — covered by the full existing suite + integration gate)
**Gate**: full

**Commit**: `feat(cli): wire root-cause enrichment into SLOAnalyzer at boot`

---

### T13: `AddServiceDrawer.tsx` — capture and send `slo_type`/`datadog_service_tag`

**What**: The existing SLO-search selection flow (already resolves `slo_name` for the create/update payload) also captures `slo_type`/`datadog_service_tag` from the same search result and includes them in the Create/Update request body.
**Where**: `web/src/features/services/AddServiceDrawer.tsx`, `web/src/features/services/hooks.ts` (or wherever the create/update mutation payload is built)
**Depends on**: T6
**Reuses**: The exact existing `slo_name` capture/send flow — same selection handler, two more fields riding along
**Requirement**: RCA-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Selecting an SLO in the drawer captures `slo_type`/`datadog_service_tag` from the search result
- [ ] Create/Update payload includes both fields
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Test count: existing count + at least 2 new tests (fields present on save, fields absent/empty for a flow-type SLO with no single service tag)

**Tests**: unit
**Gate**: quick (frontend)

**Commit**: `feat(services): capture slo_type/datadog_service_tag when linking an SLO`

---

### T14: `IntegrationsPage.tsx` — root-cause enrichment toggle

**What**: New toggle control in the AI/LLM category section of `IntegrationsPage`, reflecting `llm_settings.root_cause_enrichment_enabled`, calling T8's `PATCH` endpoint on change. Hidden entirely (design.md's resolved Risk) when no active LLM provider is connected. New `integrations.llm.rootCauseEnrichment.*` i18n keys (pt-BR + en, per this session's own `AGENTS.md` English-only-code rule — the toggle's user-facing label still goes through `t()`, only test/comment text is English).
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`, `web/src/locales/pt-BR.json`, `web/src/locales/en.json`
**Depends on**: T8
**Reuses**: The existing `activateMutation`/toggle pattern already in this file for other LLM provider controls (`internal/api/integrations_handler.go`'s frontend counterpart)
**Requirement**: RCA-07, RCA-08, RCA-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Toggle renders reflecting current setting, hidden when no active LLM provider
- [ ] Switching it calls the `PATCH` endpoint and reflects the new state
- [ ] `pt-BR.json`/`en.json` parity maintained (`npm run i18n:check` clean)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check`
- [ ] Test count: existing count + at least 3 new tests (renders when provider active, hidden when no provider, toggle-then-persist + one "renders in English" smoke test per this repo's convention)

**Tests**: unit
**Gate**: quick (frontend)

**Commit**: `feat(integrations): add root-cause enrichment toggle to LLM settings UI`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1:  T1    T2          (independent, executed in listed order)
Phase 2:  T3 → T4
          T3 → T5
Phase 3:  T4 → T6
          T5 → T7 → T8
Phase 4:  T1 → T10
          T2 → T10
          T9 → T10 → T11
          T4 → T12
          T5 → T12
          T10 → T12
Phase 5:  T6 → T13
          T8 → T14
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

14 tasks total → packs into 2 batches at the ~7-task budget: **Batch 1 = Phases 1-3 (T1-T8, 8 tasks)**, **Batch 2 = Phases 4-5 (T9-T14, 6 tasks)**. Offer sub-agent dispatch per the skill's Sub-Agent Delegation rule before starting Execute.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `CauseHint` + `SearchErrorTrackingIssues` | 1 file (1 method + 1 helper) | ✅ Granular |
| T2: Extend `SLOSummary` decoding | 1 file (1 struct + decode logic) | ✅ Granular |
| T3: Migration `0039` | 2 files (up/down, same migration) | ✅ Granular |
| T4: `db.Service` + repository | 1 file | ✅ Granular |
| T5: LLM settings repository methods | 1 file (2 methods) | ✅ Granular |
| T6: `services_handler.go` fields | 1 file | ✅ Granular |
| T7: `llm.Service.SetRootCauseEnrichmentEnabled` | 1 file (1 method) | ✅ Granular |
| T8: `LLMProvidersHandler.UpdateSettings` + route | 2 files (handler + route registration) | ⚠️ OK - cohesive (a handler is never testable/committable without its route) |
| T9: `AnalysisInput` + prompts + doc-comment fix | 1 file | ✅ Granular |
| T10: `SLOAnalyzer` interfaces + setter + resolver | 1 file | ✅ Granular |
| T11: Wire resolver into dispatch methods | 1 file (2 methods modified) | ✅ Granular |
| T12: Boot wiring | 1 file | ✅ Granular |
| T13: `AddServiceDrawer` capture fields | 2 files (component + hooks, one cohesive change) | ⚠️ OK - cohesive (payload fields must change together) |
| T14: `IntegrationsPage` toggle + i18n | 3 files (component + 2 locale JSONs) | ⚠️ OK - cohesive (this repo's own i18n convention requires both locale files change together, never one alone) |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming arrow | ✅ Match |
| T2 | None | No incoming arrow (independent of T1, same file, listed-order only) | ✅ Match |
| T3 | None | No incoming arrow | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T3 | T3 → T5 | ✅ Match |
| T6 | T4 | T4 → T6 | ✅ Match |
| T7 | T5 | T5 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | None | No incoming arrow | ✅ Match |
| T10 | T1, T2, T9 | T1 → T10, T2 → T10, T9 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T4, T5, T10 | T4 → T12, T5 → T12, T10 → T12 | ✅ Match |
| T13 | T6 | T6 → T13 | ✅ Match |
| T14 | T8 | T8 → T14 | ✅ Match |

No forward-phase dependency anywhere — every `Depends on` points to a task in the same or an earlier phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Datadog connector | unit | unit | ✅ OK |
| T2 | Datadog connector | unit | unit | ✅ OK |
| T3 | Migration | none | none | ✅ OK |
| T4 | Repository | unit + integration | unit + integration | ✅ OK |
| T5 | Repository | unit + integration | unit + integration | ✅ OK |
| T6 | API handler | unit | unit | ✅ OK |
| T7 | Domain logic (`internal/llm`) | unit | unit | ✅ OK |
| T8 | API handler | unit | unit | ✅ OK |
| T9 | Domain logic (`internal/llm`) | unit | unit | ✅ OK |
| T10 | Domain logic (`internal/poller`) | unit | unit | ✅ OK |
| T11 | Domain logic (`internal/poller`) | unit | unit | ✅ OK |
| T12 | Wiring (`internal/cli`) | none (per matrix: entity/config/wiring — build gate only) | none | ✅ OK |
| T13 | Frontend component | unit | unit | ✅ OK |
| T14 | Frontend component | unit | unit | ✅ OK |

No violations. No task defers its tests to a later task.

---

## Tools for Execution

This project has no configured MCP servers or skills specific to Go/TS implementation beyond what's already listed per-task (all `NONE` — every task's data/API shape was already verified live during Design, so no further live lookups are needed during Execute).
