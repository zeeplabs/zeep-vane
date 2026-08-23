# Company Settings Validation

**Date**: 2026-08-23
**Iteration**: 2/3 (re-verification after the round-1 FAIL; this report supersedes it)
**Spec**: `.specs/features/company-settings/spec.md`
**Diff range**: `2a7c14d..HEAD` (`5ffd44b`) - 17 commits (14 feature + 3 fix: `7daa93a`, `a9a3705`, `5ffd44b`)
**Verifier**: independent sub-agent (author ≠ verifier). Full re-derivation from `spec.md`; the round-1 report was read as history only - every AC citation below was re-located in the current tree and every mutation was re-injected from scratch.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Migration (singleton) | ✅ Done | `internal/db/migrations/0012_company_settings.up.sql` + 2 integration tests |
| T2 `CompanySettingsRepository` | ✅ Done | 3 methods, 3 integration tests |
| T3 `UPLOADS_DIR` config | ✅ Done | set + default tests |
| T4 `uploads.Save` | ✅ Done | 3 unit tests (fresh dir, cross-ext overwrite, same-ext overwrite) |
| T5 Handler `Get`/`Update` | ✅ Done | 4 integration tests |
| T6 Handler `UploadLogo` | ✅ Done | **was ⚠️ Partial in round 1** - now 8 integration tests including the missing 500/write-failure case (`:404`), the just-under-limit boundary case (`:367`) and the multipart field-name contract (`:457`). Matrix line "…/500 for `UploadLogo` - SET-13" now satisfied. Retains the declared, reviewed `SPEC_DEVIATION` (SVG sniffing). |
| T7 `logoFileHandler` | ✅ Done | 4 integration tests incl. traversal + missing file |
| T8 Wire admin routes | ✅ Done | 403/401/owner-pass matrix over all 3 routes + unauthenticated `/uploads/` |
| T9 Dual-mount `/uploads/` | ✅ Done | both halves covered (`/uploads/{filename}` → file bytes; `/` → status JSON) |
| T10 Public status enrichment | ✅ Done | production + I12 preview + null-logo |
| T11 MSW handlers | ✅ Done | **was ⚠️ Partial in round 1** - the jsdom multipart-read limitation is real (re-confirmed as an environment constraint, not re-tested this round), but it is no longer a coverage hole: the field-name contract is now pinned on both sides *outside* MSW (Go literal test + a `fetch` spy that inspects the `FormData` in-process) |
| T12 `settings/hooks.ts` | ✅ Done | 6 tests (5 + the new field-name contract test) |
| T13 `SettingsPage.tsx` | ✅ Done | 4 tests |
| T14 `public-status/hooks.ts` | ✅ Done | 2 tests; `mockData.companySettings` no longer imported by any production path (verified: the only remaining reference in `web/src` is its own definition, `src/lib/mockData.ts:245`, consumed solely by `test/msw/handlers.ts`) |

Out-of-plan change (unchanged from round 1, re-reviewed): `web/src/lib/apiClient.ts` suppresses the forced `Content-Type: application/json` when the body is `FormData`. Required for multipart to work at all, minimal, and indirectly asserted by the MSW content-type check. Accepted.

