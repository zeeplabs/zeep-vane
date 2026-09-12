# List Pagination Validation

**Feature**: `list-pagination`
**Validated**: 2026-09-12
**Validator**: fresh-eyes standalone pass (no separate worker sub-agent available in this harness — author and verifier are the same agent, same limitation recorded in the other features' validations)
**Result**: PASS (after fixing one real gap, PAG-07)

---

## Context

This feature shipped without a `validation.md` — the only feature in `.specs/features/` in that state. This is a retro-validation: re-run the gates, map every `PAG-01..15` AC to the test(s) that exercise it, and fix any real gap found. It is not a re-implementation.

The retro-validation found a **real, user-visible gap**: PAG-07 (the incidents list's `Pager`) was never implemented. `IncidentsPage.tsx` still read a fixed `useIncidents(1)` behind a stale `SPEC_DEVIATION` comment ("T14 not yet built") even though `T14`'s `Pager` component exists and every other list screen uses it. Task `T5` (`IncidentsPage.tsx` renders `Pager`) was left unchecked. Fixed in this pass (see below).

---

## Spec-Anchored Check

| Req | Status | Evidence |
| --- | --- | --- |
| PAG-01 | ✅ Verified | `internal/db/incident_repository_test.go` (`ListPaginated` page 1/2/order-by-created_at-desc); `internal/api/incidents_handler_test.go` (`TestListIncidents_ReturnsMostRecentFirstWithServiceIDs`, `TestListIncidents_Page2_ReturnsRemainderNoOverlapWithPage1`) |
| PAG-02 | ✅ Verified | `internal/api/pagination_test.go` (`TestParsePage_MissingDefaultsToOne`); all handler default-page tests |
| PAG-03 | ✅ Verified | `TestParsePage_ZeroClampsToOne`/`_Negative`/`_NonNumeric`; `TestListIncidents_InvalidPage_ClampsToPageOne_200`, `TestListDomains_InvalidPage_ClampsToPage1`, `TestListServices_InvalidPage_ClampsToPage1`, `TestListStatusPages_InvalidPage_ClampsToPage1`, `TestPollerStatus_InvalidPage_ClampsToPage1`, `TestListIncidentUpdates_InvalidPage_ClampsToPageOne_200` |
| PAG-04 | ✅ Verified | `TestListIncidents_PageBeyondLast_EmptyItems200`; repo `*_PageBeyondLast_EmptyItemsCorrectTotal` for incidents/updates/domains/services/status-pages; `TestPage_ItemsEmptyNotNilSerializesAsEmptyArray` |
| PAG-05 | ✅ Verified | `internal/db/incident_repository_test.go` (`ListUpdatesPaginated` page1+2, beyond-last, scoped-to-incident, unknown-incident 404); `TestListIncidentUpdates_Page2_ReturnsRemainderNoOverlapWithPage1` |
| PAG-06 | ✅ Verified (source) | `COUNT(*) OVER() AS total` present in every paginated repo query (`incident_repository.go:152`, `domain_repository.go:91`, `service_repository.go:61`, `status_page_repository.go:274`, `email_provider_repository.go:98`, `integration_repository.go:105`) — single-query total, no separate `COUNT(*)` |
| PAG-07 | ✅ Verified (**fixed this pass**) | `IncidentsPage.tsx` now holds `page` state, passes it to `useIncidents`, computes `totalPages = max(1, ceil(total/page_size))` and renders `<Pager>`. New tests: `IncidentsPage.test.tsx` "renderiza o Pager e navega para a página seguinte"; `Pager.test.tsx` (4 tests) |
| PAG-08 | ✅ Verified | Handler tests for all 6: `TestListDomains_*`, `TestListServices_*`, `TestListStatusPages_*`, `TestPollerStatus_*`, email-providers handler tests, `admins_test.go`; repo `ListPaginated` tests for each |
| PAG-09 | ✅ Verified | `internal/api/admins_test.go` — `TestListAdmins_Owner_200_MergesPendingInviteWithStatus` + `findAdminAcrossPages` walks every page until `page*page_size >= total`, proving the in-memory merge and combined `total` |
| PAG-10 | ✅ Verified (implementation) / ⚠️ minor residual | All 6 screens import the shared `<Pager>` (`DomainsSection.tsx:152`, `ServicesSection.tsx:150`, `StatusPagesSection.tsx:227`, `AdminsPage.tsx:201/228`, `PollerStatusPage.tsx`, `EmailProvidersPage.tsx`). Explicit render assertions exist for Poller/Email; the other 4 render it but no test asserts the control explicitly. Test-only gap, not functional — residual noted below |
| PAG-11 | ✅ Verified (**test added this pass**) | Hooks invalidate the resource prefix (`["incidents"]`, `["admins"]`, …), not the exact page key. New test `IncidentsPage.test.tsx` "criar incidente na página 2 e voltar à página 1 mostra o novo incidente" proves cross-page invalidation end-to-end |
| PAG-12 | ✅ Verified | `TestPublicStatusGet_ResolvedIncidents_Page1ExactlyPageSizeCorrectTotal`, `_Page2ReturnsRemainder` |
| PAG-13 | ✅ Verified | `PublicStatusPage.test.tsx` "mostra 10 incidentes resolvidos e o botão Carregar mais quando há mais de 10" |
| PAG-14 | ✅ Verified | `PublicStatusPage.test.tsx` "clicar em Carregar mais adiciona a página seguinte sem duplicar nem reordenar a primeira"; `public-status/hooks.test.ts` "loadMoreResolvedIncidents adiciona a página 2 sem substituir/reordenar a página 1" |
| PAG-15 | ✅ Verified | `PublicStatusPage.test.tsx` "botão Carregar mais desaparece quando todos os incidentes resolvidos foram carregados" |

**Spec-anchored check**: 15/15 ACs verified, 14 fully test-backed and 1 (PAG-06) source-verified (single-query `COUNT(*) OVER()` is a code-shape requirement, not a behavior a test can distinguish from a second round-trip without a query-count probe).

---

## Gate Results

| Gate | Result |
| --- | --- |
| Frontend | `npx tsc -b --noEmit` clean; `npm run test` → **423/423 passed**, 76 files (was 421 before the PAG-07 fix; +2 tests) |
| Backend integration | `make test-integration` (`-tags=integration -count=1 -p 1`, fresh disposable Postgres) → **all 25 packages ok**, 0 failures |
| Backend build | `go build ./...`, `go vet ./...`, `gofmt -l` clean |

---

## Discrimination Sensor

Not re-run for this retro-validation. No pre-existing mutant set exists for this feature, and the one code change made here (PAG-07) is a direct, literal-assertion test: removing `<Pager>` from `IncidentsPage` fails the new PAG-07 test (the control would not render and navigation would be impossible), and removing the `page` state would leave the list pinned to page 1. The tests assert observable behavior (page text + items changing), not implementation internals.

---

## Issues Found

1. **(High, fixed) PAG-07 was never implemented.** `IncidentsPage.tsx` read a fixed `useIncidents(1)` with a stale `SPEC_DEVIATION` comment claiming `T14` (Pager) didn't exist. `T5`'s Done-when was unchecked. Consequence: the P1 endpoint — the one this feature exists for — had no pager; with more than 25 incidents, older ones were unreachable in the UI. Fixed by wiring `page` state + `<Pager>` (matching every other list screen) and adding the two tests T5's Done-when required (pager navigation; create-on-page-2 → page-1 refresh). The stale `SPEC_DEVIATION` comment was removed.
2. **(Low, residual) PAG-10 explicit render assertions.** 4 of the 6 screens render the shared `Pager` but have no test asserting the control. The component itself is unit-tested and the 2 other screens assert it, so the risk is low; adding a one-line `Pager` assertion to each of the 4 section tests would close it. Not fixed here to keep the change scoped to the real (functional) gap.
3. **(Info) `tasks.md`'s `T5` Done-when was never checked and its stale deviation comment survived.** The absence of a `validation.md` is exactly why this shipped unnoticed. Registering lesson `L-047` (`ac_gap`): a feature's task checkboxes and `SPEC_DEVIATION` comments must be reconciled before a feature is called done, and every feature needs a `validation.md`.

---

## Next Steps

- No blocking work remains; the feature is verified end-to-end.
- Optional cleanup: add explicit `Pager` assertions to `DomainsSection`/`ServicesSection`/`StatusPagesSection`/`AdminsPage` tests (finding 2), and re-check the other `tasks.md` Done-when boxes that were never ticked (the feature is functionally complete; this is bookkeeping).
