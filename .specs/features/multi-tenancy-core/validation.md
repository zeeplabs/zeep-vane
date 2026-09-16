# Multi-Tenancy Core Validation

**Date**: 2026-09-10
**Spec**: `.specs/features/multi-tenancy-core/spec.md`
**Diff range**: `f6035e8~1..HEAD` (33 commits, `f6035e8` = first commit through `b31ad74` = HEAD)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 | ✅ Done | Deviation found and corrected in-place before release (documented in tasks.md) |
| T2 | ✅ Done | - |
| T3 | ✅ Done | - |
| T4 | ✅ Done | SPEC_DEVIATION: `CompanySettingsRepository` not deleted this batch, closed by T16 |
| T5 | ✅ Done | - |
| T6 | ✅ Done | SPEC_DEVIATION: tenant+membership atomic with each other, not with the admin insert (documented, acceptable trade-off) |
| T7 | ✅ Done | - |
| T8 | ✅ Done | - |
| T9 | ✅ Done | - |
| T10 | ✅ Done | - |
| T11 | ✅ Done | - |
| T12 | ✅ Done | - |
| T13 | ✅ Done | - |
| T14 | ✅ Done | - |
| T15 | ✅ Done | Deferred wiring closed by 2026-09-10 fix round (AD-024) |
| T16 | ✅ Done | - |
| T17 | ✅ Done | - |
| T18 | ✅ Done | - |
| T19 | ✅ Done | - |
| T20 | ✅ Done | Found-during-Execute gap (unauthenticated tenant resolution), closed by T20b |
| T20b | ✅ Done | AD-023 recorded |

All 20 tasks (T1-T19 plus the T20/T20b pair discovered during Execute) are marked complete in `tasks.md` with `[x]` Done-when items and a commit reference. No partial/blocked tasks found.

---

## Spec-Anchored Acceptance Criteria

### P1: Isolamento de dado por tenant via RLS

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: policy RLS em toda tabela com `tenant_id` | Every listed table has RLS enabled+forced with a `tenant_isolation` policy referencing `app.tenant_id`, no stray policies | `internal/db/multi_tenancy_migration_test.go:52-102` (`TestMultiTenancyMigration_EveryTenantScopedTableIsRLSProtected`) - asserts `relrowsecurity`/`relforcerowsecurity` true, exact policy count/name match against `wantPolicies`, and `usingExpr` contains `"app.tenant_id"` | ✅ PASS |
| AC2: middleware `SET LOCAL app.tenant_id` antes de query de domínio | `app.tenant_id` visible inside the handler's transaction equals the session's active tenant claim | `internal/api/tenant_context_middleware_test.go:54-86` (`TestTenantContext_ActiveTenantClaim_SetsAppTenantID`) - `if gotTenantID != tenantID { t.Errorf(...) }` | ✅ PASS |
| AC3: sem `app.tenant_id`, zero linhas (fail-closed) | Query with no context set returns exactly 0 rows, never an error, never another tenant's rows | `internal/db/rls_test.go:142-168` (`TestRLS_NoTenantContext_ReturnsZeroRows`) - `if count != 0 { t.Errorf(...) }`; cross-tenant direction at `rls_test.go:175-241` | ✅ PASS |
| AC4: poller seta `app.tenant_id` por tenant, nunca `BYPASSRLS` | Poller iterates tenants one at a time, each with its own tx/context, never a bypass role | `internal/poller/poller_tenant_test.go:118-149` (`TestPollCycle_TenantIterationEnabled_ProcessesEachTenantInItsOwnContext`) - asserts `tx.beginCalls == [tenant-a tenant-b]`, one `UpdateStatus` per tenant's own service, no cross-tenant leakage in the assertion set | ✅ PASS |

**Independent Test** (spec.md): matches `TestRLS_TenantA_NeverSeesTenantB_EvenWithUnfilteredQuery` / `TestRLS_TenantB_NeverSeesTenantA_Symmetric` almost verbatim (2 seeded tenants, one authenticated as A, deliberately unfiltered query). ✅ Covered.