No fix-commit regressions elsewhere: the 3 fix commits touch exactly 2 files (`internal/api/company_settings_handler_test.go`, `web/src/features/settings/hooks.test.ts`) - **test files only**, zero production code changed after round 1.

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SET-01 PATCH valid name+email → persist + 200 | 200 + updated JSON, values persisted | `internal/api/company_settings_handler_test.go:182` `rec.Code != http.StatusOK`; `:189` `resp.Name != "Acme Inc."`; `:201` re-GET `getResp.Name != "Acme Inc."`; repo level `internal/db/company_settings_repository_test.go:63` | ✅ PASS |
| SET-02 non-owner admin GET → 403 | `403 Forbidden` | `internal/cli/routes_test.go:495` `TestAdminRouter_CompanySettings_OperatorAndViewer_403` → `:510` `rec.Code != http.StatusForbidden` (operator+viewer × all 3 routes); `:545` no session → `:556` 401; `:523` owner passes authorization | ✅ PASS |
| SET-03 fresh install GET → seeded row, never 404 | 200, `name:""`, `contact_email:""`, `logo_url:null` | `internal/api/company_settings_handler_test.go:156` 200, `:163` `resp.Name != ""`, `:169` `resp.LogoURL != nil`; DB level `internal/db/company_settings_migration_test.go:36` `count != 1`, `:42-53` seeded values | ✅ PASS |
| SET-04 empty `name` → 422, row untouched | 422 + prior value survives | `internal/api/company_settings_handler_test.go:219` 422; `:228` `getResp.Name != "Acme Inc."` (pre-seeded at `:214`, then re-read) | ✅ PASS |
| SET-05 invalid `contact_email` → 422, row untouched | 422 + prior value survives | `internal/api/company_settings_handler_test.go:245` 422; `:254` `getResp.ContactEmail != "owner@acme.example.com"` | ✅ PASS |
| SET-06 exactly one row, always | second row rejected at DB level; seeded once | `internal/db/company_settings_migration_test.go:77` `INSERT ... VALUES (2)` must error; `:36` `count != 1` | ✅ PASS |
| SET-07 PNG/SVG ≤10 MB → stored, `logo_url` updated, 200 | 200 + `logo_url` = app-served path + file on disk | PNG `internal/api/company_settings_handler_test.go:269` 200, `:276` `*resp.LogoURL != "/uploads/logo.png"`, `:280` `os.Stat(uploadsDir/logo.png)`, `:289` persisted via re-GET; SVG `:304`/`:311`/`:314` | ✅ PASS (real multipart parse + real byte sniff + real `os.Stat`) |
| SET-08 >10 MB → 422, no file written, `logo_url` unchanged; bound = **10 MB** | 422 + 0 files + `logo_url` unchanged, and the bound is exactly 10 MB | `internal/api/company_settings_handler_test.go:340` 422, `:348` `len(entries) != 0`, `:357` `getResp.LogoURL != nil`. **Bound now pinned to the spec literal**: `:319` `const specMaxLogoBytes = 10 * 1024 * 1024`, payload `:334` `make([]byte, specMaxLogoBytes+1024)` (no longer derived from `maxLogoBytes`). Complementary accepted side: `:367` `TestUploadLogo_JustUnderSizeLimit_200UpdatesLogoURL` sizes the *total request body* to exactly `specMaxLogoBytes-1` (subtracting measured multipart framing overhead, `:373-377`) and asserts `:381` 200 + `:388` `logo_url == "/uploads/logo.png"` + `:391` file on disk | ✅ PASS (round-1 gap closed; sensor M1 **and** the inverse narrowing mutation M2 both die - see Sensor) |
| SET-09 non-PNG/SVG → 422, previous logo untouched | 422 + `logo_url` unchanged | `internal/api/company_settings_handler_test.go:496` 422; `:505` `getResp.LogoURL != nil` | ✅ PASS |
| SET-10 new upload removes/overwrites old file | exactly 1 file in dir | `internal/api/company_settings_handler_test.go:535` `len(entries) != 1`, `:551` remaining/persisted == `/uploads/logo.svg`; unit `internal/uploads/store_test.go:56`/`:63` | ✅ PASS |
| SET-11 `UPLOADS_DIR` env, default `./data/uploads` | verbatim when set; `./data/uploads` when unset | `internal/config/config_test.go:69` `cfg.UploadsDir != "/mnt/vane-uploads"`; `:85` `cfg.UploadsDir != "./data/uploads"` | ✅ PASS |
| SET-12 stored logo served, no auth | file bytes, 200, no session | `internal/api/logo_file_handler_test.go:89` `TestLogoFileHandler_NoAuthenticationRequired_200` → `:100` 200 with no Authorization/cookie; real admin router `internal/cli/routes_test.go:576` not 401/403; public host listener `internal/cli/serve_test.go:324` → `:357` `body == "fake-logo-bytes"` (proves the logo file, not the status JSON) | ✅ PASS |
| SET-13 write failure → 500, DB not updated, error logged | 500 + `logo_url` unchanged + logged error | `internal/api/company_settings_handler_test.go:404` `TestUploadLogo_SaveFailure_500NoLogoURLChange`: seeds a real successful upload first (`:411-418`), forces a **real** write failure via `os.Chmod(uploadsDir, 0o500)` (`:420`, restored in `t.Cleanup`), asserts `:426` `rec.Code != http.StatusInternalServerError`, then restores write access and asserts on a following GET `:442` `*getResp.LogoURL != "/uploads/logo.png"` - a **concrete unchanged value**, not merely non-nil | ✅ PASS (round-1 gap closed; sensor M3 dies). ⚠️ Sub-clause note below: the "SHALL log" half is implemented (`internal/api/company_settings_handler.go:161`) but not asserted. |
| SET-14 never-uploaded → `logo_url: null` | `logo_url: null` | `internal/api/company_settings_handler_test.go:169` `resp.LogoURL != nil` on a fresh install | ✅ PASS |
| SET-15 both public endpoints include real name/logo | `company.name`/`company.logo_url` from `company_settings` | production `internal/api/public_status_handler_test.go:414` `body.Company.Name != "Acme Status"`, `:417` `*body.Company.LogoURL != "/uploads/logo.png"`; I12 preview `internal/api/public_status_preview_handler_test.go:129`; frontend `web/src/features/public-status/hooks.test.ts:36-37` | ✅ PASS |
| SET-16 null logo → `logo_url: null` publicly | `logo_url: null`, no placeholder | `internal/api/public_status_handler_test.go:444` `body.Company.LogoURL != nil`; `web/src/features/public-status/hooks.test.ts:56` `expect(result.current.data!.logo_url).toBeNull()` | ✅ PASS |

