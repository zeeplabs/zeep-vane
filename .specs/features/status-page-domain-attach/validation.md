# Status Page Domain Attach Validation

**Date**: 2026-08-23
**Spec**: `.specs/features/status-page-domain-attach/spec.md`
**Diff range**: `3f7a176..HEAD` (`c9ecc80`) - 18 commits
**Verifier**: independent sub-agent (author ≠ verifier)
**Iteration**: 3/3 (final)
**Verdict**: ✅ **PASS**

Round 1 FAIL (2 gaps: ambiguous list label; surviving URL-guard mutant) and round 2 FAIL (1 gap:
`AttachDomain` concurrency test did not force real contention) were both re-derived from scratch this
round, not taken on trust. All findings from prior rounds are closed. Two **non-blocking documentation
defects** remain and are recorded below (Minor 1 and Minor 2) - neither affects shipped behavior, test
coverage, or the sensor result.

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Migration `0013` nullable + partial unique index | ✅ Done | `2c5e5a2` |
| T2 Nullable `StatusPage` model, `Create`/`List` | ✅ Done | `643cdd4` |
| T3 `AttachDomain` repository method | ✅ Done | `1bbdce2`; concurrency test hardened in `c9ecc80` |
| T4 `PUBLIC_DNS_TARGET` config | ✅ Done | `37c73c7` |
| T5 Relax `Create` validation | ✅ Done | `700cd57` |
| T6 `AttachDomain` handler | ✅ Done | `9369d9e` |
| T7 `InstanceConfigHandler.DNSTarget` | ✅ Done | `4474126` |
| T8 Remove `published`-only preview gate | ✅ Done | `9084425`; old 404-on-draft test rewritten, not deleted |
| T9 Wire routes under `writeRoles` | ✅ Done | `fbaca65` |
| T10 Nullable frontend types | ✅ Done | `3131a08` |
| T11 MSW handlers | ✅ Done | `8aefe2e` |
| T12 `useAttachDomain` / `useDNSTarget` | ✅ Done | `7a8aed3` |
| T13 Domain-less create form | ✅ Done | `79ca54c` |
| T14 `AttachDomainDrawer` | ✅ Done | `e77a390` |
| T15 4 distinguishable states + always-visible preview | ✅ Done | `fa48491`; round-1 fixes: `342536e` (list label), `9146127` (`publicUrl` guard test) |

No partial or blocked tasks.

---

## Spec-Anchored Acceptance Criteria

Mapping note: `spec.md` states 15 acceptance criteria across 3 stories but allocates 14 `SPD-*` IDs;
`SPD-07` covers two criteria (already-attached `409` and invalid-`domain_id` `422`), matching the
implementer's own mapping in `internal/db/status_page_repository.go:96-101`. Every criterion is
evidenced below regardless of ID bookkeeping (see Minor 2).

### P1: Admin previews a status page before any domain exists

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 / SPD-01: `POST /api/status-pages` without domain fields | `201`, `domain_id: null`, `subdomain: null`, `state: "draft"` | `internal/api/status_pages_handler_test.go:268` - `rec.Code != http.StatusCreated`; `:278` `created.DomainID != nil`; `:281` `created.Subdomain != nil`; `:284` `created.State != "draft"`. Repo layer: `internal/db/status_page_repository_test.go:148` (`Create` no-domain → both `nil`). Frontend: `web/src/features/status-pages/hooks.test.ts:24` | ✅ PASS |
| AC2 / SPD-02: preview for a page with `domain_id: null` | `200` + composed preview payload, any state | `internal/api/public_status_preview_handler_test.go:182` - `rec.Code != http.StatusOK` for a page created with no domain (`:172-176`). Payload shape asserted for the same endpoint at `:65` (`200SameShapeAsProduction`) | ✅ PASS |
| AC3 / SPD-03: preview for a page with `domain_id` set and `state != "published"` | `200` (gate removed) | `internal/api/public_status_preview_handler_test.go:160` - `rec.Code != http.StatusOK` on the `draft`+domain fixture (test explicitly documented as the rewrite of the old 404 assertion, `:147-153`) | ✅ PASS |
| AC4 / SPD-04: unknown status page ID | `404` (unchanged) | `internal/api/public_status_preview_handler_test.go:228` - `rec.Code != http.StatusNotFound`. `published`/`tls_failed` unaffected: `:202`, `:218` (both `StatusOK`) | ✅ PASS |
| AC5 / SPD-05: system SHALL NOT require `domain_id`/`subdomain` to create (schema + handler) | Schema nullable; handler accepts absence; partial combo rejected | Schema: `internal/db/status_pages_migration_test.go:15` (NULL pair accepted), `:86` (many NULL rows never blocked). Handler: `internal/api/status_pages_handler_test.go:297` (`subdomain` only → `422`), `:311` (`domain_id` only → `422`), `:268` (neither → `201`), `:148`/`:170` with-domain path unchanged | ✅ PASS |

