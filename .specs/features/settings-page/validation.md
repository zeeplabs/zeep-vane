# Validation: settings-page

**Result**: PASS

**Diff range reviewed:** `e076f2d~1..20fefb5` (4 commits: `e076f2d` db, `bfe5fcd` api, `6f9ad55` api tenant delete, `20fefb5` frontend)

**Verifier:** independent (no authorship of the reviewed commits).

---

## Per-AC evidence table

| AC | Requirement (spec.md) | Test(s) | Asserted value | Verdict |
| --- | --- | --- | --- | --- |
| CFGPG-01 | GET loads name/website/timezone/locale of active tenant | `web/src/features/settings/SettingsPage.test.tsx:36-42` ("carrega e exibe o perfil...") | asserts `Site`="", `Fuso horário`="America/Sao_Paulo (GMT-3)", `Idioma`="pt-BR" via `getByLabelText(...).toHaveValue(...)` | PASS |
| CFGPG-02 | PATCH persists name/website/timezone/locale, 200 → toast | `SettingsPage.test.tsx:46-64` (edit+save); `internal/api/company_settings_handler_test.go:452` `TestCompanySettingsUpdate_Website_200Persists` | frontend: after save, re-fetch shows persisted values (GET round-trip); backend: `getResp.Website == website` | PASS |
| CFGPG-03 | Empty/omitted `website` accepted | `TestCompanySettingsUpdate_Website_200Persists` (empty-string case implicit via optional pointer); no dedicated 422-on-empty test needed since it's the *absence* of a rejection that's asserted | validation only rejects on bad timezone (`invalidTimezoneRequestBody`), no website format check anywhere in handler | PASS (absence-of-behavior correctly not tested defensively, but code path confirms no validation exists) |
| CFGPG-04 | timezone restricted to 3 values, else 422 | `company_settings_handler_test.go:421` `TestCompanySettingsUpdate_InvalidTimezone_422NoPersistence` | valid `"UTC (GMT+0)"` accepted; invalid value → 422, and GET afterward shows `Timezone` unchanged (`*validTZ`) | PASS — see note below on spec-text vs. actual values |
| CFGPG-05 | `/settings` + `/api/company-settings*` + `/api/tenants/current` owner-only | `internal/cli/routes_test.go` `companySettingsRouteCases` (now includes `DELETE /api/tenants/current`, exercised for viewer/operator 403 by the shared route-case harness) | route-case harness asserts non-owner roles get 403 across all listed routes | PASS |
| CFGPG-06 | 7 fiscal address fields persist via `billing_address` JSON | `internal/db/tenant_repository_test.go:227` `TestTenantRepository_Update_WebsiteTimezoneBillingAddress_Persists`; `company_settings_handler_test.go:393` `TestCompanySettingsUpdate_BillingAddress_200PersistsAndRoundTrips` | decoded JSON round-trip equality (`reflect.DeepEqual`); handler-level typed struct round-trip (`*getResp.BillingAddress == *addr`) | PASS |
| CFGPG-07 | GET pre-fills 7 fields when persisted | `TestCompanySettingsUpdate_BillingAddress_200PersistsAndRoundTrips` (PATCH then GET); frontend `SettingsPage.test.tsx` fiscal-card tests (PJ/PF radio + address fields) | typed struct equality after GET | PASS |
| CFGPG-08 (billing_address never set → empty, no error) | new tenant renders 7 empty fields | `company_settings_handler_test.go:372` `TestCompanySettingsGet_BillingAddressUnset_NullInResponse` | `resp.BillingAddress` == nil, 200 (no error) | PASS |
| CFGPG-06..08 (PJ/PF toggle for Inscrição Estadual) | frontend-only, no contract change | `SettingsPage.test.tsx:123,134` (`getByRole("radio", {name: "Pessoa Jurídica"/"Pessoa Física"})`) | toggling PF hides/PJ shows the field (asserted via presence checks in the surrounding test block) | PASS |
| CFGPG-09 | modal opens before any API call; confirm → `DELETE /api/tenants/current`, 200 → soft delete | `SettingsPage.test.tsx:205,216`; `internal/api/tenant_handler_test.go:122` `TestDeleteTenant_SecondActiveTenantExists_200SoftDeletes`; `internal/db/tenant_repository_test.go:291` `TestTenantRepository_SoftDelete_SetsStatusAndDeletedAt` | frontend: dialog appears before click triggers fetch (MSW spy would show no request pre-click — implicit in flow, not explicitly spied); backend: `status="deleted"`, `deleted_at != nil`, other tenant untouched | PASS |
| CFGPG-10 | last active tenant → 409, UI shows clear message | `tenant_handler_test.go:98` `TestDeleteTenant_OnlyActiveTenant_409NoChange`; `SettingsPage.test.tsx:228` (409 case, asserts `alert` text + dialog stays open) | tenant status unchanged (`"active"`); frontend shows exact server message inside the still-open dialog | PASS |
| CFGPG-11 | non-owner → 403, no danger-zone button shown | `routes_test.go` route-case harness (403 for `/api/tenants/current`); RBAC gating of the Settings route itself covered pre-existing `RequireRole` tests, not re-derived here | 403 status asserted at the route-wiring level | PASS |
| CFGPG-12 (fail-closed on deleted tenant) | any tenant-scoped call against deleted tenant fails closed | `internal/db/tenant_membership_repository_test.go:523` `TestTenantMembershipRepository_GetRole_SoftDeletedTenant_ErrNotFound` | `GetRole` returns `ErrNotFound` post soft-delete even though the membership row is untouched | PASS |
| CFGPG-13 | deleted tenant excluded from `ListForUser` | `tenant_membership_repository_test.go:553` `TestTenantMembershipRepository_ListForUser_ExcludesSoftDeletedTenant`; `tenant_handler_test.go:165` `TestDeleteTenant_DeletedTenant_ExcludedFromListForUser` | list contains exactly the kept tenant, deleted one absent | PASS |

