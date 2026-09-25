# tenant-domain-shared-listener Validation

**Date**: 2026-09-25
**Spec**: `.specs/features/tenant-domain-shared-listener/spec.md`
**Diff range**: `a386c08..9ac1bfc` (feature commits `557e47a`, `1c81eef`, `ea16ce1`, `727402d`, `f9145f4`, `9ac1bfc`)
**Verifier**: standalone fresh-eyes pass (see caveat below)

> **Caveat (honest disclosure)**: the harness had no dispatchable independent sub-agent (the configured
> reviewer model failed to resolve). Validation therefore ran as the skill's documented standalone
> fallback: a fresh re-read of `spec.md` and the diff, evidence-or-zero mapping, and the discrimination
> sensor in a throwaway git worktree. Author == verifier for this pass, so the independence guarantee is
> weaker than a true sub-agent Verifier. The sensor and the gate are still empirical and independently
> reproducible.

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `newPublicStatusMux` extracted; pure refactor, build gate green |
| T2   | ✅ Done | `HostRouter` fallback param; 7 call sites updated; unit + router integration green |
| T3   | ✅ Done | `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER` opt-in flag; config tests green |
| T4   | ✅ Done | `serveAdminHandler` wiring; 3 new integration tests green |
| T5   | ✅ Done | README config row + EasyPanel runbook |
| T6   | ✅ Done | `AD-038` recorded in `.specs/STATE.md` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| P1-AC1 / TDS-01: flag on + `Host` matches a published `StatusPage` | Served by the same public status path as `newHTTPSServer`, same tenant-scoped RLS transaction | `internal/cli/serve_handler_test.go:106` - `if !containsServiceName(body.Services, serviceName)` after `fetchPublicStatus` (200 + JSON decode); tenant scoping from `internal/router/host_router_tenant_test.go:277` - `beginner.tenantIDs[0] != tc.want.tenantID` and `:281` - `services[0] != tc.want.serviceName` | ✅ PASS |
| P1-AC2 / TDS-02: flag on + `Host` does NOT match any published `StatusPage` | Falls through to `buildAdminRouter` unchanged | `internal/cli/serve_handler_test.go:134` - `resp.StatusCode != http.StatusOK` and `:144` - `health.Status != "ok"` for `GET /healthz` on an unmatched admin host | ✅ PASS |
| P1-AC3 / TDS-03: flag disabled (default) | Byte-for-byte identical to today; no `GetByHostname` lookup on the admin path | `internal/cli/serve_handler_test.go:71` - `resp.StatusCode != http.StatusNotFound` (a tenant host returns the admin router's JSON 404, not the public 200) and `:74` - content-type must be `application/json`; default polarity pinned at `internal/config/config_test.go:479` - `TenantDomainsOnAdminListener` must stay false for unset/`"false"`/`"1"`/`"TRUE"` | ✅ PASS |
| P1-AC4 / TDS-04: `newHTTPSServer` / `tls.HostPolicy` / `tls.NewManager` unaffected | Dedicated `:443` listener behavior unchanged | `internal/cli/serve.go:235` - `router.HostRouter(..., http.HandlerFunc(http.NotFound))` preserves the old unmatched-host 404; regression assertion `internal/router/host_router_test.go:133` (integration, `TestHostRouter_UnrecognizedHost_404`); no diff under `internal/tls/` in `a386c08..9ac1bfc` | ✅ PASS |
| P2-AC1 / TDS-05: docs describe the manual procedure step by step, EasyPanel as the worked example | Numbered end-to-end steps | `README.md:337` ("Registering a tenant domain (EasyPanel)") + steps at `README.md:339`-`:347` | ✅ PASS (prose; no automated test by design) |
| P2-AC2 / TDS-06: docs state the step is manual today and point at the deferred automation | Explicit "manual" + deferred pointer | `README.md:339` - "**manual, per tenant domain, today**"; `README.md:349` - "**Deferred automation**" blockquote naming the out-of-scope EasyPanel API integration | ✅ PASS (prose; no automated test by design) |

**Status**: ✅ All ACs covered, no gaps.
**Spec-precision notes**: none blocking. One method note: T4's "Done when" suggested proving the flag-off path with a call-counting fake `statusPageHostLookup`; the disabled path constructs no lookup, so the proof is behavioral (admin JSON 404 vs public 200), which is documented in `internal/cli/serve_handler_test.go:52`-`:58` and is killed by the sensor below.

---

## Discrimination Sensor

Run in a detached git worktree at `9ac1bfc` (`/var/folders/.../vane-sensor`), mutated, tested, restored, then removed. Real worktree `git status --porcelain` was empty before and after.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| 1 | `internal/router/host_router.go:104` | Unresolved-host branch `fallback.ServeHTTP(w, r)` → `http.NotFound(w, r)` | ✅ Killed - `TestHostRouter_UnmatchedOrUnpublishedHostname_ServesFallback/unregistered_hostname` fails at `host_router_fallback_test.go:89` |
| 2 | `internal/config/config.go` (flag load) | Opt-in `== "true"` → opt-out `!= "false"` (default flips to true) | ✅ Killed - `TestLoad_TenantDomainsOnAdminListenerNotTrue_DefaultsFalse` fails at `config_test.go:479` for `"1"`/`"TRUE"` |
| 3 | `internal/cli/serve.go:260` | `serveAdminHandler` guard flipped (`if !flag → if flag`), reversing the wiring | ✅ Killed - both `TestServeAdminHandler_FlagDisabled_TenantHostReachesAdminRouterOnly` and `TestServeAdminHandler_FlagEnabled_TenantHostServesPublicStatus` fail |

**Sensor depth**: lightweight (3 behavior-level mutations, one per new decision boundary: fallback dispatch, flag polarity, listener wiring).
**Result**: 3/3 killed - PASS ✅
**Isolation**: scratch worktree removed; real worktree porcelain matched the empty baseline.

---

## Edge Cases

- [x] Unregistered hostname falls through to fallback: `host_router_fallback_test.go:89`-`:97` asserts the fallback's own 418 + body, not a 404.
- [x] Registered-but-draft hostname falls through to fallback: same test, `"registered but unpublished page"` case.
- [x] Flag enabled + unmatched admin host still serves the admin router: `serve_handler_test.go:134`, `:144`.
- [x] Flag off + tenant hostname does not leak into the public path: `serve_handler_test.go:71`, `:74`.
- [x] Unmatched host on the `:443` listener still 404s (no regression): `host_router_test.go:133`, green in the full gate.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ `serveAdminHandler` is a 6-line wiring function; `newPublicStatusMux` is a verbatim extraction |
| Surgical changes | ✅ Only the fallback call sites, the signature, the wiring, config, docs |
| No scope creep | ✅ No endpoint, schema, or frontend change; deferred automation left out |
| Matches patterns | ✅ Follows the `HTTPSEnabled`/`SecureCookies` flag-loading pattern (opposite default) and the existing `HostRouter` RLS flow |
| Spec-anchored outcomes | ✅ Each assertion targets the spec-defined outcome (200 JSON / 404 JSON / status `ok` / distinct fallback body) |
| Every test maps to a requirement | ✅ 3 new router tests → TDS-01/02; 2 config tests → TDS-03; 3 CLI integration tests → TDS-01/02/03 |
| Guidelines followed | ✅ `AGENTS.md` (English code-facing text; no new migration; integration gate on a disposable container) |

---

## Gate Check

- **Gate commands** (per `AGENTS.md` §3, the authoritative form of tasks.md's Full gate):
  - `make test-integration` (fresh disposable Postgres, `-p 1`, `-count=1`): **all packages ok, exit 0**
  - `go test ./...`: **491 passed, 0 failed**
  - `gofmt -l .`: clean
  - `go vet ./...`: clean
- **Failures**: none
- **Skipped**: none

---

## Requirement Traceability Update

Spec.md uses no inline requirement IDs (IDs live in `tasks.md`). Statuses:

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| TDS-01 | Implemented | ✅ Verified |
| TDS-02 | Implemented | ✅ Verified |
| TDS-03 | Implemented | ✅ Verified |
| TDS-04 | Implemented | ✅ Verified |
| TDS-05 | Implemented | ✅ Verified (prose) |
| TDS-06 | Implemented | ✅ Verified (prose) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 6/6 ACs matched the spec-defined outcome; 0 coverage gaps; 1 non-blocking method note (behavioral proof for TDS-03 in place of the suggested fake lookup counter).
**Sensor**: 3/3 mutations killed.
**Gate**: full integration gate + unit suite green.

**What works**: Tenant custom domains resolve through the admin listener when `VANE_TENANT_DOMAINS_ON_ADMIN_LISTENER=true`, with the admin SPA/API unchanged on every other `Host`; the dedicated `:443` path is untouched; the flag defaults off; the README runbook documents the manual EasyPanel step.

**Issues found**: none blocking.

**Next steps**: none for the feature. Optional future work is the deferred EasyPanel API automation, explicitly out of scope.