### P1: Admin attaches a custom domain to an existing status page

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 / SPD-06: valid attach on a domain-less page | `200`, both fields set, `state` still `"draft"` | `internal/api/status_pages_handler_test.go:370` - `rec.Code != http.StatusOK`; `:378` `*updated.DomainID != domainID`; `:381` `*updated.Subdomain != "status"`. State-unchanged asserted at repo layer: `internal/db/status_page_repository_test.go:342` (`..._SucceedsWithStateUnchanged`). Frontend: `web/src/features/status-pages/hooks.test.ts:45` | ✅ PASS |
| AC2 / SPD-07: target already has non-null `domain_id` | `409`, row NOT modified | `internal/api/status_pages_handler_test.go:400` - `rec.Code != http.StatusConflict`. Row-unmodified proven at repo layer: `internal/db/status_page_repository_test.go:380` (`..._ErrDomainAlreadyAttachedRowUnmodified`) | ✅ PASS |
| AC3 / SPD-07: `domain_id` does not reference an existing `Domain` | `422`, row NOT modified | `internal/api/status_pages_handler_test.go:427` - `rec.Code != http.StatusUnprocessableEntity`; repo: `internal/db/status_page_repository_test.go:410` (`..._ErrInvalidDomainRowUnmodified`) | ✅ PASS |
| AC4 / SPD-08: empty `subdomain` | `422`, row NOT modified | `internal/api/status_pages_handler_test.go:413` - `rec.Code != http.StatusUnprocessableEntity` (rejected pre-DB at `internal/api/status_pages_handler.go:123`, so the row is never touched) | ✅ PASS |
| AC5 / SPD-09: `(domain_id, subdomain)` pair already used by another page | `409`, row NOT modified | `internal/api/status_pages_handler_test.go:446` - `rec.Code != http.StatusConflict`; repo: `internal/db/status_page_repository_test.go:431` (`..._ErrDuplicateDomainSubdomainRowUnmodified`); DB constraint: `internal/db/status_pages_migration_test.go:42` | ✅ PASS |
| AC6 / SPD-10: DNS-target read endpoint | `200` with configured string, or `null` if unset | `internal/api/instance_config_handler_test.go:74` - `body.Target == nil \|\| *body.Target != "vane.example.com"`; `:95` - `body.Target != nil` for the unset case. Config layer: `internal/config/config_test.go:92`/`:108`. Frontend: `web/src/features/status-pages/hooks.test.ts:109`/`:116` | ✅ PASS |
| AC7 / SPD-11: both endpoints restricted to `owner`/`operator` (`403` for `viewer`) | `403` viewer; owner/operator pass; `401` no session | Both routes are in `writeRouteCases()`: `internal/cli/routes_test.go:274` (`PATCH /api/status-pages/{id}/domain`), `:286` (`GET /api/instance/dns-target`), driven by `:360` `rec.Code != http.StatusForbidden` (viewer) and `:383` never-401/403 (owner+operator). No-session `401`: `internal/cli/routes_test.go:629`. Wiring: `internal/cli/routes.go:94-95` (`writeRoles`) | ✅ PASS |

### P2: Distinguishable status labels