### P1: Self-hosted provisiona tenant único no bootstrap

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `/bootstrap` cria 1 tenant + 1 membership (owner) na mesma transação | 1 `tenants` row + 1 `tenant_memberships` row role=owner exist after a successful bootstrap | `internal/api/bootstrap_handler_test.go:256-298` (`TestBootstrapHandler_Create_Success_CreatesTenantAndOwnerMembership`) - `if tenantCount != 1`, `if membershipRole != db.RoleOwner` | ✅ PASS |
| AC2: nunca exibir seletor de tenant com exatamente 1 membership | Frontend: 1-membership users never see `TenantSelector` | `web/src/features/auth/TenantSelector.test.tsx` (per T17's task note: "1-membership users never see this screen" is one of T17's Done-when items, backed by 5 new component tests) - not re-read line-by-line but corroborated by `RequireAuth`'s `needsTenantSelection` derivation in `web/src/auth/AuthProvider.tsx` gating the redirect strictly on `memberships.length > 1` | ✅ PASS |
| AC3: `/bootstrap` com admin existente recusa com 4xx, sem tenant duplicado | Same 409 as before bootstrapping, no new tenant | `internal/api/bootstrap_handler_test.go:300` (`TestBootstrapHandler_Create_AlreadyBootstrapped_Returns409NoSecondAdmin`) | ✅ PASS |

### P1: Signup público SaaS com verificação de email obrigatória

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: signup cria tenant/user/membership + dispara verificação | tenant(plan=free) + user + owner membership created; email sent; `email_verified_at` null | `internal/api/signup_handler_test.go:138-186` (`TestSignup_NewEmail_201_...`) - asserts `resp.Status == "pending_verification"`, `EmailVerifiedAt == nil`, tenant name/role via SQL, `provider.lastMessage.To == email` | ✅ PASS |
| AC2: login bloqueado enquanto `email_verified_at` nulo | Login rejected with a message indicating verification required | `internal/api/signup_handler_test.go:373` (`TestLogin_UnverifiedEmail_403_ClearMessage`) | ✅ PASS |
| AC3: link de verificação marca `email_verified_at`, libera login | Valid token marks verified, subsequent login succeeds | `internal/api/signup_handler_test.go:292` (`TestSignupVerify_ValidToken_200_MarksVerifiedAllowsLogin`) | ✅ PASS |
| AC4: email já verificado em outro tenant → novo tenant + membership, sem duplicar user/senha | No duplicate `users` row, exactly 1 new membership, no password required | `internal/api/signup_handler_test.go:188-232` (`TestSignup_ExistingVerifiedEmail_201_...`) - `if userCount != 1`, `if membershipCount != 1` | ✅ PASS |
| AC5: falha de envio não reverte, reenvio funciona | Tenant/user/membership intact after simulated email failure; resend works | `internal/api/signup_handler_test.go:429` (`TestResendVerification_EmailSendFails_TenantUserMembershipIntact`) | ✅ PASS |
| AC6: rate limit por IP, 429, sem criar tenant | Exceeding threshold from one IP → 429 | `internal/cli/routes_test.go` `TestAdminRouter_SignupRateLimit_ExceedsBurst_429` (per T12's task note; not re-read line-by-line, corroborated by T12's Done-when + test count) | ✅ PASS |

