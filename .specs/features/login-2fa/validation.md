# Login 2FA Step Validation

**Date**: 2026-09-11
**Spec**: `.specs/features/login-2fa/spec.md`
**Diff range**: `6cac888..1fda02c` (7 commits: 7f007e9 docs, efee28d T1, 7074b98 T2, 65806cd T3, f81d189 T4, 14dd12b T5, 1fda02c gap-fix)
**Verifier**: independent fresh-eyes validation. This harness exposes no general sub-agent (only read-only `cavecrew-*`), so the `validate.md` standalone fallback was used: the coverage tables were re-derived from `spec.md` + test files, not from the implementation. Author-equals-verifier separation was not achievable here; flagged as a limitation, not hidden.

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | MSW challenge + verify handlers |
| T2   | ✅ Done | pt-BR/en strings |
| T3   | ✅ Done | `LoginOutcome` + `verifyTwoFactor` |
| T4   | ✅ Done | `LoginTwoFactorStep` |
| T5   | ✅ Done | `LoginPage` two-step wiring |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| LOGIN2FA-01 WHEN 2FA-enabled credentials THEN step, no session | verification step shown; no navigation away from `/login` | `LoginPage.test.tsx:49` - `await screen.findByLabelText("Código de verificação")`; `:94` - `expect(screen.queryByText("home page")).not.toBeInTheDocument()`; `:95` - `expect(screen.getByTestId("location")).toHaveTextContent("/login")` | ✅ PASS |
| LOGIN2FA-02 WHEN valid code THEN complete login | navigate to `/` (home) | `LoginPage.test.tsx:107` - `expect(await screen.findByText("home page")).toBeInTheDocument()` | ✅ PASS |
| LOGIN2FA-03 IF invalid/expired THEN inline error + stay | `"Código inválido ou expirado. Tente novamente."`; step remains | `LoginPage.test.tsx:119` - `expect(await screen.findByRole("alert")).toHaveTextContent("Código inválido ou expirado. Tente novamente.")`; `:122` - no `home page`; `:123` - code field present | ✅ PASS |
| LOGIN2FA-04 WHEN non-2FA credentials THEN unchanged | home; no 2FA step | `LoginPage.test.tsx:194` - `expect(await screen.findByText("home page")).toBeInTheDocument()`; `:195` - `expect(screen.queryByLabelText("Código de verificação")).not.toBeInTheDocument()`; provider: `AuthProvider.test.tsx:316` - `expect(...outcome...).toHaveTextContent("authenticated")` | ✅ PASS |
| LOGIN2FA-05 WHEN valid recovery code THEN complete login | home | `LoginPage.test.tsx:137` - `expect(await screen.findByText("home page")).toBeInTheDocument()` | ✅ PASS |
| LOGIN2FA-06 IF recovery invalid/used THEN inline error + stay | alert shown; recovery field remains | `LoginPage.test.tsx:150` - `toHaveTextContent("Código inválido ou expirado. Tente novamente.")`; `:153` - `expect(screen.getByLabelText("Código de recuperação")).toBeInTheDocument()` | ✅ PASS |
| LOGIN2FA-07 WHEN switch method THEN clear input + error | field cleared; error hidden | `LoginTwoFactorStep.test.tsx:39` - `expect(screen.getByLabelText("Código de recuperação")).toHaveValue("")`; `:53` - `expect(screen.getByLabelText("Código de verificação")).toHaveValue("")`; page: `LoginPage.test.tsx:168` - `expect(screen.queryByRole("alert")).not.toBeInTheDocument()` | ✅ PASS |
| LOGIN2FA-08 WHEN "Voltar" THEN credentials, discard token | email form returns; code step gone | `LoginPage.test.tsx:180` - `expect(screen.getByLabelText("E-mail")).toBeInTheDocument()`; `:181` - `expect(screen.queryByLabelText("Código de verificação")).not.toBeInTheDocument()` | ✅ PASS |
| LOGIN2FA-09 token held only in memory | not in URL or browser storage | `LoginPage.test.tsx:182` - `expect(screen.getByTestId("location")).toHaveTextContent("/login")`; `:187` - `expect(JSON.stringify(window.localStorage)).not.toContain("msw-2fa-challenge")`; `:188` - same for `window.sessionStorage` | ✅ PASS |
| LOGIN2FA-10 WHEN `login` gets `challenge_token` THEN `twoFactorRequired`, no hydrate | outcome kind `twoFactorRequired`; admin stays null | `AuthProvider.test.tsx:298` - `expect(screen.getByTestId("outcome")).toHaveTextContent("twoFactorRequired")`; `:300` - `expect(screen.getByTestId("admin")).toHaveTextContent("null")` | ✅ PASS |
| LOGIN2FA-11 WHEN `verifyTwoFactor` succeeds THEN hydrate from `/me` | status `authenticated`; admin.email `owner@vane.app` | `AuthProvider.test.tsx:338` - `expect(screen.getByTestId("status")).toHaveTextContent("authenticated")`; `:339` - `expect(JSON.parse(...).email).toBe("owner@vane.app")` | ✅ PASS |
| LOGIN2FA-12 all new strings render pt + en | English labels under `en` locale | `LoginPage.test.tsx:203` (test) - `:214` `expect(await screen.findByLabelText("Verification code")).toBeInTheDocument()`; plus `Verify`/`Back`/`Use a recovery code` buttons and `Recovery code` field | ✅ PASS (gap closed during validation, commit `1fda02c`) |

