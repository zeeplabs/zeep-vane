# Validation Report — deployment-mode

**Verifier**: independent (fresh read, no inherited assumptions from the author of commit `84db367`).
**Scope reviewed**: commit `84db3677e89eb7a4ff455b91adf2235a89d669ef` on top of `0fd5109`, branch `develop`.

**Result**: PASS

---

## Per-AC Evidence

| AC | Requirement | Evidence | Status |
| --- | --- | --- | --- |
| DEPMODE-01 | `VANE_DEPLOYMENT_MODE` read at boot, `""`/`self_hosted`/`saas` accepted, any other value fails boot | `internal/config/config.go:172-178` — `os.Getenv("VANE_DEPLOYMENT_MODE")`, empty→`DeploymentModeSelfHosted` default, else validated against the two constants (`config.go:33-38`), error otherwise. Covered by `internal/config/config_test.go:391` (`TestLoad_DeploymentModeInvalid_Error`). | Met |
| DEPMODE-02 | self-hosted mode → 3 signup routes 404, handler/repo untouched | `internal/cli/routes.go:287-300` — `requireSaaSMode` middleware short-circuits with `http.NotFound` before calling `next`. Wired onto all 3 routes at `routes.go:143-145`. Verified live: `TestAdminRouter_SignupRoutes_SelfHostedMode_404` (`routes_test.go:1283+`) asserts 404 on signup/verify/resend-verification with `DeploymentMode: self_hosted`. | Met |
| DEPMODE-03 | saas mode → 3 routes behave exactly as before | Same middleware, `deploymentMode == config.DeploymentModeSaaS` path calls `next.ServeHTTP` unchanged. Pre-existing signup tests (`TestAdminRouter_SignupRateLimit_*`) run against `newAdminRouterAndTenantForTest`'s router, which is explicitly configured `DeploymentMode: config.DeploymentModeSaaS` (`routes_test.go:40`) and pass. | Met |
| DEPMODE-04 | `POST /api/bootstrap` unchanged in both modes | `routes.go:149` — bootstrap route has no `requireSaaSMode` wrapper, only the pre-existing `credentialLimiter.Middleware`. **Gap**: no test exercises `POST /api/bootstrap` under `DeploymentMode: self_hosted` specifically — `TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` only runs under the shared saas-mode test router. Confirmed via mutation sensor (b) below: gating bootstrap behind `requireSaaSMode` in self-hosted mode does not fail any test. Code is correct (spec's "no new gate" is honestly implemented — no gate was added), but the AC's own protection ("continue functioning identically") has no independent regression test. | Met (code), gap in coverage |
| DEPMODE-05 | `GET /api/bootstrap/status` includes `deployment_mode` | `internal/api/bootstrap_handler.go:69-72` (`bootstrapStatusResponse.DeploymentMode`), populated from `h.deploymentMode` set via constructor (`bootstrap_handler.go:61-77`), wired from `cfg.DeploymentMode` at `routes.go:88`. Tested in `TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` (parses response as `map[string]any`, comment explicitly calls out the new field) and `bootstrap_handler_test.go`. | Met |
| DEPMODE-06 | `AuthProvider` exposes `deploymentMode` from the same boot fetch | `web/src/auth/AuthProvider.tsx:82-86` (interface field + doc), `:131` (`useState`), `:161-172` (fetch destructures `deployment_mode`, sets state in the same `try` block as `needsBootstrap`, no extra round-trip), `:304` (exposed on context value). Tested in `AuthProvider.test.tsx`. | Met |
| DEPMODE-07 | `LoginPage` hides link / `SignupPage` shows restricted message in self-hosted; routes still exist | `web/src/features/auth/LoginPage.tsx:19,149-154` — link wrapped in `{deploymentMode === "saas" ? (...) : null}`. `web/src/features/signup/SignupPage.tsx:25,85-89` — restricted-message branch renders instead of the form/pending views when `deploymentMode === "self_hosted"`, form/pending JSX untouched otherwise (no route removal — `App`'s router still mounts `SignupPage`/`VerifyEmailPage`). Tested in `LoginPage.test.tsx` and `SignupPage.test.tsx`. | Met |
| DEPMODE-08 | saas mode → frontend behavior identical to `auth-pages-redesign` baseline | Same conditionals: `deploymentMode === "saas"` shows the link, `pendingEmail`/form branches in `SignupPage` are reached unchanged when not self-hosted. Regression coverage: existing `LoginPage`/`SignupPage` tests continue to pass with the MSW default (`deployment_mode: "saas"`, confirmed in `web/src/test/msw/handlers.ts`). | Met |

Edge cases (empty env var → self_hosted; optimistic `"saas"` default while boot fetch in flight; direct `/signup` URL access in self-hosted shows restricted message, not a failing form) are all covered by the same code paths above — no separate implementation was needed since they fall out of the state machine described.

---

## Gate Results

| Gate | Result |
| --- | --- |
| `go build ./...` | Clean |
| `go vet ./...` | Clean |
| `gofmt -l` on all changed `.go` files | Clean (no output) |
| Backend integration suite (disposable Postgres, `docker run postgres:16-alpine`, port 5433, destroyed after) | All packages `ok`, including `internal/api` (46s), `internal/cli` (9.2s), `internal/db` (46.9s) |
| `cd web && npx tsc -b --noEmit` | Clean |
| `cd web && npm run test -- --run` | 90 files / 554 tests passed |

Both disposable containers used (`vane-test-pg` for the real gate, `vane-test-pg-mut` for the sensor run) were stopped/removed after use. `vane-dev-pg` was never touched.

---

## Discrimination Sensor (isolated git worktree, `--detach` at `84db367`, `web/dist` copied in for embed, `node_modules` symlinked for vitest — worktree destroyed after, real working tree untouched throughout)

| # | Mutation | Result | Evidence |
| --- | --- | --- | --- |
| a | Invert `requireSaaSMode`'s condition (`==` instead of `!=` `config.DeploymentModeSaaS`) | **Killed** | `TestAdminRouter_SignupRoutes_SelfHostedMode_404` and both `TestAdminRouter_SignupRateLimit_*` tests fail (signup 404s in saas-mode router, works in self-hosted-mode router) |
| b | Also wrap `POST /api/bootstrap` with `requireSaaSMode(cfg.DeploymentMode)` | **Survived** | `go test -tags=integration ./internal/cli/... ./internal/api/...` stays green — no test creates a bootstrap admin under `DeploymentMode: self_hosted`. Real coverage gap for DEPMODE-04, see table above. |
| c | Hardcode `AuthProvider`'s `deploymentMode` initial `useState` to `"self_hosted"` instead of `"saas"` | **Killed** | 4 `SignupPage.test.tsx` tests fail — form fields (`Nome da organização`, etc.) not found because the restricted message renders before/without a resolved MSW fetch in test timing |
| d | Remove the `deploymentMode === "self_hosted"` conditional in `SignupPage` (force `false`) | **Killed** | `SignupPage.test.tsx`'s self-hosted-mode test fails: `findByText("Cadastro indisponível")` never resolves, form renders instead |
| e | Make `config.Load` skip the invalid-value validation (`if false && ...`) | **Killed** | `TestLoad_DeploymentModeInvalid_Error` fails: `Load()` returns no error for an invalid `VANE_DEPLOYMENT_MODE` |

**4/5 mutations killed, 1 survived** (mutation b — a real, if narrow, coverage gap on AC4/DEPMODE-04).

---

## Ranked Gap List

1. **(Low severity, test-coverage only)** No integration test asserts `POST /api/bootstrap` succeeds specifically under `DeploymentMode: self_hosted` — the only router exercising bootstrap creation (`TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter`) uses the shared saas-mode test router (`newAdminRouterAndTenantForTest`, `routes_test.go:40`). The implementation is correct (no gate was added to `/bootstrap` per spec, and none should be), but DEPMODE-04 ("SHALL continue functioning identically in both modes") has no regression test that would catch a future accidental gate on that route in self-hosted mode specifically. Recommend adding a `self_hosted`-mode variant of the existing bootstrap round-trip test (clone of `TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` with `DeploymentMode: config.DeploymentModeSelfHosted`), or extending `TestAdminRouter_SignupRoutes_SelfHostedMode_404`'s router to also assert bootstrap succeeds there.
2. **(Cosmetic/no-op, not a defect)** No action needed on documentation: README's Configuration table and `.specs/STATE.md`'s `AD-033` entry both accurately describe the shipped behavior (verified against actual code, not just claims).

No other functional, security, or spec-compliance gaps found. All 8 ACs have direct, verifiable evidence; all stated gates are green; 4 of 5 injected mutations were caught by the existing suite.