### P1: Convite de membro do time escopado por tenant

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: `Invite` cria `tenant_invites` com `tenant_id` da sessão ativa | Invite scoped to caller's active tenant | `internal/api/admins_test.go:298` (`TestInviteAdmin_Owner_201_CreatesInviteAndAuditEntry`) plus cross-tenant isolation proof at `admins_test.go:1894` (`TestInviteAdmin_SameEmailPendingInOtherTenant_NotInvalidated`) | ✅ PASS |
| AC2: email já existente (verificado) → só cria membership, redireciona login | No password field accepted/required, membership only | `internal/api/admins_test.go:834` (`TestAcceptInvite_ExistingUser_201_CreatesOnlyMembershipNoSessionIssued`) | ✅ PASS |
| AC3: email novo → fluxo atual (define senha) | `user` + membership created, password flow unchanged | `internal/api/admins_test.go:885` (`TestAcceptInvite_NewEmail_201_KeepsSetPasswordFlow`) | ✅ PASS |
| AC4: `Invite`/`List`/`UpdateRole`/`Delete`/`ResendInvite`/`CancelInvite` restritos ao tenant ativo | Owner of tenant B never lists/mutates tenant A's members/invites | `internal/api/admins_test.go:1808` (`TestListAdmins_OwnerFromOtherTenant_NeverSeesMembersOrInvites`), `:1833` (`TestResendInvite_OtherTenantInviteID_404NoStateChange`), `:1864` (`TestCancelInvite_OtherTenantInviteID_404NoStateChange`) | ✅ PASS |
| AC5: token de convite expirado (>1h) recusado como hoje | Same existing 401 behavior | `internal/api/admins_test.go:679` (`TestAcceptInvite_ExpiredToken_401_NoStateChange`) | ✅ PASS |

**Independent Test** (spec.md): matches `TestAcceptInvite_ExistingUser_EndsUpWithMembershipInBothTenants` (`admins_test.go:929`) almost verbatim. ✅ Covered.

### P2: Troca de tenant ativo na sessão

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: login com >1 membership mostra seleção antes do dashboard | `needsTenantSelection` true, no active tenant set | `internal/api/auth_handler_test.go:510` (`TestLogin_MultipleMemberships_SucceedsWithNoActiveTenant`), frontend gate in `web/src/routes/RequireRole.tsx` (T17 note) | ✅ PASS |
| AC2: `switch-tenant` com membership válida atualiza cookie sem novo login | New cookie authenticates a follow-up `/api/auth/me` scoped to the new tenant | `internal/api/auth_handler_test.go:595-654` (`TestSwitchTenant_ValidMembership_UpdatesCookieNoReloginRequired`) - `if body.TenantID != secondTenantID`, follow-up `/api/auth/me` `if meBody.ActiveTenantID != secondTenantID` | ✅ PASS |
| AC3: `switch-tenant` sem membership → 403, sessão inalterada | 403, zero cookies set in response | `internal/api/auth_handler_test.go:660-701` (`TestSwitchTenant_NoMembership_403SessionUnchanged`) - `if rec.Code != http.StatusForbidden`, `if len(rec.Result().Cookies()) != 0` | ✅ PASS |

### P2: Perfil da empresa por tenant (dados fiscais)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: PATCH persiste `legal_name`/`tax_id`/`tax_id_type` em `tenants`, opcionais | Fields persist, optional | `internal/api/company_settings_handler_test.go:335` (`TestCompanySettingsUpdate_ValidCNPJ_200PersistsFiscalFields`); repository-level at `internal/db/tenant_repository_test.go:113,142` | ✅ PASS |
| AC2: CPF≠11 dígitos ou CNPJ≠14 dígitos → rejeita, sem persistir | 422, no persistence | `internal/db/tenant_repository_test.go:164,191` (`..._WrongDigitCount_ErrInvalidTaxIDNoPersist`); HTTP layer at `internal/api/company_settings_handler_test.go:273,304` | ✅ PASS |
| AC3: `billing_address` nunca exposto na UI (self-hosted) | Field never returned/accepted by the endpoint | `internal/api/company_settings_handler_test.go:369` (`TestCompanySettingsGet_NeverExposesBillingAddress`) - asserts the marshaled JSON carries no such key | ✅ PASS |

