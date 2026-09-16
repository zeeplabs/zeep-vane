# Meu Perfil Page Validation

**Date**: 2026-09-11
**Spec**: `.specs/features/profile-page/spec.md`
**Diff range**: uncommitted working tree on `develop` (baseline HEAD `378b28f`)
**Verifier**: independent fresh-eyes pass (standalone fallback - this harness exposes no general-purpose sub-agent; author != verifier enforced by re-deriving coverage from the spec rather than reusing the author's task notes)

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 | ✅ Done | `/me` gains `two_factor_enabled` |
| T2 | ✅ Done | MSW handlers for profile/2FA + `/me` field |
| T3 | ✅ Done | `AuthProvider.two_factor_enabled` + `refreshAdmin()` |
| T4 | ✅ Done | `profile.*` / `twoFactor.*` i18n (pt + en) |
| T5 | ✅ Done | `useUpdateProfileName`, `useChangePassword` |
| T6 | ✅ Done | `useEnroll2FA`, `useConfirm2FA`, `useDisable2FA` |
| T7 | ✅ Done | `PersonalInfoCard` |
| T8 | ✅ Done | `EnrollDrawer` |
| T9 | ✅ Done | `DisableDialog` |
| T10 | ✅ Done | `TwoFactorCard` |
| T11 | ✅ Done | `SecurityCard` |
| T12 | ✅ Done | `SessionsSection` revoke confirmation |
| T13 | ✅ Done | `ProfilePage` |
| T14 | ✅ Done | `/profile` route |
| T15 | ✅ Done | `AvatarMenu` link |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| P1: reach page - route + 3 cards | 3 cards render | `web/src/features/profile/ProfilePage.test.tsx:34` - `expect(await screen.findByText("Informações pessoais"))` (+ Segurança, Sessões ativas) | ✅ PASS |
| P1: user-menu link (any role) | "Meu Perfil" -> `/profile` | `web/src/layout/AvatarMenu.test.tsx:70` - `expect(getByRole("menuitem",{name:"Meu Perfil"})).toHaveAttribute("href","/profile")`; non-owner `:78` | ✅ PASS |
| P1: unauthenticated -> login | redirect via RequireAuth | `web/src/App.test.tsx:119` - `expect(getByRole("heading",{name:"Entrar"}))` | ✅ PASS |
| P4: name success | PATCH `{name}` -> refresh + toast | `web/src/features/profile/PersonalInfoCard.test.tsx:53` - `findByText("Nome atualizado.")`; hook `hooks.test.ts:28` | ✅ PASS |
| P5: display new name, no reload | context `admin.name` updates | `web/src/features/profile/hooks.test.ts:45` - `expect(auth.admin?.name).toBe("Ana Silva")` | ✅ PASS |
| P6: empty name blocked | inline error, no request | `PersonalInfoCard.test.tsx:69` - `findByText("Informe seu nome.")` + `expect(patchSpy).not.toHaveBeenCalled()` `:87` | ✅ PASS |
| P7: password success | 200 -> clear + toast | `web/src/features/profile/SecurityCard.test.tsx:85` - `findByText("Senha atualizada.")` + fields `""` `:93-95` | ✅ PASS |
| P8: confirmation mismatch | inline mismatch, no request | `SecurityCard.test.tsx:44` - `findByText("As senhas não coincidem.")` + `expect(spy).not.toHaveBeenCalled()` `:59` | ✅ PASS |
| P9: wrong current | 401 -> inline error | `SecurityCard.test.tsx:63` - `findByText("Senha atual incorreta.")`; hook `hooks.test.ts:68` `rejects.toMatchObject({status:401})` | ✅ PASS |
| P10: policy 422 | 422 -> inline policy error | `SecurityCard.test.tsx:74` - `findByText("A senha deve ter entre 8 e 72 caracteres.")`; hook `hooks.test.ts:78` | ✅ PASS |
| P11: session survives | still authenticated | `SecurityCard.test.tsx:96` - `await expect(apiFetch("/api/auth/me")).resolves.toMatchObject({email:"owner@vane.app"})` | ✅ PASS |
| P12: 2FA status rendering | from `two_factor_enabled` | `web/src/features/two-factor/TwoFactorCard.test.tsx:42` - badge "Desativada"; `:56` badge "Ativada" | ✅ PASS |
| P13: enroll QR + secret | QR of `otpauth_uri` + raw secret | `web/src/features/two-factor/EnrollDrawer.test.tsx:38` - `qr.querySelector("svg")` + `enroll-secret` non-empty; hook `hooks.test.ts:29` | ✅ PASS |
| P14: confirm + recovery once | 10 codes in dedicated step | `EnrollDrawer.test.tsx:75` - `getAllByTestId("recovery-code")` `.toHaveLength(10)`; hook `hooks.test.ts:55` | ✅ PASS |
| P15: invalid code | inline error, stays verify | `EnrollDrawer.test.tsx:48` - `findByText("Código inválido. Tente novamente.")` + field still present `:61` | ✅ PASS |
| P16: enable refresh | onEnabled after dismissal | `EnrollDrawer.test.tsx:95` - `expect(onEnabled).toHaveBeenCalledTimes(1)`; card wires it to `refreshAdmin` | ✅ PASS |
| P17: disable success | 200 -> refresh to disabled | `DisableDialog.test.tsx:41` - `onDisabled` called; `TwoFactorCard.test.tsx:75` - `data-enabled="false"` | ✅ PASS |
| P18: disable wrong password | 401 -> inline, stays enabled | `DisableDialog.test.tsx:56` - `findByText("Senha incorreta.")` + `onDisabled` not called; hook `hooks.test.ts:95` | ✅ PASS |
| P19: sessions list | reuses SessionsSection | `SessionsSection.test.tsx:46` - 2 rows, current badge, no button on current | ✅ PASS |
| P20: confirmation dialog | dialog before DELETE | `SessionsSection.test.tsx:99` - `confirm-revoke-button` present + `expect(spy).not.toHaveBeenCalled()` `:116` | ✅ PASS |
| P21: confirm revokes | DELETE + row removed | `SessionsSection.test.tsx:72` - confirm -> `getAllByTestId("session-row")` `.toHaveLength(1)` | ✅ PASS |
| P22: cancel keeps | no DELETE, row stays | `SessionsSection.test.tsx:120` - `expect(spy).not.toHaveBeenCalled()` + rows stay 2 `:138` | ✅ PASS |
| P23: `/me` 2FA status | bool, true only when confirmed | `internal/api/auth_handler_test.go:383` (false + key present) and `:423` (true) | ✅ PASS |

**Status**: ✅ All 23 ACs covered with `file:line` evidence; asserted values match the spec-defined outcomes.

---

## Edge Cases

- [x] Name `422` -> same inline error: `PersonalInfoCard.test.tsx:89` (`422 do servidor mostra o erro inline de nome`)
- [x] Enroll `409` -> close + refresh + informational: `EnrollDrawer.test.tsx:99` (`enroll 409 fecha o drawer, avisa e chama onEnabled`)
- [x] Sessions list failure -> card load error: `SessionsSection.test.tsx:149`
- [x] Recovery-codes "not shown again" warning: `EnrollDrawer.test.tsx:77`
- [x] Browser locale English -> strings in English: `ProfilePage.test.tsx:44` (asserts "My Profile", "Personal information", "Security", "Active sessions")

---

## Discrimination Sensor

Scratch: temp file copies under `/tmp/vane-mut` (never `git stash`); each mutant restored from its backup before the next; `git status --porcelain` matched the pre-sensor baseline after every run.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| M1 | `internal/api/auth_handler.go` (`Me`) | `TwoFactorEnabled: twoFactorEnabled` -> `false` | ✅ Killed (`TestMe_TwoFactor*` failed on integration DB) |
| M2 | `web/src/features/profile/PersonalInfoCard.tsx` | `if (!trimmed)` -> `if (false)` (empty-name guard off) | ✅ Killed |
| M3 | `web/src/features/two-factor/EnrollDrawer.tsx` | removed inline error setter on confirm failure | ✅ Killed |
| M4 | `web/src/features/sessions/SessionsSection.tsx` | confirm button no-op (dialog confirm stops revoking) | ✅ Killed |
| M5 | `web/src/features/two-factor/TwoFactorCard.tsx` | `enabled = admin.two_factor_enabled` -> negated | ✅ Killed |

**Sensor depth**: lightweight (5 targeted behavior-level mutations; P1 feature, not P0)
**Result**: 5/5 killed - PASS ✅

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ (SessionsSection's documented no-confirm choice updated in place, not duplicated) |
| No scope creep | ✅ (Notifications card and login-time 2FA explicitly deferred) |
| Matches patterns | ✅ (reuses `Card`/`Field`/`Dialog`/`Drawer`/`useMutation`/`sonner`, mirrors `features/sessions/`) |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Per-layer Coverage Expectation met | ✅ (Go handler: both states; components: happy + edge + error per flow) |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed | ✅ `AGENTS.md` §5 (MSW mirrors backend shape, `Pagination`/coverage), §3 gates |

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && cd web && npx tsc -b --noEmit && npm run test`
- **Result**: 378 passed, 0 failed, 0 skipped
- **Integration gate** (`TEST_DATABASE_URL=<disposable> go test -tags=integration -p 1 ./...`): all packages ok (incl. `internal/api`)
- **Test count before feature**: 340 (frontend)
- **Test count after feature**: 378 (frontend)
- **Delta**: +38 frontend tests, +2 Go integration assertions
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

No surviving mutants or uncovered ACs. Three spec-precision gaps were found during this pass and closed before sign-off (English-locale edge case, enroll 409 at the component level, name 422 at the component level), plus an explicit PROFPAGE-11 session-survival assertion. No fix tasks outstanding.

---

## Requirement Traceability Update

All `PROFPAGE-01..23` moved from `Pending` to `Done` in `spec.md`.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 23/23 ACs matched spec outcome; 0 spec-precision gaps remaining
**Sensor**: 5/5 mutations killed
**Gate**: 378 passed, 0 failed

**What works**: page + route + user-menu entry; name update with empty/422 validation; password change with mismatch/401/422 handling that keeps the session; full 2FA enroll/confirm/disable with one-time recovery codes; sessions revoke behind a confirmation dialog; `GET /api/auth/me` reports 2FA status.

**Issues found**: none outstanding.

**Next steps**: commits are left to the user (AGENTS.md §2). Nothing pushed.