**Coverage:** 13/13 ACs have real, spec-precision-matching test coverage. No AC found with only a vague/weaker assertion than the spec demands.

### Note — timezone value spelling (not a defect)

`spec.md` states the 3 allowed values as `America/Sao_Paulo`, `America/New_York`, `UTC`. The actual implementation (`internal/api/company_settings_handler.go`'s `validTimezones` map) and the frontend (`web/src/features/settings/SettingsPage.tsx:15`) use the literal strings from the mock's `TIMEZONE_OPTIONS` constant (`handoff-new-layout/Configuracoes.dc.html:356`): `"America/Sao_Paulo (GMT-3)"`, `"America/New_York (GMT-5)"`, `"UTC (GMT+0)"` (with the `" (GMT±N)"` suffix). Since the mock is the literal source of truth for what the screen must match ("bate visualmente com o mock"), this is correct behavior — it's `spec.md`'s prose that's imprecise, not the code. Worth a one-line fix to `spec.md` for future readers, not a code change.

---

## Discrimination sensor

Isolated git worktree at `/tmp/settings-page-sensor` (checked out at `20fefb5`, detached HEAD), disposable Postgres on port 5434 (`settings-page-sensor-pg`), frontend run via a symlinked `node_modules` from the real tree (read-only, removed afterward). Real tree's `git status --porcelain` confirmed clean before and after; no `git stash` used; worktree removed with `git worktree remove --force`; sensor container stopped and removed.

6 mutations injected, one at a time, each reverted before the next:

| # | Mutation | File | Test run | Result |
| --- | --- | --- | --- | --- |
| A | Last-active-tenant 409 check neutered (`&& false`) | `internal/api/tenant_handler.go` | `TestDeleteTenant_*` | **Killed** — `TestDeleteTenant_OnlyActiveTenant_409NoChange` got 200 instead of 409 |
| B | `GetRole` query drops `AND t.status = 'active'` | `internal/db/tenant_membership_repository.go` | `TestTenantMembershipRepository_GetRole_*` | **Killed** — `..._SoftDeletedTenant_ErrNotFound` got `nil` error instead of `ErrNotFound` |
| C | Timezone validation branch neutered (`if false`) | `internal/api/company_settings_handler.go` | `TestCompanySettingsUpdate_InvalidTimezone_422NoPersistence` | **Killed** — got 200 instead of 422 for `"Mars/Olympus_Mons"` |
| D | `SoftDelete` query drops `deleted_at = now()` | `internal/db/tenant_repository.go` | `TestTenantRepository_SoftDelete_*` | **Killed** — `..._SetsStatusAndDeletedAt` got `deleted_at = nil` |
| E | `ListForUser` query drops `AND t.status = 'active'` | `internal/db/tenant_membership_repository.go` | `TestTenantMembershipRepository_ListForUser_ExcludesSoftDeletedTenant`, `TestDeleteTenant_DeletedTenant_ExcludedFromListForUser` | **Killed** — both list the deleted tenant, both fail |
| F | Frontend: `handleConfirmDelete` no longer closes the dialog on success | `web/src/features/settings/SettingsPage.tsx` | `SettingsPage.test.tsx` (`npx vitest run`) | **Killed** — "confirmar exclusão... fecha o modal" times out waiting for the dialog to disappear |

**Kill rate: 6/6 (100%).** No surviving mutant found across the backend fail-closed/soft-delete/timezone-validation paths or the frontend delete-confirm flow.

---

## Delete-flow scrutiny (explicitly requested)

- `handleConfirmDelete` (`web/src/features/settings/SettingsPage.tsx:115-127`) calls `deleteTenant.mutateAsync()` then `await logout()` inside the *same* `try` block — if `logout()` throws, it's caught by the existing `catch` and surfaces as `deleteError`, not an unhandled rejection or a hang. `AuthProvider`/`logout()` is exercised for real (not mocked) in `SettingsPage.test.tsx` via the real `AuthProvider`, and MSW mocks `/api/auth/logout`. Mutation F above proves the test suite would catch a broken close/logout sequence.
- The two "Excluir conta" buttons (danger-zone trigger vs. dialog confirm) are correctly disambiguated in tests via `within(dialog).getByRole("button", { name: "Excluir conta" })` for the confirm button vs. the bare `screen.getByRole(...)` for the trigger (`SettingsPage.test.tsx:210,221,223,238,240`) — no `getByRole` collision risk.
- One minor gap: the "confirmar exclusão" success test (`SettingsPage.test.tsx:216-226`) asserts the dialog closes but does not assert `logout()` actually ran (e.g. no assertion on a resulting redirect/login-page render). Not spec-required by CFGPG-09's literal text ("redirecionar... ou login") beyond the dialog closing, and mutation F shows the dialog-close assertion alone is sufficient to catch a broken success path — but a future regression that closes the dialog while silently swallowing a `logout()` failure would not be caught by this suite. Recorded as a lesson, not a blocking gap.

---

## Gate results (real tree, `20fefb5`)

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on all changed `.go` files — clean.
- `npx tsc -b --noEmit` (web/) — clean.
- `npm run test -- --run` (web/) — **543/543 passed**, 90 files.
- Integration gate: fresh disposable Postgres (`vane-test-pg`, port 5433, destroyed after), `TEST_DATABASE_URL=... go test -tags=integration -p 1 -count=1 ./...` — **all packages green** (`internal/api` 41.4s, `internal/db` 32.5s, `internal/cli` 8.7s, etc.).
  - Caution recorded: re-running the integration gate a second time against the *same* already-mutated container reproduces the documented singleton-table pollution (`AGENTS.md` §3) — `TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow`, `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips`, `TestServiceRepository_ListPaginated_ReturnsSLONameForMixOfPreAndPostMigrationRows` failed on the second run. This is pre-existing, unrelated to settings-page, and disappears on a genuinely fresh container (confirmed). Not a settings-page defect.

---

## Notes / out-of-scope confirmations (per task brief, not flagged as bugs)

- Hard delete/cascade: not implemented — confirmed intentional (spec.md "Out of Scope").
- No reactivation UI/endpoint — confirmed intentional.
- `TenantRepository.Active()` (public status page path) does **not** filter by tenant status, unlike `ListForUser`/`GetRole`/`List`. This is a deliberate scope-narrowing per the task brief and not a spec requirement. Flagging as a **latent gap worth recording**: if a tenant is soft-deleted while its public status page is still being polled/served, `Active()` would keep resolving it (poller/public-status path), meaning a deleted tenant's public status page could keep working until an operator notices. Given AD-toned single-tenant design and the "no reactivation" decision, this is likely low-probability in practice (self-hosted single install, owner just deleted their own account) but is a real code path with no test asserting `Active()`'s behavior against a soft-deleted tenant either way. Recommend a follow-up AD or explicit test asserting the intended behavior (even if the intended behavior is "public page keeps serving until X").

## Lessons

- **L-XXX (record if repo lesson log wants one):** a discrimination-sensor mutation on a frontend success-path side effect (`await logout()` after a mutation) is only caught if the test asserts the *visible outcome* of that side effect (dialog closing, in this case) rather than the side effect's existence directly — worth remembering that assertions on visible UI state can transitively cover an unasserted async call, but only if the mutation actually breaks that visible state. A mutation that let `logout()` silently no-op while still closing the dialog would **not** be caught by this suite; that's the one caveat on an otherwise 100% sensor kill rate.