**Status**: ✅ All 12 ACs covered with `file:line` evidence and spec-defined outcomes matched.

---

## Discrimination Sensor

Scratch: temporary `git worktree` at `HEAD` (node_modules symlinked), mutations applied per-file and reverted with `git checkout --` between runs. Real tree porcelain was empty before and after; the scratch was removed.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ----------- | ------- |
| M1 | `LoginPage.tsx` (handleSubmit) | `outcome.kind === "twoFactorRequired"` → `!==` | ✅ Killed |
| M2 | `LoginPage.tsx` (handleVerify) | `err.status === 401` → `err.status !== 401` | ✅ Killed |
| M3 | `AuthProvider.tsx` (login) | `if (body.challenge_token)` → `if (!body.challenge_token)` | ✅ Killed |
| M4 | `AuthProvider.tsx` (verifyTwoFactor) | recovery branch `"recoveryCode" in factor` → `false` (always sends `code`) | ✅ Killed |
| M5 | `LoginTwoFactorStep.tsx` (switchMethod) | removed `setValue("")` on method switch | ✅ Killed |
| M6 | `test/msw/handlers.ts` (login) | `if (twoFactorEnabledState)` → `if (!twoFactorEnabledState)` | ✅ Killed |

**Sensor depth**: P0-full (auth is a critical path) - 6 behavior-level mutations, ≥5 required.
**Result**: 6/6 killed - PASS ✅

---

## Interactive UAT Results

Not performed in this session. The feature is user-facing; UAT can be run against a live SPA if desired, but every flow is exercised by the component tests above.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes | ✅ (only `AuthProvider`, `LoginPage`, `i18n`, `handlers`, plus the two new `features/auth` files) |
| No scope creep | ✅ (no backend, no separate route, no trusted-device) |
| Matches patterns | ✅ (reuses `Field`/`Button`, `/me` hydrate, MSW conventions, `login.*` i18n namespace) |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Per-layer Coverage Expectation met (provider + component layers, happy + edge + error) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed: `AGENTS.md` §3 (gates), §5 (i18n, MSW-shape), `CONTRIBUTING.md` (commits) | ✅ |

Note: `LoginTwoFactorStep` gained an `onMethodChange` prop beyond the interface sketched in `design.md`. It is required to satisfy LOGIN2FA-07 (clear the page-owned error on method switch) and is a refinement of the sketch, not a behavioral deviation.

---

## Edge Cases

- [x] Expired challenge (5 min) → backend 401 → inline `invalid` error; recover via "Voltar" (`LoginPage.tsx:handleVerify`; AC3/AC8 tests).
- [x] 2FA disabled after challenge → backend 401 → same inline error (same 401 branch).
- [x] Non-401 failure → generic `login.twoFactor.generic` (`LoginPage.tsx:handleVerify` else branch); no silent failure.
- [x] English locale → all new strings render in English (LOGIN2FA-12 test).
- [x] Step does not render the password field or the forgot-password link (credentials form is conditionally replaced, `LoginPage.tsx` render).

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && cd web && npx tsc -b --noEmit && npm run test`
- **Result**: 397 passed, 0 failed, 0 skipped
- **Test count before feature**: 378
- **Test count after feature**: 397
- **Delta**: +19 new tests
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans (if issues found)

### Fix 1: LOGIN2FA-12 English-locale coverage gap

- **Root cause**: No test asserted the new strings under the `en` locale; the AC was implemented but unverified.
- **Fix task**: Added `renderiza o passo de 2FA em inglês quando o locale é en` to `LoginPage.test.tsx`.
- **Priority**: Major (coverage gap)
- **Status**: Closed in commit `1fda02c`.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| LOGIN2FA-01..12 | Done | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 12/12 ACs matched spec outcome; 0 spec-precision gaps
**Sensor**: 6/6 mutations killed
**Gate**: 397 passed

**What works**: 2FA-enabled login shows the verification step with no session; TOTP and recovery codes both complete login; invalid/expired codes show an inline error and keep the step; "Voltar" returns to credentials; the challenge token never leaves component memory; non-2FA login is unchanged; every new string ships in pt-BR and English.

**Issues found**: none open. One Major coverage gap (LOGIN2FA-12 English locale) was found and closed during validation.

**Next steps**: Feature complete. Optional: interactive UAT on a live SPA; release prep when requested.
