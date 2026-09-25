# Tenant Custom Domain Behind a Shared Reverse Proxy Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/tenant-domain-shared-listener/design.md`
**Status**: Approved

---

## Requirement IDs

Short codes assigned here for traceability (spec's Acceptance Criteria have no prior IDs of their own):

| ID | Acceptance Criterion (spec.md) |
| --- | --- |
| TDS-01 | P1-AC1: flag on + matching `Host` → same public status response as `newHTTPSServer`, same RLS tenant scoping |
| TDS-02 | P1-AC2: flag on + non-matching `Host` → falls through to `buildAdminRouter` unchanged |
| TDS-03 | P1-AC3: flag off (default) → byte-for-byte identical to today, no added `GetByHostname` lookup |
| TDS-04 | P1-AC4: `newHTTPSServer`/`tls.HostPolicy`/`tls.NewManager` unaffected by this feature existing |
| TDS-05 | P2-AC1: README/runbook documents the manual EasyPanel domain-registration procedure |
| TDS-06 | P2-AC2: documentation states the step is manual today and points at the deferred automation |

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/router/host_router_tenant_test.go`, `internal/config/config_test.go`, `internal/cli/routes_test.go`) plus spec ACs. No `AGENTS.md`/lint-config guideline files found beyond the repo-wide `AGENTS.md` already in context - strong default applied (1:1 to spec ACs, every listed edge case), same depth as `status-page-domain-attach`/`saas-transactional-email` tasks.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go router (`HostRouter`, new `fallback` param) | unit | Matched hostname behavior unchanged (existing assertions preserved verbatim); unmatched hostname now invokes `fallback` instead of a literal `404` - proven with a distinguishable fake fallback (e.g. a handler writing a unique body), not just re-asserting `404` - TDS-01, TDS-02 | `internal/router/host_router_tenant_test.go` | `go test ./internal/router` |
| Go config (`TenantDomainsOnAdminListener`) | unit | Set `"true"` → `true`; unset/any other value → `false` (default) - TDS-03 | `internal/config/config_test.go` | `go test ./internal/config` |
| Go wiring (`cli.newPublicStatusMux` extraction) | unit | Pure refactor - existing behavior via `newHTTPSServer`'s own path unchanged; no new test required beyond confirming the build still passes, since this task moves code without changing it | n/a (verified by existing suite + build) | `go build ./... && go test ./...` |
| Go wiring (admin-listener conditional wrap) | integration | Flag off: admin handler is `buildAdminRouter(...)` directly, `GetByHostname` never called (spy/counter on a fake repository) - TDS-03. Flag on + tenant-domain `Host`: served by public mux with correct RLS-scoped tenant - TDS-01. Flag on + admin-domain `Host`: served by admin router unchanged (e.g. `/healthz` still responds) - TDS-02 | `internal/cli/serve_handler_test.go` (new, tag `integration` for the DB-backed cases) | `go test -tags=integration ./internal/cli` |
| Docs (`README.md` config table + new runbook section) | none | Build/lint gate only - no automated test for prose | `README.md` | manual review |

## Gate Check Commands

> Generated from `Makefile` (`go test ./...`, `gofmt -l .`, `go vet ./...`) and existing integration-test convention (`go test -tags=integration ./...`), matching the commands already established in prior features' `tasks.md`. No frontend changes in this feature - no `web/` gate needed.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test | `go test -tags=integration ./... && gofmt -l . && go vet ./...` |
| Build (backend) | After phase completion | `go build ./... && gofmt -l . && go vet ./...` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend Foundation

Tasks: T1, T2, T3 - see dependency graph in `## Phase Execution Map` below for exact edges.

### Phase 2: Wiring

Task: T4 - see dependency graph below.

### Phase 3: Docs

Tasks: T5, T6 - see dependency graph below.

---

## Task Breakdown

### T1: Extract `newPublicStatusMux` helper

**What**: Lift the 3-route public mux construction (`/uploads/`, `/api/public-status`, `/` → SPA) currently inline in `newHTTPSServer` into its own `newPublicStatusMux(pool *db.Pool, logger *zap.Logger) *http.ServeMux` function. Pure refactor - `newHTTPSServer` calls the new function instead of building the mux inline; no behavior change.
**Where**: `internal/cli/serve.go`
**Depends on**: None
**Reuses**: existing `api.NewPublicStatusHandler`, `api.NewLogoFileHandler`, `web.StaticHandler` construction verbatim, just relocated
**Requirement**: n/a (pure refactor, prerequisite for T4)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `newHTTPSServer` calls `newPublicStatusMux(pool, logger)` instead of constructing the mux inline
- [x] No behavior change - the extracted function's body is identical to what was inline before, only relocated
- [x] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`
- [x] Test count: unchanged (this task adds no new tests, pure refactor)

**Tests**: none (pure refactor, verified by build)
**Gate**: build

**Commit**: `refactor(cli): extract newPublicStatusMux from newHTTPSServer`

---

### T2: `HostRouter` gains a `fallback` parameter

**What**: `router.HostRouter` signature becomes `HostRouter(statusPages statusPageHostLookup, pool tenantTxBeginner, publicHandler, fallback http.Handler) http.Handler`. Both existing `http.NotFound(w, r)` call sites (unresolved hostname, `state != "published"`) become `fallback.ServeHTTP(w, r)`. `newHTTPSServer`'s call site passes `http.HandlerFunc(http.NotFound)` as the new 4th argument, preserving its exact current behavior. Doc comment (`host_router.go:70-74`) rewritten: no longer claims admin dispatch-by-host is "not implemented here" or that unmatched hosts unconditionally get `404` - describes the `fallback` parameter and points at `AD-038` instead.
**Where**: `internal/router/host_router.go`, `internal/cli/serve.go` (the one `HostRouter(...)` call site inside `newHTTPSServer`), `internal/router/host_router_tenant_test.go` (every existing call site gets `http.HandlerFunc(http.NotFound)` as its 4th argument)
**Depends on**: None
**Reuses**: `HostRouter`'s existing resolution/RLS-scoping logic entirely - only the two `http.NotFound` call sites change
**Requirement**: TDS-01, TDS-02 (prerequisite), TDS-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Every pre-existing `HostRouter` test still passes with its original assertions, using `http.HandlerFunc(http.NotFound)` as the 4th argument (matched-case behavior fully unchanged)
- [x] A new test proves the unmatched-hostname case now invokes an arbitrary `fallback` handler instead of a literal `404` - e.g. a fallback that writes a distinguishable response body, asserted on directly (not just re-asserting `404`, since a fallback that happens to also return `404` would pass a weaker test without proving the new behavior)
- [x] `newHTTPSServer`'s own behavior is unchanged (still `404` for an unmatched hostname on the `:443` listener) - confirmed by keeping `newHTTPSServer`'s existing tests, if any, green
- [x] Doc comment no longer contains the stale "not implemented here" / unconditional-404 claims
- [x] Gate check passes: `go test ./... && gofmt -l . && go vet ./...`
- [x] Test count: at least the same as before this task, plus 1+ new (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(router): add fallback parameter to HostRouter`

---

### T3: `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER` config flag

**What**: `config.Config` gains `TenantDomainsOnAdminListener bool`, loaded via `os.Getenv("VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER") == "true"` (opt-in, default `false`) - same section of `config.Load()` as `SecureCookies`/`DeploymentMode`.
**Where**: `internal/config/config.go`, `internal/config/config_test.go`
**Depends on**: None
**Reuses**: existing boolean-flag loading pattern (`HTTPSEnabled`/`SecureCookies`), just opposite default polarity
**Requirement**: TDS-03 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER=true` → `Config.TenantDomainsOnAdminListener == true`
- [ ] Unset, empty, or any value other than the literal `"true"` → `false`
- [ ] Gate check passes: `go test ./internal/config`
- [ ] Test count: at least the same as before this task, plus 2+ new (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER flag`

---

### T4: Wire `HostRouter` onto the admin listener behind the flag

**What**: Extract the admin listener's handler construction (currently the inline `buildAdminRouter(pool, cfg, logger, pollerManager)` expression at `serve.go:123`) into a small unexported `serveAdminHandler(pool *db.Pool, cfg config.Config, logger *zap.Logger, pollerManager *PollerManager) http.Handler` function, so it is directly testable without starting a real network listener. When `cfg.TenantDomainsOnAdminListener` is `true`, it returns `router.HostRouter(db.NewStatusPageRepository(pool), pool, newPublicStatusMux(pool, logger), buildAdminRouter(pool, cfg, logger, pollerManager))`; when `false`, it returns `buildAdminRouter(pool, cfg, logger, pollerManager)` directly (zero added logic on the disabled path). `RunE` calls `serveAdminHandler(...)` instead of `buildAdminRouter(...)` directly.
**Where**: `internal/cli/serve.go`, `internal/cli/serve_handler_test.go` (new)
**Depends on**: T1, T2, T3
**Reuses**: `newPublicStatusMux` (T1), `router.HostRouter` with its new `fallback` param (T2), `cfg.TenantDomainsOnAdminListener` (T3), `db.NewStatusPageRepository`, `buildAdminRouter` (unchanged)
**Requirement**: TDS-01, TDS-02, TDS-03, TDS-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Flag `false` (default): `serveAdminHandler(...)` returns `buildAdminRouter(...)` unchanged - a request to any hostname reaches the admin router directly, with no `GetByHostname` call happening anywhere in the path (proven via a call-counting fake `statusPageHostLookup`, confirming it is never even constructed/invoked on this path)
- [ ] Flag `true` + request with a `Host` header matching a published status page: response body/behavior matches what `newPublicStatusMux` would serve directly (public status JSON / logo / SPA), and the request's tenant-scoped RLS transaction is opened against that status page's `TenantID`
- [ ] Flag `true` + request with a `Host` header that does not match any published status page (including the admin's own domain): response matches `buildAdminRouter(...)`'s behavior unchanged (e.g. `/healthz` still responds `200`)
- [ ] `newHTTPSServer`/`RunE`'s `:443` listener wiring is untouched by this task - still built and started exactly as before when `cfg.HTTPSEnabled`
- [ ] Gate check passes: `go test -tags=integration ./internal/cli && gofmt -l . && go vet ./...`
- [ ] Test count: at least the same as before this task, plus 3+ new (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): serve tenant custom domains through the admin listener when enabled`

---

### T5: README - document the flag and the EasyPanel manual runbook

**What**: Add `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER` to the `README.md` `## 📋 Configuration` table (same row style as `VANE_HTTPS_ENABLED`). Add a new subsection (e.g. under `## 🌐 Public status page routing` or as its own `## 🔀 Tenant custom domains behind a shared reverse proxy` section) with the step-by-step EasyPanel runbook: attach the domain in `vane`'s UI first, then register that same hostname in EasyPanel's Domains feature pointed at the admin app's port, confirm DNS propagation, confirm the status page loads over HTTPS.
**Where**: `README.md`
**Depends on**: T4
**Reuses**: existing `README.md` config-table row format and section structure
**Requirement**: TDS-05, TDS-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Config table has a new row for `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER` (Required: No, Default: `false`, Notes explain the shared-reverse-proxy use case and point at `newHTTPSServer`/`:443` as the alternative for dedicated infra)
- [ ] New runbook subsection lists the concrete EasyPanel steps end to end, with no undocumented step
- [ ] Runbook explicitly states this registration step is manual today and names the deferred automation (Out of Scope in `spec.md`) as the future improvement
- [ ] Gate check passes: manual review (no automated test for prose)

**Tests**: none
**Gate**: n/a (docs)

**Commit**: `docs(readme): document tenant-domain-shared-listener flag and EasyPanel runbook`

---

### T6: Record `AD-038` in `.specs/STATE.md`

**What**: Append the `AD-038` decision (from `design.md`'s Tech Decisions table) to `.specs/STATE.md`'s `## Decisions` section: `HostRouter` supports a caller-supplied fallback instead of a hard-coded `404`, and the admin listener can optionally reuse it (`VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER`) to serve tenant custom domains without dedicating ports 80/443 to `vane` directly.
**Where**: `.specs/STATE.md`
**Depends on**: T4
**Reuses**: existing `AD-NNN` entry format (see `AD-037` as the most recent example)
**Requirement**: n/a (project bookkeeping, not a spec AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-038` entry present with Decision/Reason/Trade-off/Scope/Date/Status fields, same shape as `AD-037`
- [ ] Gate check passes: manual review (no automated test for docs)

**Tests**: none
**Gate**: n/a (docs)

**Commit**: `docs(specs): record AD-038 for tenant-domain-shared-listener`

---

## Phase Execution Map

This is the authoritative dependency graph - every edge below matches a task's `Depends on` field exactly (see the Diagram-Definition Cross-Check table):

```
T1 -> T4
T2 -> T4
T3 -> T4
T4 -> T5
T4 -> T6
```

Execution is strictly sequential within and across phases in task-number order (T1..T6) - there is no intra-phase parallelism. T1, T2, and T3 have no dependencies on each other and could in principle run in parallel, but the protocol executes them in order within Phase 1 regardless.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Extract `newPublicStatusMux` | 1 file, 1 function extraction | ✅ Granular |
| T2: `HostRouter` fallback param | 2 files (signature+comment, 1 call site), 1 test file updated | ✅ Granular |
| T3: Config flag | 1 config field | ✅ Granular |
| T4: Admin-listener wiring | 1 new function, 1 call-site change, 1 new test file | ✅ Granular |
| T5: README docs | 1 file, 1 table row + 1 subsection | ✅ Granular |
| T6: `AD-038` in `STATE.md` | 1 file, 1 decision entry | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming edge | ✅ Match |
| T2 | None | No incoming edge | ✅ Match |
| T3 | None | No incoming edge | ✅ Match |
| T4 | T1, T2, T3 | T1 -> T4, T2 -> T4, T3 -> T4 | ✅ Match |
| T5 | T4 | T4 -> T5 | ✅ Match |
| T6 | T4 | T4 -> T6 | ✅ Match |

Every edge in the `## Phase Execution Map` graph corresponds to exactly one `Depends on` entry above, and vice versa. No task depends on a later-phase task - all dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `newPublicStatusMux` | Go wiring (pure refactor) | n/a (verified by build) | none | ✅ OK |
| T2: `HostRouter` fallback | Go router | unit | unit | ✅ OK |
| T3: Config flag | Go config | unit | unit | ✅ OK |
| T4: Admin-listener wiring | Go wiring | integration | integration | ✅ OK |
| T5: README docs | Docs | none | none | ✅ OK |
| T6: `AD-038` | Docs | none | none | ✅ OK |

No violations - every task's `Tests` field matches its layer's matrix requirement.

---

## Tips

- **Phases are ordered** - Each phase completes before the next; tasks run in order within a phase
- **Reuses = Token saver** - Always reference existing code
- **One commit per task** - commit messages listed above
- **T2's new test is the load-bearing one** - a fallback test that only re-asserts `404` would pass even if the `fallback` parameter were silently ignored inside `HostRouter`; use a fallback handler with a distinguishable response body/status to actually prove it was invoked.
- **T4's flag-off assertion is the load-bearing one for that task** - proving `serveAdminHandler` never even constructs/calls `GetByHostname` when the flag is off is what protects every existing self-hosted/SaaS-without-this-feature deployment from a latency regression; don't settle for "the admin router still responds" alone, since that would also pass if the flag were accidentally wired backward.