**Status**: ✅ **16/16 ACs matched their spec-defined outcome. 0 gaps, 0 discrimination gaps.**

**Residual sub-clause note (not a gap, Minor / informational):** SET-13's third clause ("SHALL log the underlying error") has no assertion. The code path exists and is executed by the new test (`company_settings_handler.go:161`, `h.logger.Error("company-settings: failed to save logo file", zap.Error(err))`), and the two *observable* outcomes the AC specifies (500, DB untouched) are both pinned and mutation-proven. Adding a `zaptest/observer` assertion would be strictly better but is not load-bearing: no spec behavior depends on the log line, and it is not a silent-widening risk of the kind the sensor targets. Recorded rather than silently passed.

---

## Cross-Layer Contract: multipart field name `logo` (round-1 Minor gap)

Round 1 flagged that `"logo"` was an independent literal on each side with no test observing it. Both sides are now pinned, and I confirmed each pin is genuinely load-bearing by mutating that side (M4, M5 below):

- **Go side**: `internal/api/company_settings_handler_test.go:457` `TestUploadLogo_MultipartFieldNameContract_LogoAccepted` builds its multipart part with the **hardcoded literal** `writer.CreateFormFile("logo", ...)` (`:462`) instead of going through `buildMultipartLogoRequest`, which uses `logoFormFieldName` and would move with any rename. Asserts `:480` 200.
- **Frontend side**: `web/src/features/settings/hooks.test.ts:81` spies on `globalThis.fetch`, finds the upload call, and asserts `:99` `expect(init?.body).toBeInstanceOf(FormData)` + `:100` `expect((init!.body as FormData).get("logo")).toBe(file)` - inspecting the `FormData` in-process, bypassing MSW's body reader entirely. This also strengthens SET-07 at the frontend layer: the round-1 "not proven" item *"that the selected `File` is actually attached to the body at all"* is now proven (`.toBe(file)` is identity on the exact `File` instance).

The chosen approach is exactly the one round 1 recommended (spy on `fetch`, do not read the body inside an MSW handler), so no new jsdom hang risk was introduced - re-confirmed by the frontend suite running green in 5.8 s with no timeouts.

---

## Discrimination Sensor

Isolated `git worktree add <scratch> HEAD` (never `git stash`); the real tree was read-only throughout. Five mutations, all re-injected from scratch this round (M1 is the round-1 survivor, re-run; M2-M5 are new, and M2/M3/M4/M5 all land in code guarded by the three fix commits):

