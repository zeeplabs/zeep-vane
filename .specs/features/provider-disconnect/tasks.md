# Provider Disconnect Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: skipped - this feature mirrors the existing Connect/Activate architecture exactly (same repository/service/handler layering, same `writeRoles` authorization, same audit pattern). No new architectural decision is introduced; see `spec.md`'s Assumptions & Open Questions for the design-equivalent decisions (route shape, idempotency, FK-driven active-provider clear).
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (AGENTS.md §3 + sampled `internal/db/email_provider_repository_test.go`, `internal/api/email_providers_handler_test.go`, `internal/api/email_providers_audit_integration_test.go`, `web/src/features/email-providers/hooks.test.ts`, `web/src/features/email-providers/EmailProvidersPage.test.tsx`, `web/src/features/settings/llmProviderHooks.test.ts`). Guidelines found: `AGENTS.md` §3 (build gate: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l`; frontend: `npx tsc -b --noEmit`, `npm run test`) and §3's integration-test rule (disposable Postgres only, `-p 1`, never `vane-dev-pg`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Repository (`db.EmailProviderRepository`, `db.LLMProviderRepository`) | unit (against a real disposable Postgres - existing repo test convention, not mocked) | Delete removes the row; deleting a non-existent provider is a no-op (no error); FK clears `active_provider` when the deleted row was active | `internal/db/email_provider_repository_test.go`, `internal/db/llm_provider_repository_test.go` | `go test ./internal/db/...` (against whatever connection the existing repo test file already uses) |
| Service (`email.Service`, `llm.Service`) | unit | All branches: happy path, never-connected/idempotent, active-provider-cleared vs. unchanged | `internal/email/service_test.go`, `internal/llm/service_test.go` | `go test ./internal/email/... ./internal/llm/...` |
| Handler (`EmailProvidersHandler.Disconnect`, `LLMProvidersHandler.Disconnect`) | unit (httptest, no real DB - existing handler test pattern) | All routes in scope: happy (204), unknown provider (404), never-connected (204 idempotent), viewer forbidden (403) | `internal/api/email_providers_handler_test.go`, `internal/api/llm_providers_handler_test.go` | `go test ./internal/api/...` |
| Audit integration (`email_provider_disconnected`, `llm_provider_disconnected`) | integration | One audit row per disconnect, correct action name and actor | `internal/api/email_providers_audit_integration_test.go`, `internal/api/llm_providers_audit_integration_test.go` | `make test-integration` |
| Router wiring (`internal/cli/routes.go`) | none | Covered transitively by the handler/integration tests above hitting the real route | - | build gate only |
| Frontend hook (email + LLM disconnect mutations) | unit | Mutation calls correct method/URL, invalidates the right query key on success | `web/src/features/email-providers/hooks.test.ts`, `web/src/features/settings/llmProviderHooks.test.ts` | `npm run test` |
| Frontend component (Disconnect button + confirmation dialog) | unit | Dialog opens on click, cancel makes no API call, confirm calls mutation, pending disables button, error path shows message | `web/src/features/email-providers/EmailProvidersPage.test.tsx`, `web/src/features/settings/AISettings.test.tsx` | `npm run test` |

**Note on repository tests:** the existing sample (`email_provider_repository_test.go`) runs against a real Postgres connection, not mocks - per AGENTS.md §3 this must be a disposable container, never `vane-dev-pg`. Follow whatever connection convention the existing repo test file already uses (read it before writing the new test).

**Note on the LLM hook file:** `web/src/features/settings/llmProviderHooks.test.ts` imports its hooks from `./hooks` - the LLM provider hooks live in `web/src/features/settings/hooks.ts` alongside `useDeleteTenant`, not in a same-named `llmProviderHooks.ts` file. Confirmed by reading the test file's import.

## Gate Check Commands

> Generated from codebase (`Makefile`, `web/package.json`) - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only service/handler unit tests | `go build ./... && go test ./... && go vet ./...` |
| Quick (frontend) | After a task touching only hooks/components | `npx tsc -b --noEmit && npm run test` (run from `web/`) |
| Full | After a task touching repository or audit integration tests | Quick, plus `make test-integration` |
| Build | End of each phase | `go build ./... && go test ./... && go vet ./... && gofmt -l <changed .go files>`, plus frontend Quick, plus `make test-integration` if the phase touched repository/integration code |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend - Repository Layer

T1, T2 (independent - no dependency between them; grouped by layer, not by chain).

### Phase 2: Backend - Service Layer

T3, T4.

### Phase 3: Backend - Handler + Routing + Audit + Build Gate

T5 → T6, T7 → T8, then T9.

### Phase 4: Frontend - Email Provider Disconnect

T10 → T11.

### Phase 5: Frontend - LLM Provider Disconnect

T12 → T13.

---

## Task Breakdown

### T1: Add `DeleteProvider` to `EmailProviderRepository`

**What**: Add `DeleteProvider(ctx context.Context, provider string) error` to `internal/db/email_provider_repository.go` - `DELETE FROM email_providers WHERE provider = $1`; no error when zero rows affected (idempotent, per spec Assumptions).
**Where**: `internal/db/email_provider_repository.go`
**Depends on**: None
**Reuses**: Existing method style in the same file (`UpsertProvider`, `Get`) - same error-wrapping convention (`fmt.Errorf("db: failed to ...: %w", err)`).
**Requirement**: PROVDISC-01, PROVDISC-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `DeleteProvider` deletes the matching row and returns `nil` on success, including when zero rows matched.
- [x] Test added in `internal/db/email_provider_repository_test.go`: delete an existing row (row gone after), delete a non-existent provider (no error), delete the active provider (verify `email_settings.active_provider` becomes NULL via the FK, no manual clear needed).
- [x] Gate check passes: quick

**Tests**: unit (real Postgres, per existing sample)
**Gate**: quick

---

### T2: Add `DeleteProvider` to `LLMProviderRepository`

**What**: Add `DeleteProvider(ctx context.Context, provider string) error` to `internal/db/llm_provider_repository.go`, mirroring T1's behavior for `llm_providers`/`llm_settings`.
**Where**: `internal/db/llm_provider_repository.go`
**Depends on**: None
**Reuses**: T1's implementation pattern (read T1's diff before starting, since it lands first in Phase 1).
**Requirement**: PROVDISC-04, PROVDISC-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `DeleteProvider` mirrors T1's behavior for `llm_providers`.
- [x] Test added in `internal/db/llm_provider_repository_test.go` mirroring T1's three cases.
- [x] Gate check passes: quick

**Tests**: unit (real Postgres)
**Gate**: quick

---

### T3: Add `Disconnect` to `email.Service`

**What**: Add `Disconnect(ctx context.Context, provider string) error` to `internal/email/service.go` - calls `s.repo.DeleteProvider`. No new sentinel error needed (delete is idempotent by design - never-connected returns success, matching PROVDISC-02).
**Where**: `internal/email/service.go`
**Depends on**: T1
**Reuses**: `Activate`'s method shape/logging conventions in the same file.
**Requirement**: PROVDISC-01, PROVDISC-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `EmailProviderStore` interface (in `service.go`) gains `DeleteProvider(ctx context.Context, provider string) error`.
- [x] `Service.Disconnect` calls it and returns any error wrapped, no special-casing for "not found" (idempotent per spec).
- [x] Test added in `internal/email/service_test.go`: happy path, provider was active (no error, delegates to repo), never-connected (no error).
- [x] Gate check passes: quick

**Tests**: unit - 1:1 to PROVDISC-01/02 branches
**Gate**: quick

---

### T4: Add `Disconnect` to `llm.Service`

**What**: Add `Disconnect(ctx context.Context, provider string) error` to `internal/llm/service.go`, mirroring T3 for LLM providers.
**Where**: `internal/llm/service.go`
**Depends on**: T2
**Reuses**: T3's implementation pattern.
**Requirement**: PROVDISC-04, PROVDISC-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `llm.Service.Disconnect` mirrors T3.
- [x] Test added in `internal/llm/service_test.go` mirroring T3's cases.
- [x] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T5: Add `Disconnect` handler method for email providers

**What**: Add `EmailProvidersHandler.Disconnect(w, r)` handling the email-provider disconnect request - unknown provider name maps to the existing `writeUnknownEmailProvider` (404); success responds `204 No Content`; the row's id/display name is fetched via `h.rows.Get` **before** calling `svc.Disconnect` (the row won't resolve afterward), so a successful delete of an existing row can still emit the `email_provider_disconnected` audit entry - same "capture before mutate" reasoning `ServicesHandler.Delete` uses.
**Where**: `internal/api/email_providers_handler.go`
**Depends on**: T3
**Reuses**: `ServicesHandler.Delete`'s "capture name before delete, audit after success" pattern; `EmailProvidersHandler.Activate`'s error-mapping/audit style in the same file.
**Requirement**: PROVDISC-01, PROVDISC-02, PROVDISC-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `emailProviderService` interface gains `Disconnect(ctx context.Context, provider string) error`.
- [x] `Disconnect` handler: 404 for unknown provider name; on success, 204 No Content, regardless of whether the row existed beforehand (idempotent).
- [x] Audit action `email_provider_disconnected` recorded only when a row actually existed before the delete (nothing to disconnect otherwise).
- [x] Test added in `internal/api/email_providers_handler_test.go`: 204 happy path (row captured before delete, the audit precondition; the actual `admin_audit_log` insertion is verified by T6's real-DB integration test, same split Connect/Activate already use), 404 unknown provider name, 204 never-connected (idempotent), 500 unexpected service error. Viewer 403 is covered generically by `internal/api/middleware_role_test.go`'s `RequireRole` tests (the handler itself is role-agnostic - same as Connect/Activate, neither of which has a per-handler viewer test).
- [x] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T6: Wire the email provider disconnect route

**What**: Register `protected.With(writeRoles).Delete("/api/integrations/email/{provider}", emailProvidersHandler.Disconnect)` in the routes block alongside the existing Connect/Activate email routes.
**Where**: `internal/cli/routes.go`
**Depends on**: T5
**Reuses**: The adjacent `Connect`/`Activate` route lines' exact `writeRoles` gating.
**Requirement**: PROVDISC-01, PROVDISC-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Route registered under `writeRoles`, same middleware chain as the Connect/Activate email routes.
- [ ] Test added/extended in `internal/api/email_providers_audit_integration_test.go`: disconnecting a connected provider via the real router produces exactly one `email_provider_disconnected` audit row.
- [ ] Gate check passes: full

**Tests**: integration (audit, exercised through the real route)
**Gate**: full

---

### T7: Add `Disconnect` handler method for LLM providers

**What**: Mirror T5 for `LLMProvidersHandler.Disconnect` - same 404/204/audit-before-delete shape, audit action `llm_provider_disconnected`.
**Where**: `internal/api/llm_providers_handler.go`
**Depends on**: T4
**Reuses**: T5's implementation pattern.
**Requirement**: PROVDISC-04, PROVDISC-05, PROVDISC-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `llmProviderService` interface gains `Disconnect`.
- [ ] `Disconnect` handler mirrors T5's behavior for LLM providers.
- [ ] Test added in `internal/api/llm_providers_handler_test.go` mirroring T5's four cases.
- [ ] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T8: Wire the LLM provider disconnect route

**What**: Register `protected.With(writeRoles).Delete("/api/integrations/llm/{provider}", llmProvidersHandler.Disconnect)` alongside the existing LLM routes.
**Where**: `internal/cli/routes.go`
**Depends on**: T7
**Reuses**: T6's implementation pattern.
**Requirement**: PROVDISC-04, PROVDISC-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Route registered under `writeRoles`.
- [ ] Test added/extended in `internal/api/llm_providers_audit_integration_test.go` mirroring T6's audit test.
- [ ] Gate check passes: full

**Tests**: integration (audit)
**Gate**: full

---

### T9: Backend build/vet/format gate for the full backend surface

**What**: Run the full backend build gate across everything added in T1-T8 before moving to frontend work, catching any cross-file wiring issue (interface satisfaction, unused imports) a single task's quick gate wouldn't surface.
**Where**: N/A (verification task, no new files)
**Depends on**: T6, T8
**Reuses**: N/A

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `go build ./...` passes.
- [ ] `go test ./...` passes (full suite, not just the new packages).
- [ ] `go vet ./...` passes.
- [ ] `gofmt -l` reports no changed files needing formatting.
- [ ] `make test-integration` passes (disposable container per AGENTS.md §3 - never `vane-dev-pg`).

**Tests**: none (verification task; matrix marks router wiring/build-gate work as "none" - build gate only)
**Gate**: build

---

### T10: Add `useDisconnectEmailProvider` hook

**What**: Add a mutation hook in `web/src/features/email-providers/hooks.ts` calling `DELETE /api/integrations/email/${provider}`, invalidating `["integrations", "email"]` on success - mirrors `useActivateEmailProvider` exactly. Also add the MSW handler for this route (matches the real 204/no-body response, per AGENTS.md §5) inline as part of the same test setup.
**Where**: `web/src/features/email-providers/hooks.ts`
**Depends on**: T6
**Reuses**: `useActivateEmailProvider` in the same file.
**Requirement**: PROVDISC-07, PROVDISC-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useDisconnectEmailProvider()` returns a mutation whose `mutationFn` calls `apiFetch` with `method: "DELETE"` against the correct URL, no request body.
- [ ] `onSuccess` invalidates `["integrations", "email"]`.
- [ ] MSW handler added/updated for the DELETE route (204, no body).
- [ ] Test added in `web/src/features/email-providers/hooks.test.ts`: mutation calls correct method/URL, query invalidated on success.
- [ ] Gate check passes: quick (frontend)

**Tests**: unit
**Gate**: quick (frontend)

---

### T11: Add Disconnect button + confirmation dialog to `EmailProvidersPage`

**What**: Add a "Disconnect" button next to each connected provider row in `EmailProvidersPage.tsx`, opening the shared `Dialog` component for confirmation (mirrors `SettingsPage.tsx`'s `deleteDialogOpen` pattern) before calling `useDisconnectEmailProvider`. Disable the button while the mutation is pending. Add the dialog's title/body/confirm/cancel copy as new i18n keys under the existing `emailProviders` block in `i18n.ts` (pt-BR + en, per AGENTS.md §5 - no hardcoded strings); this is a same-PR string addition to the shared resources object, not a separate deliverable.
**Where**: `web/src/features/email-providers/EmailProvidersPage.tsx`
**Depends on**: T10
**Reuses**: `SettingsPage.tsx`'s delete-tenant confirmation dialog pattern (`Dialog` component, `open`/`onOpenChange`, confirm/cancel buttons); `handleActivate`'s existing button-disable-while-pending pattern in the same file.
**Requirement**: PROVDISC-07, PROVDISC-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Clicking "Disconnect" opens a confirmation dialog naming the provider; no API call yet.
- [ ] Cancel closes the dialog with no API call, provider still shown as connected.
- [ ] Confirm calls `useDisconnectEmailProvider`'s mutation; on success the provider disappears from the list (query invalidation refetches).
- [ ] Button is disabled while the mutation is pending (same pattern as `activateMutation.isPending`).
- [ ] On mutation failure, an error message is shown and the provider list is left unchanged.
- [ ] Test added/extended in `EmailProvidersPage.test.tsx`: open dialog, cancel (no call), confirm (call + row removed), pending disables button, error path shows message.
- [ ] Gate check passes: quick (frontend)

**Tests**: unit
**Gate**: quick (frontend)

---

### T12: Add `useDisconnectLlmProvider` hook

**What**: Mirror T10 for the LLM settings screen - add a mutation hook calling `DELETE /api/integrations/llm/${provider}` in `web/src/features/settings/hooks.ts` (the actual LLM provider hooks file - confirmed via `llmProviderHooks.test.ts`'s `./hooks` import), invalidating the LLM providers query key, alongside the existing `useDeleteTenant`/LLM hooks already in that file.
**Where**: `web/src/features/settings/hooks.ts`
**Depends on**: T8
**Reuses**: T10's implementation pattern; the existing `useActivateLLMProvider` hook in the same file.
**Requirement**: PROVDISC-07, PROVDISC-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Disconnect mutation hook mirrors T10 for the LLM endpoint.
- [ ] MSW handler added/updated for the LLM DELETE route.
- [ ] Test added in `web/src/features/settings/llmProviderHooks.test.ts` mirroring T10's coverage.
- [ ] Gate check passes: quick (frontend)

**Tests**: unit
**Gate**: quick (frontend)

---

### T13: Add Disconnect button + confirmation dialog to `AISettings`

**What**: Mirror T11 for `AISettings.tsx` (the LLM provider settings screen) - Disconnect button, confirmation dialog, pending-disable, error handling, plus the equivalent i18n keys under the existing LLM-related i18n block.
**Where**: `web/src/features/settings/AISettings.tsx`
**Depends on**: T12
**Reuses**: T11's implementation pattern.
**Requirement**: PROVDISC-07, PROVDISC-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Mirrors all of T11's `Done when` items for the LLM settings screen.
- [ ] Test added/extended in `AISettings.test.tsx` mirroring T11's coverage.
- [ ] Gate check passes: full (frontend quick, plus `make test-integration` to close out backend+frontend together for the whole feature)

**Tests**: unit
**Gate**: full

---

## Phase Execution Map

Dependency arrows (every `Depends on` in a task body has exactly one matching arrow below, and vice versa):

```
T1 → T3
T2 → T4
T3 → T5
T4 → T7
T5 → T6
T6 → T9
T6 → T10
T7 → T8
T8 → T9
T8 → T12
T10 → T11
T12 → T13
```

Phase membership (execution order within a phase is top-to-bottom as listed in Task Breakdown): Phase 1 = T1, T2. Phase 2 = T3, T4. Phase 3 = T5, T6, T7, T8, T9. Phase 4 = T10, T11. Phase 5 = T12, T13.

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `DeleteProvider` to `EmailProviderRepository` | 1 method, 1 file | ✅ Granular |
| T2: Add `DeleteProvider` to `LLMProviderRepository` | 1 method, 1 file | ✅ Granular |
| T3: Add `Disconnect` to `email.Service` | 1 method, 1 file | ✅ Granular |
| T4: Add `Disconnect` to `llm.Service` | 1 method, 1 file | ✅ Granular |
| T5: Email `Disconnect` handler method | 1 method, 1 file | ✅ Granular |
| T6: Wire email disconnect route | 1 route line, 1 file | ✅ Granular |
| T7: LLM `Disconnect` handler method | 1 method, 1 file | ✅ Granular |
| T8: Wire LLM disconnect route | 1 route line, 1 file | ✅ Granular |
| T9: Backend build gate | 0 files, verification only | ✅ Granular |
| T10: `useDisconnectEmailProvider` hook | 1 function, 1 file | ✅ Granular |
| T11: Email Disconnect button + dialog | 1 component, 1 file | ✅ Granular |
| T12: `useDisconnectLlmProvider` hook | 1 function, 1 file | ✅ Granular |
| T13: LLM Disconnect button + dialog | 1 component, 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (none) | ✅ Match |
| T2 | None | (none) | ✅ Match |
| T3 | T1 | T1 → T3 | ✅ Match |
| T4 | T2 | T2 → T4 | ✅ Match |
| T5 | T3 | T3 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T4 | T4 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T6, T8 | T6 → T9, T8 → T9 | ✅ Match |
| T10 | T6 | T6 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T8 | T8 → T12 | ✅ Match |
| T13 | T12 | T12 → T13 | ✅ Match |

No dependency points to a later phase - every arrow points forward in phase order or stays within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Repository | unit | unit | ✅ OK |
| T2 | Repository | unit | unit | ✅ OK |
| T3 | Service | unit | unit | ✅ OK |
| T4 | Service | unit | unit | ✅ OK |
| T5 | Handler | unit | unit | ✅ OK |
| T6 | Router wiring + audit integration | none (router) + integration (audit) → highest = integration | integration | ✅ OK |
| T7 | Handler | unit | unit | ✅ OK |
| T8 | Router wiring + audit integration | none (router) + integration (audit) → highest = integration | integration | ✅ OK |
| T9 | none (verification) | - | none | ✅ OK |
| T10 | Frontend hook | unit | unit | ✅ OK |
| T11 | Frontend component | unit | unit | ✅ OK |
| T12 | Frontend hook | unit | unit | ✅ OK |
| T13 | Frontend component | unit | unit | ✅ OK |

No violations.