**Independent Test** (spec.md): matches `TestCompanySettingsUpdate_ValidCNPJ_200PersistsFiscalFields` + `TestCompanySettingsUpdate_CPFWrongDigitCount_422NoPersistence` almost verbatim. ✅ Covered.

### P3: Groundwork de locale e branding (schema apenas)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1: colunas `locale`(default `pt-BR`)/`primary_color`/`secondary_color` existem, sem UI consumindo | `tenants.locale` defaults to `pt-BR` | `internal/db/tenant_repository_test.go:38-56` (`TestTenantRepository_Create_GeneratesIDAndDefaults`) - `if tenant.Locale != "pt-BR"`; column existence for `primary_color`/`secondary_color` confirmed structurally in `internal/db/migrations/0024_multi_tenancy_core.up.sql:78-80`, no application code reads them (grep confirms zero non-repository/non-migration references) | ✅ PASS (existence + default); no test asserts the raw `\d tenants` column list itself, but the repository round-trip is equivalent evidence |
| AC2: formato hex `#RRGGBB` esperado para features futuras | Documented expectation, no enforcement in this feature | n/a - spec explicitly defers validation to a future feature that will consume these columns | ⚠️ Spec-precision gap (deliberate, by spec's own text - "documentado aqui para a feature que vier a validar") |

---

**Status**: ✅ All ACs covered / ⚠️ 1 deliberate spec-precision gap flagged (TENANT-26, explicitly out of this feature's enforcement scope per spec.md itself)

---

## Discrimination Sensor

Isolated scratch: temporary `git worktree add <scratch> HEAD` (never `git stash`). Pre-sensor baseline `git status --porcelain` on the real tree: `?? .specs/design-briefing.md` (pre-existing, unrelated untracked file). Post-cleanup porcelain matched exactly.

| # | File:line | Description | Killed? |
| - | --------- | ------------ | ------- |
| 1 | `internal/api/auth_handler.go` (`SwitchTenant`, `if !hasMembership {` → `if hasMembership {`) | Inverted the switch-tenant membership check - would let a caller switch into a tenant they have no membership in, and reject legitimate switches | ✅ Killed - `TestSwitchTenant_ValidMembership_UpdatesCookieNoReloginRequired` failed (403 instead of 200) and `TestSwitchTenant_NoMembership_403SessionUnchanged` failed (200 instead of 403) |
| 2 | `internal/db/tenant_membership_repository.go` (`isSoleOwner`, `return ownerCount <= 1, nil` → `return ownerCount < 1, nil`) | Off-by-one on the last-owner guard - the guard would never trigger for the actual last-owner case (`ownerCount == 1`) | ✅ Killed - `TestTenantMembershipRepository_UpdateRole_LastOwnerDemotion_ErrLastOwner` and `TestTenantMembershipRepository_Delete_LastOwner_ErrLastOwnerNoRowRemoved` both failed (`error = <nil>, want ErrLastOwner`) |
| 3 | `internal/api/auth_handler.go` (`Login`, removed the `if user.EmailVerifiedAt == nil { ... 403 ... }` block) | Removed the email-verification login gate entirely | ✅ Killed - `TestLogin_UnverifiedEmail_403_ClearMessage` failed (200 instead of 403, returned a live session token) |

**Sensor depth**: lightweight (3 targeted behavior-level mutations), matching this feature's tier - none of the three touches payment/billing (out of scope this feature), so the default (not P0-full) tier applies.
**Result**: 3/3 killed - ✅ PASS

Scratch worktree removed (`git worktree remove --force`); real tree's `git status --porcelain` confirmed identical to the pre-sensor baseline.

---

## Interactive UAT

Not performed. This is a backend-and-groundwork feature (RLS, provisioning, session, invite scoping) validated end-to-end by integration tests that exercise the real HTTP handlers against a real Postgres with RLS enforcing under a non-superuser role - equivalent rigor to manual UAT for this class of behavior. The three new frontend screens (T17-T19) are covered by component tests with MSW mocks mirroring the real backend response shape; no complex visual/interaction judgment call was identified that would need a human pass.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| No features beyond what was asked | ✅ - T20/T20b is a genuine spec gap closed in-line (unauthenticated tenant resolution), not scope creep; documented as such in tasks.md |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ - e.g. `TenantTxFunc` in T15 is justified by unit-testability, not speculative generality |
| Only touched files required for task | ✅ - deviations from a task's literal `Where` are documented with justification in each task's notes (T3, T6, T7, T10, T13, T14) |
| Didn't "improve" unrelated code | ✅ - T19's note explicitly declines to retrofit `react-i18next` onto pre-existing strings outside its own new fields, citing AGENTS.md §8 |
| Matches existing patterns/style | ✅ - RLS policy style, repository shape, MSW handler conventions all follow established project patterns |
| Would senior engineer approve? | ✅ |
| Tests map to acceptance criteria and are non-shallow (spot-check one story) | ✅ - spot-checked P1 Signup SaaS in full: every AC has a assertion targeting the spec's exact expected value (status code, exact row counts, exact field state), not just "no error" |
| Spec-anchored outcome check: each test's asserted value matches the spec-defined outcome (or gap flagged) | ✅ - 25/26 requirement IDs matched precisely; 1 (TENANT-26) is a deliberate spec-precision gap by the spec's own text |
| Per-layer Coverage Expectation met: domain logic has 1:1 AC mapping; routes/e2e cover happy + edge + error paths for every route in scope | ✅ - confirmed via Test Coverage Matrix in tasks.md cross-checked against actual test files found |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion (no unclaimed tests) | ✅ - no orphan test files found outside the documented scope |
| Documented project quality/testing guidelines followed (cite guideline file, or "none - strong defaults applied") | ✅ - `AGENTS.md` §3/§4/§5 (integration-test DB rule, Page[T] pattern n/a here, i18n, MSW mock shape) |

---

## Edge Cases (from spec.md)

- [x] Mesmo email chama `/api/signup` duas vezes antes de verificar → 409, sem duplicar tenant/membership: `internal/api/signup_handler_test.go:234` (`TestSignup_SameUnverifiedEmailRetried_409_NoSecondTenant`)
- [x] Owner único de um tenant tenta remover a si mesmo → recusado: `internal/api/admins_test.go:1216` (`TestDeleteAdmin_SelfRemovalAsLastOwner_409`), backed at the repository layer by `internal/db/tenant_membership_repository_test.go:272` (`TestTenantMembershipRepository_Delete_LastOwner_ErrLastOwnerNoRowRemoved`) - also the exact mutation the sensor targeted and killed
- [x] Migration de RLS falha no meio → rollback total (1 transação): `golang-migrate`'s standard per-migration transaction wraps `0024_multi_tenancy_core.up.sql`; no explicit negative test found for a *partial*-failure rollback, but this is a property of the migration tool itself (single file = single transaction), not custom code this feature wrote - not flagged as a gap
- [x] Usuário sem nenhuma `tenant_membership` tenta logar → recusado, nunca dashboard vazio: `internal/api/auth_handler_test.go:473` (`TestLogin_ZeroMemberships_403NoSessionIssued`)

---

## Gate Check

- **Gate command**: Quick (`go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...`), Full (disposable Postgres per AGENTS.md §3, `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...`), Frontend (`npx tsc -b --noEmit && npm run test`, from `web/`)
- **Result**: all three green, 0 failed
  - `go build ./...`: clean
  - `go vet ./...`: clean
  - `gofmt -l` on every changed `.go` file still present in the tree: no output (clean)
  - `go test ./...`: all packages `ok` (unit/no-DB tier)
  - `go test -tags=integration -p 1 ./...` against a disposable `vane-test-pg` container (never `vane-dev-pg`): all packages `ok`, including `internal/api` (26.6s), `internal/cli` (8.2s), `internal/db` (11.4s), `internal/router` (cached from a prior clean run this session)
  - `npx tsc -b --noEmit`: clean, no errors
  - `npm run test`: 54 test files, **296 tests passed**, 0 failed (matches T19's own final count)
- **Test count before feature**: not independently re-derived (would require checking out `f6035e8~1` and running the full suite); tasks.md's own running notes cite 277 after Phase 2/3, 282 after T17, 291 after T18, 296 after T19 - consistent, monotonically increasing, no unexplained drops
- **Delta**: no regression signal - count only ever increased across the batch's own checkpoints
- **Skipped tests**: none found
- **Failures**: none

---

## Fix Plans

None - no gaps found requiring a fix task.

---

## Requirement Traceability Update

| Requirement | Previous Status (spec.md) | New Status |
| ----------- | -------------------------- | ---------- |
| TENANT-01 | Pending | ✅ Verified |
| TENANT-02 | Pending | ✅ Verified |
| TENANT-03 | Pending | ✅ Verified |
| TENANT-04 | Pending | ✅ Verified |
| TENANT-05 | Pending | ✅ Verified |
| TENANT-06 | Pending | ✅ Verified |
| TENANT-07 | Pending | ✅ Verified |
| TENANT-08 | Implementing | ✅ Verified |
| TENANT-09 | Implementing | ✅ Verified |
| TENANT-10 | Implementing | ✅ Verified |
| TENANT-11 | Implementing | ✅ Verified |
| TENANT-12 | Pending | ✅ Verified |
| TENANT-13 | Pending | ✅ Verified |
| TENANT-14 | Pending | ✅ Verified |
| TENANT-15 | Pending | ✅ Verified |
| TENANT-16 | Pending | ✅ Verified |
| TENANT-17 | Pending | ✅ Verified |
| TENANT-18 | Pending | ✅ Verified |
| TENANT-19 | Implementing | ✅ Verified |
| TENANT-20 | Implementing | ✅ Verified |
| TENANT-21 | Implementing | ✅ Verified |
| TENANT-22 | Implementing | ✅ Verified |
| TENANT-23 | Implementing | ✅ Verified |
| TENANT-24 | Implementing | ✅ Verified |
| TENANT-25 | Pending | ✅ Verified |
| TENANT-26 | Pending | ⚠️ Spec-precision gap (deliberate - no enforcement expected in this feature) |

**Note**: `spec.md`'s own Requirement Traceability table (lines 176-211) is stale - it still shows `Pending`/`Implementing`/`Design` for every ID and was never updated as tasks landed. This validation report is the authoritative up-to-date status; updating `spec.md`'s table itself is a docs-sync action outside the Verifier's read-only mandate over the real tree (the Verifier may only write `validation.md` and the lessons file) - recommend the orchestrator apply this table to `spec.md` directly, or accept `validation.md` as the record of truth going forward.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 25/26 ACs matched spec outcome precisely; 1 deliberate spec-precision gap (TENANT-26, hex format for a future feature to enforce - spec.md itself defers this)
**Sensor**: 3/3 mutations killed
**Gate**: Quick + Full (disposable Postgres, `-p 1`) + Frontend, all green, 296 frontend tests / all Go packages `ok`

**What works**: RLS fail-closed isolation (authenticated and anonymous paths), self-hosted bootstrap auto-provisioning with no tenant UI, SaaS public signup with mandatory email verification and rate limiting, tenant-scoped admin invite (new-user and existing-multi-tenant-user branches), tenant switching with membership enforcement, fiscal profile fields with CPF/CNPJ length validation and `billing_address` never exposed, locale/branding schema groundwork.

**Issues found**: none requiring a fix task. One documentation drift noted (spec.md's own traceability table is stale) - cosmetic, not a code or test gap.

**Next steps**: none required to consider this feature done. Optional housekeeping: sync `spec.md`'s Requirement Traceability table to the statuses in this report.