| Criterion (WHILE X THE Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 / SPD-12: `domain_id: null` | Label distinct from the certificate-pending label + CTA to attach | List: `web/src/features/status-pages/StatusPagesSection.test.tsx:72` - `getAllByText("Sem domínio configurado")`; `:73` - CTA link `"Anexar domínio"`. Detail: `web/src/features/status-pages/StatusPageDetail.test.tsx:98` + `:99` (pending label absent) + `:102` (button opens drawer) | ✅ PASS |
| AC2 / SPD-13: `domain_id` set and `state == "draft"` | DNS/certificate-pending label, replacing "Emitindo certificado" | List: `StatusPagesSection.test.tsx:82` - `getByText("Aguardando validação de DNS/certificado")`; `:83` - `queryByText("Emitindo certificado")` absent; `:84` - no-domain label absent (mutual exclusion). Detail: `StatusPageDetail.test.tsx:110-112` | ✅ PASS |
| AC3 / SPD-14: `published` / `tls_failed` | Today's labels unchanged | `StatusPagesSection.test.tsx:92` (`"Publicada"`), `:94` (`"Falha"`), `:96` (tls error text), `:98` (no "Emitindo certificado" anywhere). Detail: `StatusPageDetail.test.tsx:53-58` (published + public URL + preview link), `:76-80` (tls_failed) | ✅ PASS |

**Status**: ✅ All 15 criteria covered with `file:line` evidence asserting the spec-defined outcome. 0 spec-precision gaps.

---

## Edge Cases

- [x] **Two concurrent attach requests on the same domain-less page → exactly one `200`, one `409`, no lost update.** `internal/db/status_page_repository_test.go:473` (`TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins`). Verified this round to force **real** contention, not accidental sequencing: an explicit holder transaction takes the same `SELECT domain_id FROM status_pages WHERE id = $1 FOR UPDATE` on the same row (`:488`), performs the attach itself (`:495`), and stays uncommitted while a real `AttachDomain` call runs in a goroutine (`:506-510`); the test fails outright if that call returns within 300 ms (`:514`, "the row lock did not block it"), then asserts `ErrDomainAlreadyAttached` after the holder commits (`:531`) and that the holder's `domain_id`/`subdomain` survived (`:539`, `:542` - no lost update). Empirically confirmed by mutation M1 below.
- [x] **Attach collides with a hostname created via the legacy with-domain create path** → `409` from the partial unique index. `internal/db/status_pages_migration_test.go:42`, `internal/api/status_pages_handler_test.go:446`.
- [x] **DNS target never configured must not block attaching.** `web/src/features/status-pages/AttachDomainDrawer.test.tsx:48` ("mostra aviso ... sem bloquear o formulário"); backend `target: null` at `internal/api/instance_config_handler_test.go:95`.

---

## Discrimination Sensor

All mutations were injected into a throwaway `git worktree` (`git worktree add <scratch> HEAD`),
run there, then discarded with `git worktree remove --force`. No `git stash` was used. Every
mutation was re-run from scratch this round - no result was carried over from round 1 or 2.

| # | File:line (scratch copy) | Mutation | Test run | Killed? |
| - | ------------------------ | -------- | -------- | ------- |
| M1 | `internal/db/status_page_repository.go:110` | Removed `FOR UPDATE` from `SELECT domain_id FROM status_pages WHERE id = $1 FOR UPDATE` (the exact mutant that **survived** in round 2) | `go test -tags=integration -race -count=10 -run TestAttachDomain_ConcurrentAttachesOnSamePage_ExactlyOneWins ./internal/db` | ✅ Killed **10/10** - `status_page_repository_test.go:531: AttachDomain() error = <nil>, want ErrDomainAlreadyAttached` (lost update detected) |
| M2 | `internal/api/status_pages_handler.go:73` | Removed the both-or-neither guard (`if (req.Subdomain == "") != (req.DomainID == "")` → `if false`) | `go test -tags=integration -run TestCreateStatusPage ./internal/api` | ✅ Killed - `OnlySubdomainSet_422` got `500`, `OnlyDomainIDSet_422` got `201` with silently-dropped `domain_id` |
| M3 | `internal/api/public_status_preview_handler.go:59-79` | Reinstated the removed production gate (`if sp.State != "published" { http.NotFound }`) | `go test -tags=integration -run TestPublicStatusPreview ./internal/api` | ✅ Killed - 3 failures: `DraftPageWithDomain_200`, `DraftPageNoDomain_200`, `TLSFailedPage_200Unaffected` all got `404` |
| M4 | `web/src/features/status-pages/StatusPagesSection.tsx:107` | Collapsed the two labels back into one ambiguous label (`"Sem domínio configurado"` → `"Aguardando validação de DNS/certificado"`) | `npx vitest run src/features/status-pages/StatusPagesSection.test.tsx StatusPagesPage.test.tsx` | ✅ Killed - 2 failed (SPD-12 list label, SPD-01/SPD-12 create flow) |
| M5 | `StatusPagesSection.tsx:20` + `StatusPageDetail.tsx:12` | Removed both `publicUrl` null guards (`if (!domain_id \|\| !subdomain) return null`) | `npx vitest run src/features/status-pages/` | ✅ Killed - 2 failed (`https://null` rendered in both list and detail) |

**Sensor depth**: expanded (5 mutations - data-integrity/race path treated as critical)
**Result**: **5/5 killed** - ✅ PASS

**Stability re-check (no flake introduced by the round-2 fix)**:
`go test -tags=integration -race -count=20 -run TestAttachDomain ./internal/db` with production code
**intact** → `ok ... 16.182s`, **20/20 green**, zero race warnings.

**Isolation verified**: real-tree `git status --porcelain` before sensor work and after cleanup are
byte-identical (` M .specs/LESSONS.md`, ` M .specs/lessons.json`, ` M web/tsconfig.tsbuildinfo`,
`?? .specs/features/status-page-domain-attach/validation.md`). Both scratch worktrees removed;
`git worktree list` shows only the real tree.

---

## Gate Check

- **Gate commands** (tasks.md Gate Check Commands, Build level - all re-run by the Verifier):
  - `go build ./... && gofmt -l . && go vet ./...` → **clean** (build OK, no unformatted files, no vet findings)
  - `go test -tags=integration -p 1 -count=1 ./internal/db ./internal/api ./internal/cli ./internal/config` → **ok** for all 4 packages (db 2.26s, api 7.18s, cli 2.49s, config 0.19s)
  - `cd web && npm run build` → **built in 674ms**, 201 modules, `tsc -b` clean
  - `cd web && npm run test` → **42 files, 162 tests passed, 0 failed, 0 skipped**
- **Result**: 220 Go top-level tests passed + 162 frontend tests passed, **0 failed, 0 skipped**
- **Go test count per package (all passing, verified top-level `--- PASS` == static `func Test` count)**: db 55, api 137, cli 21, config 7 = **220**
- **Test count before feature** (`3f7a176`, same 4 packages): db 44, api 121, cli 20, config 5 = **190**
- **Delta (Go)**: **+30**, strictly non-decreasing
- **Frontend count**: 126 static `it(` cases pre-feature → **147** at HEAD (**+21**); runner reports 162 executed cases. Strictly non-decreasing (round 1: 157 runner → round 2: 162 runner)
- **Skipped tests**: none
- **Failures**: none

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes | ✅ |
| No scope creep | ✅ Pre-existing gaps (no FK/uniqueness mapping on the legacy with-domain `Create`) left untouched as the spec's Out of Scope requires |
| Matches patterns | ✅ `pgerrcode` mapping mirrors `domain_repository.go`; `writeRoles` reused verbatim; `Drawer` reused |
| Spec-anchored outcome check (asserted values match spec) | ✅ 15/15 |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ Every new route has happy + `404`/`409`/`422` + RBAC (`401`/`403`) coverage |
| Every test maps to a spec requirement - no unclaimed tests | ✅ The two "mutante #5" tests are explicitly labeled as guard-discrimination tests for the `publicUrl` null-safety guard, with the reason documented in-test |
| Documented guidelines followed | ✅ none in repo - strong defaults applied (no `AGENTS.md`/lint-config guideline file exists) |

**Deliberate divergence**: `AD-008` (preview no longer mirrors production's `published` gate) is
documented in-code at `internal/api/public_status_preview_handler.go:49-57`, replacing the now-false
"mirrors HostRouter's gate" comment - the risk called out in `design.md` Risks row 2 is closed.

---

## Findings (non-blocking)

### Minor 1 - `spec.md:102` still describes the abandoned race mechanism

`spec.md:35` was corrected in `c9ecc80` to say `SELECT ... FOR UPDATE`, but the **Edge Cases** section
still reads:

> `- IF two concurrent attach requests ... - covered by the conditional `UPDATE ... WHERE domain_id IS NULL` guard.`

- **Root cause**: the round-2 fix corrected only one of the two places where the spec described the guard.
- **Impact**: documentation only. The shipped mechanism is `SELECT ... FOR UPDATE`
  (`internal/db/status_page_repository.go:110`), it is correct, and it is now empirically proven by
  mutation M1. No AC, test, or behavior is affected. This is exactly the stale-description pattern that
  invited the round-2 regression, so it is worth a one-line edit.
- **Priority**: Minor (docs). Recommended fix: replace the trailing clause of `spec.md:102` with
  "covered by the `SELECT ... FOR UPDATE` row lock (design.md Tech Decisions)".

### Minor 2 - `spec.md` has 15 acceptance criteria but 14 `SPD-*` IDs

`SPD-07` is used for two distinct criteria (already-attached `409` and invalid-`domain_id` `422`), and
`SPD-05` is cited for two different things across `spec.md:22`/`:103` (attach uniqueness) and
`tasks.md` T5 (relaxed `Create`).

- **Impact**: bookkeeping only - every criterion is covered by evidence regardless. Flagged so the
  traceability table is not trusted as a complete count.
- **Priority**: Minor (docs).

Neither finding is a coverage, behavior, or discrimination gap, so neither blocks the PASS verdict.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| SPD-01 | Implementing | ✅ Verified |
| SPD-02 | Implementing | ✅ Verified |
| SPD-03 | Implementing | ✅ Verified |
| SPD-04 | Implementing | ✅ Verified |
| SPD-05 | Implementing | ✅ Verified |
| SPD-06 | Implementing | ✅ Verified |
| SPD-07 | Implementing | ✅ Verified |
| SPD-08 | Implementing | ✅ Verified |
| SPD-09 | Implementing | ✅ Verified |
| SPD-10 | Implementing | ✅ Verified |
| SPD-11 | Implementing | ✅ Verified |
| SPD-12 | Implementing | ✅ Verified |
| SPD-13 | Implementing | ✅ Verified |
| SPD-14 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 15/15 criteria (14 `SPD-*` IDs) matched the spec-defined outcome, 0 spec-precision gaps
**Sensor**: 5/5 mutations killed (expanded depth), including the round-2 survivor at `-race -count=10`
**Gate**: 220 Go + 162 frontend tests passed, 0 failed, 0 skipped; build/gofmt/vet/tsc clean
**Stability**: `-race -count=20` on the reworked concurrency test → 20/20, no flake

**What works**:
- Domain-less status page creation end to end (`201` with `null`/`null`/`draft`), schema, repository, handler, SPA form.
- Preview composes for any state including no-domain, `404` on unknown ID preserved; `AD-008` documented in-code.
- `PATCH /api/status-pages/{id}/domain` with all five distinguishable outcomes (`200`/`404`/`409` already-attached/`422` invalid/`409` duplicate pair) plus a genuinely contended concurrency proof.
- `GET /api/instance/dns-target` with configured and `null` cases; both new routes `writeRoles`-gated (viewer `403`, no session `401`) through the real `buildAdminRouter`.
- Four distinguishable UI states with mutual-exclusion assertions and the "Emitindo certificado" ambiguity gone; preview link always visible; `publicUrl` null guards proven discriminating.

**Issues found**: Minor 1 (`spec.md:102` stale mechanism description) and Minor 2 (AC/ID count mismatch) - documentation only, fixes noted above.

**Next steps**: Mark the feature done. Optionally apply the two one-line doc corrections in a `docs(spec)` commit; neither requires re-verification.