| # | File:line | Mutation | Tests run | Killed? |
| - | --------- | -------- | --------- | ------- |
| 1 | `internal/api/company_settings_handler.go:19` | `maxLogoBytes = 10 << 20` → `100 << 20` (widen bound 10×) - **the round-1 survivor** | `go test -tags=integration -run TestUploadLogo ./internal/api` | ✅ **Killed** - `TestUploadLogo_OverSizeLimit_422NoLogoURLChange` fails (round 1: survived) |
| 2 | `internal/api/company_settings_handler.go:19` | `maxLogoBytes = 10 << 20` → `5 << 20` (**narrow** bound - the inverse direction, untested in round 1) | same | ✅ Killed - `TestUploadLogo_JustUnderSizeLimit_200UpdatesLogoURL` fails. The bound is now pinned from *both* sides. |
| 3 | `internal/api/company_settings_handler.go:160-164` | removed the `return` in the `uploads.Save` error branch, so a failed write falls through to `UpdateLogoURL` (violates SET-13's "SHALL NOT update `logo_url`") | same | ✅ Killed - `TestUploadLogo_SaveFailure_500NoLogoURLChange` fails on the follow-up GET |
| 4 | `internal/api/company_settings_handler.go:115` | `logoFormFieldName = "logo"` → `"logo_file"` (breaks the real wire contract with the frontend) | same | ✅ Killed - **only** `TestUploadLogo_MultipartFieldNameContract_LogoAccepted` fails, confirming that test is the sole thing pinning the literal (every other test moves with the constant) |
| 5 | `web/src/features/settings/hooks.ts:40` | `formData.append("logo", file)` → `formData.append("logo_file", file)` (frontend half of the same contract) | `npx vitest run src/features/settings` | ✅ Killed - 1 failed / 9 passed, failing exactly at `hooks.test.ts:100` |

**Sensor depth**: lightweight-plus (5 mutations - above the ≥1-3 default, deliberately weighted onto the three areas the fix commits claim to cover, plus both directions of the size bound and both sides of the cross-layer contract)
**Result**: **5/5 killed - PASS ✅**
**Isolation**: baseline `git status --porcelain` before any sensor work =
```
 M .specs/LESSONS.md
 M .specs/lessons.json
 M web/tsconfig.tsbuildinfo
?? .specs/features/company-settings/validation.md
```
after `git worktree remove --force` + `git worktree prune`: byte-identical (`diff` → no output). ✅ No mutation leaked into the real tree.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ - narrow store interfaces, no filesystem abstraction layer, no storage-backend hook |
| Surgical changes | ✅ - the 3 fix commits touch **test files only** (2 files, +175/-9); zero production code changed after round 1, so no fix could have altered runtime behavior elsewhere |
| No scope creep | ✅ - every Out-of-Scope item (multi-tenant, object storage, resizing, version history, invite resend) absent |
| Matches patterns | ✅ - `domains_handler.go` interface style, `CORSAllowedOrigin` config style, `resetAdmins` MSW style; the new tests follow the file's existing doc-comment-names-its-AC convention |
| Only touched required files | ⚠️ - `web/src/lib/apiClient.ts` is outside every task's `Where`, but is required (a forced `application/json` breaks multipart) and minimal. Unchanged judgement from round 1: accepted. |
| Spec-anchored outcome check | ✅ - all 16 ACs assert the spec's exact value; SET-08 no longer self-references the constant under test |
| Per-layer Coverage Expectation met | ✅ - the matrix line for `CompanySettingsHandler` ("200/422(size)/422(mime)/500 for `UploadLogo`") is now fully satisfied; every other matrix row was already met |
| Every test maps to a spec requirement | ✅ - no unclaimed tests. All 3 new tests carry doc comments naming their AC/contract; `TestUploadLogo_MultipartFieldNameContract_LogoAccepted` maps to SET-07's wire contract and is justified in its comment. |
| No test weakened or deleted | ✅ - `git diff 2a7c14d..HEAD -- '*_test.go'` shows **+35 / -0** `func Test`, `+12 / -0` `it(` in web tests, and **zero deleted files**. The only `-` lines in the fix commits are the `buildMultipartLogoRequest` → `multipartLogoBody` helper extraction (behavior-preserving, verified by reading both) and `maxLogoBytes+1024` → `specMaxLogoBytes+1024` (a strengthening). |
| Documented guidelines followed | ✅ - none in repo (no `AGENTS.md`/lint guideline file); strong defaults applied, consistent with the `admin-frontend` batch |

Declared deviation, re-reviewed and still accepted: `internal/api/company_settings_handler.go:189-198` `SPEC_DEVIATION` - `design.md` prescribed `http.DetectContentType` alone, but `net/http` has no SVG sniff signature, so a literal reading could never satisfy spec.md P1-AC1 for `image/svg+xml`. `isLikelySVG` stays byte-content-based (never trusts the client `Content-Type`) and is covered by `TestUploadLogo_ValidSVG_200UpdatesLogoURL` + `TestUploadLogo_WrongMIMEType_422NoLogoURLChange`. Correct call, correctly declared.

Notes carried forward (both Minor, neither new nor introduced by the fixes):
- **Shared-DB test isolation**: the singleton `company_settings` row is reset by raw `UPDATE ... WHERE id = 1` in the `internal/db`, `internal/api` and `internal/cli` helpers against one shared database - the same pattern behind the known pre-existing `admin_repository_test.go` flake (see Gate Check).
- **Root-run caveat on the new SET-13 test**: `os.Chmod(uploadsDir, 0o500)` does not stop a process running as `uid 0`, so `TestUploadLogo_SaveFailure_500NoLogoURLChange` would not induce a failure under a root CI container and would fail loudly (asserting 500, getting 200) rather than silently passing. That is the safe failure mode - it degrades to a visible red, never to a false green - so it is recorded, not raised as a gap.
- `web/src/lib/mockData.ts:245` `companySettings` is now consumed only by `test/msw/handlers.ts`. Dead in production, which is exactly what the spec's Success Criteria asked for; no action.

---

## Edge Cases

- [x] `UPLOADS_DIR` missing at startup → lazy `MkdirAll` on first use, never a startup failure - `internal/uploads/store_test.go:15-27` (dir path `does-not-exist-yet`)
- [x] Stored logo file missing from disk while `logo_url` still points at it → 404 - `internal/api/logo_file_handler_test.go:73`/`:80`; path traversal rejected `:49`/`:65` (with a real secret file planted one level up)
- [x] Concurrent PATCH + logo upload → last write per field wins - structurally guaranteed (`Update` touches only `name`/`contact_email`, `UpdateLogoURL` only `logo_url`, `internal/db/company_settings_repository.go`), asserted independent by `internal/db/company_settings_repository_test.go:94`. Accepted per spec Assumptions; no concurrency test required.

---

## Gate Check

- **Gate command (Build, backend)**: `go build ./... && gofmt -l . && go vet ./...` → **clean** (build OK, `gofmt` no output, `vet` silent)
- **Gate command (Build, frontend)**: `cd web && npm run build` → **✓ built**, 200 modules transformed, 408.38 kB
- **Backend tests**: `go test -tags=integration -count=1 ./internal/db ./internal/api ./internal/cli ./internal/config ./internal/uploads` → `internal/api` ok, `internal/cli` ok, `internal/config` ok, `internal/uploads` ok; `internal/db` **FAIL on the pre-existing, unrelated flake** (`admin_repository_test.go:268 Create() operator returned unexpected error: db: email already registered` / `CountActiveOwners`). Re-run isolated: `go test -tags=integration -count=1 ./internal/db` → **ok**, and all 5 company-settings tests in that package pass individually. Root cause is the documented shared-database contention when packages run in parallel, in `admin_repository_test.go` - a file this feature does not touch. Not a company-settings failure.
- **Frontend tests**: `cd web && npm run test` → **40 files, 141 passed, 0 failed, 0 skipped**
- **Test count before feature**: 129 frontend tests; Go suite pre-feature (at `2a7c14d`)
- **Test count after feature**: 141 frontend (**+12**); **+35 new Go test functions**, 0 removed
- **Delta**: **+47 tests** total, of which **+4 came from the 3 fix commits** (3 Go: just-under-limit, save-failure, field-name contract; 1 web: FormData key contract). Round-1 count was 140 frontend / +32 Go → the delta is strictly positive in both directions, confirming nothing was traded away to close the gaps.
- **Skipped tests**: none
- **Failures**: none attributable to this feature (see the `internal/db` note above)

---

## Fix Plans

None. All three round-1 gaps are closed and empirically re-proven:

| Round-1 gap | Severity | Status this round | Proof |
| --- | --- | --- | --- |
| SET-08 bound not pinned (surviving mutant M1) | Major | ✅ Closed | payload now derives from the spec literal `specMaxLogoBytes`; sensor M1 (widen 10×) **and** M2 (narrow 2×) both killed |
| SET-13 zero coverage of the write-failure branch | Major | ✅ Closed | real permission-denied failure, 500 asserted, `logo_url` asserted unchanged **at a concrete value** (`/uploads/logo.png`, seeded by a prior successful upload); sensor M3 killed |
| `logo` multipart field name untested on both sides | Minor | ✅ Closed | Go literal test + frontend `fetch`-spy `FormData` assertion; sensor M4 (Go rename) and M5 (web rename) both killed |

---

## Requirement Traceability Update

| Requirement | Previous Status (round 1) | New Status |
| ----------- | ------------------------- | ---------- |
| SET-01 | ✅ Verified | ✅ Verified |
| SET-02 | ✅ Verified | ✅ Verified |
| SET-03 | ✅ Verified | ✅ Verified |
| SET-04 | ✅ Verified | ✅ Verified |
| SET-05 | ✅ Verified | ✅ Verified |
| SET-06 | ✅ Verified | ✅ Verified |
| SET-07 | ✅ Verified (backend e2e; frontend partial) | ✅ Verified (frontend payload now pinned too) |
| SET-08 | ❌ Needs Fix (bound not pinned) | ✅ **Verified** |
| SET-09 | ✅ Verified | ✅ Verified |
| SET-10 | ✅ Verified | ✅ Verified |
| SET-11 | ✅ Verified | ✅ Verified |
| SET-12 | ✅ Verified | ✅ Verified |
| SET-13 | ❌ Needs Fix (no evidence) | ✅ **Verified** (log sub-clause noted, non-blocking) |
| SET-14 | ✅ Verified | ✅ Verified |
| SET-15 | ✅ Verified | ✅ Verified |
| SET-16 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: ✅ **Ready**

**Spec-anchored check**: **16/16** ACs matched their spec-defined outcome; 0 gaps, 0 spec-precision gaps (1 informational sub-clause note on SET-13's logging clause)
**Sensor**: **5/5** mutations killed (including the round-1 survivor and the inverse of it), real tree byte-identical to baseline
**Gate**: backend build/fmt/vet clean, 4 packages `ok` + `internal/db` `ok` in isolation (pre-existing unrelated flake only under parallel packages), frontend 141/141, frontend build ✓, 0 skips
**Test delta**: +47 vs pre-feature (+4 from the fix commits), 0 tests deleted, 0 assertions weakened

**What works**: singleton migration with a DB-level `CHECK (id = 1)`; independent `Update`/`UpdateLogoURL`; validation → 422 with proven non-mutation of the row; MIME allowlist with a working SVG path behind a declared, reviewed `SPEC_DEVIATION`; the 10 MB bound now pinned to the spec literal from both directions; single-file overwrite at unit and HTTP level; the write-failure branch genuinely exercised with a real permission error, proving the DB is never pointed at a file that was never written; owner-only RBAC on all 3 routes with 401/403/owner-pass; unauthenticated `/uploads/` on both listeners with the dual-mount killed by mutation; public status enrichment on production and I12 preview including the null-logo case; the `logo` multipart contract pinned on both sides and mutation-proven; `mockData.companySettings` gone from every production path.

**Issues found**: none blocking. Recorded for the record: SET-13's "SHALL log" sub-clause is unasserted (implemented at `company_settings_handler.go:161`); the new SET-13 test's `chmod`-based failure injection is a no-op under a root-running CI container, but degrades to a visible failure rather than a false pass; the shared-database test-isolation pattern (pre-existing) remains the source of the `internal/db` parallel-run flake.

**Next steps**: close the feature. Update `spec.md` requirement statuses to ✅ Verified for SET-01..SET-16.
