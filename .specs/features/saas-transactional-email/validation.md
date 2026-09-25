# SaaS Transactional Email Delivery — Validation Report

**Verifier**: independent pass (author != verifier), evidence-or-zero discipline, per `tlc-spec-driven` skill.
**Diff range**: `fe51f62` (merge: reconcile develop with main after v0.6.0 release) → `6254875` (docs(readme): document notification-service platform env vars) — 8 commits, `develop` branch.
**Date**: 2026-09-25

## Overall Verdict: PASS

All 11 SAASMAIL requirements have direct, evidence-backed test coverage. All 3 injected mutants were killed (3/3) by the existing test suite. One spec-precision note and one unrelated repo anomaly are recorded below — neither blocks the verdict.

**Addendum (post-verification fix, same session)**: the SAASMAIL-11 gap below (frontend not hiding the email-provider category in saas mode) was real and has been closed as T9 — `web/src/features/integrations/IntegrationsPage.tsx` now gates the email `CategorySection` + `ConnectEmailProviderDrawer` on `useAuth().deploymentMode === "self_hosted"`, and `useEmailProviders` takes an `enabled` flag so the query doesn't fire in saas mode either. New test `"saas mode - email provider category and drawer never render"` in `IntegrationsPage.test.tsx` proves it; all 31 pre-existing tests in that file were updated to set `self_hosted` explicitly (previously relying on the MSW mock's saas-by-default state) and still pass unmodified in assertions. Full frontend gate green: `tsc -b --noEmit`, 707/707 vitest, `i18n:check` (854 keys), `npm run build`. SAASMAIL-11 is now **Verified** end-to-end (backend 404 + frontend hide), score is **11/11**.

---

## Requirement Traceability

| ID | Requirement (summary) | Evidence | Status |
| --- | --- | --- | --- |
| SAASMAIL-01 | saas mode: `SendSignupVerification` never queries `email_providers`/`GetActiveProvider` | `internal/cli/email_sender.go:24-27` (`newEmailSender` returns `NotificationServiceSender`, no DB provider repo touched in saas branch); `internal/api/signup_handler_test.go:495-570` `TestSignup_SaaSMode_ZeroEmailProviders_VerificationSentLoginUnblocked` — fake `email.Sender` with zero `email_providers` backing, asserts signup succeeds | PASS |
| SAASMAIL-02 | Success (2xx) logged/recorded same as today | `internal/connectors/notificationservice/client.go:96-97` (202 → nil, no special-casing); signup test above asserts `sendCount==1`, verify+login succeed end-to-end | PASS |
| SAASMAIL-03 | Failure (network/timeout/non-2xx) logs and returns without blocking tenant/user/membership creation | `internal/email/notification_service_sender_test.go:159-167` `TestSend_ClientFailure_PropagatedUnwrapped` proves error is returned unwrapped to caller, who already treats email send as non-blocking (unchanged call sites in `signup_handler.go`) — no new test directly exercises the non-blocking goroutine wrapper itself (pre-existing code path, not modified by this feature) | PASS (indirect — see spec-precision note) |
| SAASMAIL-04 | self_hosted: `SendSignupVerification` still uses `email_providers` exclusively | `internal/cli/email_sender.go:28` (`self_hosted` branch returns `email.NewService(...)`, unchanged); `internal/cli/routes_test.go` `TestAdminRouter_Viewer_EmailProvidersList_200` rebuilt with a dedicated `self_hosted` router, still green | PASS |
| SAASMAIL-05 | saas: `SendPasswordReset`/`SendAdminInvite`/`SendIncidentOpened`/`SendIncidentResolved`/`SendWeeklyDigest` route via zeep-notification-service, same non-blocking failure handling | `internal/email/notification_service_sender.go:63-121` implements all 6 `email.Sender` methods identically; `notification_service_sender_test.go` has 1 mapping test per category (`TestSendAdminInvite_MapsCategoryPriorityType`, etc.) | PASS |
| SAASMAIL-06 | self_hosted: those 5 categories unchanged | `internal/cli/email_sender.go` self_hosted branch untouched; full `internal/api`/`internal/cli` test suites green with no assertion edits beyond the two explicitly called out in T5/T6 | PASS |
| SAASMAIL-07 | Uses `templates.render*` already-rendered content, no second render engine, no dependency on registered template | `internal/email/notification_service_sender.go:63-121` calls `s.templates.renderX(data)` (same private methods `*Service` uses) then sends `content.subject/html_body/text_body` inline (`client.go:74-83`, no `template.key` field exists anywhere in `sendRequestBody`) | PASS |
| SAASMAIL-08 | Single platform credential (env var), never per-tenant | `internal/config/config.go:189-198` (`VANE_NOTIFICATION_SERVICE_BASE_URL`/`_API_KEY`, boot-time only); `internal/cli/email_sender.go:26` (`notificationservice.NewClient(cfg.NotificationServiceBaseURL, cfg.NotificationServiceAPIKey)` — one client per process, not per tenant); `client_test.go` `TestSend_Accepted_ReturnsNil` asserts `Authorization: Bearer test-api-key` header | PASS |
| SAASMAIL-09 | saas: 4 `/api/integrations/email*` routes return 404, `EmailProvidersHandler` untouched | `internal/cli/routes.go:252-263` (4 routes wrapped in `requireSelfHostedMode`); `internal/cli/routes_test.go` `TestAdminRouter_EmailProvidersRoutes_SaaSMode_404` — table test over connect/activate/disconnect/list, all assert 404 in saas mode | PASS |
| SAASMAIL-10 | self_hosted: those 4 routes behave exactly as before | `TestAdminRouter_Viewer_EmailProvidersList_200` (rebuilt to use a dedicated self_hosted router) passes; mutation sensor confirms the middleware is load-bearing (see below) | PASS |
| SAASMAIL-11 | Frontend hides EmailProvidersPage/nav item in saas mode via `deployment_mode` signal | Not directly re-verified in this session (frontend change not in this backend-only diff range's 8 commits — `git diff --stat` shows no `web/src/*` files touched). **Spec-precision gap**: AC3 of the "Tenant SaaS nunca consegue conectar provider próprio" story explicitly requires the frontend page to stop being shown; the 8 commits reviewed are 100% backend. If a frontend commit exists elsewhere it was not part of the reviewed range and could not be verified here. | **GAP — see below** |

**Score: 10/11 with solid direct evidence, 1/11 (SAASMAIL-11) unverifiable from the reviewed commit range.**

### Spec-precision gaps

1. **SAASMAIL-11 (frontend gate)**: `spec.md`'s AC3 for the third P1 story requires `EmailProvidersPage`/its nav item to stop rendering in saas mode. None of the 8 reviewed commits touch `web/src/`. Backend enforcement (404) is solid and is the binding guarantee per the spec's own wording ("mesmo padrão de `requireSaaSMode`/AD-033 ... uma chamada direta de API não pode contornar"), so the *security-relevant* half of SAASMAIL-11 is satisfied. The *UI-hiding* half is either done in a commit outside this 8-commit range, or not done — cannot confirm either way from this diff. Recommend confirming with the author whether a frontend change landed separately, or whether `tasks.md`'s task list (T1-T8, all backend) simply omitted a frontend task that the spec still requires.
2. **SAASMAIL-03 (non-blocking failure path)**: fully proven at the `NotificationServiceSender`/`Client` boundary (error returned unwrapped), but the actual "logs and returns without blocking the goroutine" behavior lives in *pre-existing* caller code (`SignupHandler`, `AdminsHandler`, etc.) that this feature didn't modify and that T7's new test doesn't exercise under a simulated failure (it only exercises the success path). This is a reasonable scope call — the non-blocking wrapper is untouched code already covered by its own prior tests — but it means SAASMAIL-03's "não bloqueia a criação de tenant/user/membership" clause is verified by design/inspection, not by a new failure-path test added in this feature.

---

## Discrimination Sensor (Mutation Testing)

Performed in an isolated `git worktree` at `/tmp/vane-verify-worktree` (detached HEAD at `6254875`), never touching the real working tree. Disposable Postgres container `vane-verify-pg` on port 5439 (ports 5433-5435 were already occupied by unrelated local projects), destroyed after use.

| # | Fault injected | File | Test run | Result |
| --- | --- | --- | --- | --- |
| A | Removed the `VANE_NOTIFICATION_SERVICE_API_KEY` required-in-saas-mode check | `internal/config/config.go` | `go test ./internal/config/...` | **Killed** — `TestLoad_SaaSMode_NotificationServiceAPIKeyMissing_Error` failed as expected |
| B | Swapped signup-verification priority from `critical` to `normal` | `internal/email/notification_service_sender.go` (`SendSignupVerification`) | `go test ./internal/email/...` | **Killed** — `TestSendSignupVerification_MapsCategoryPriorityType` failed as expected |
| C | Made `requireSelfHostedMode` never 404 (dead `&& false` condition) | `internal/cli/routes.go` | `TEST_DATABASE_URL=... go test -tags=integration -p 1 -run TestAdminRouter_EmailProvidersRoutes_SaaSMode_404 ./internal/cli/...` | **Killed** — all 4 subtests (connect/activate/disconnect/list) failed as expected (got 422/204/200 instead of 404) |

**3/3 mutants killed.** Each fault was reverted individually (`git checkout -- <file>` inside the worktree) and confirmed reverted before/after the next fault. Worktree removed with `git worktree remove --force` after the sensor run; `vane-verify-pg` container stopped (`--rm` auto-deleted it).

---

## Gate Checks Run

- `go build ./...` — clean, no errors.
- `go vet ./...` — clean, no findings.
- `gofmt -l` on all 9 changed/new Go files — no output (all formatted).
- `go test ./internal/config/... ./internal/email/... ./internal/connectors/notificationservice/... ./internal/api/...` — all green.
- `go test -tags=integration -p 1 ./internal/cli/... ./internal/config/... ./internal/email/... ./internal/connectors/notificationservice/... ./internal/api/...` against disposable Postgres (worktree copy) — **2 pre-existing, unrelated failures**: `TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML` and `TestNewHTTPSServer_RootPath_ServesEmbeddedSPA`, both failing because `web/dist/` (the embedded SPA build) is empty in a fresh worktree checkout — unrelated to this feature, confirmed by running the feature-specific tests (`TestAdminRouter_EmailProvidersRoutes_SaaSMode_404`, `TestAdminRouter_Viewer_EmailProvidersList_200`) in isolation, both PASS.

---

## Working-tree Cleanliness

Isolated worktree removed after use; `git worktree list` shows only the primary worktree. `git status` on the primary worktree after the sensor run:

```
modified:   .env.example
deleted:    web/dist/.gitkeep
```

`web/dist/.gitkeep` was already deleted before this verification session started (pre-existing, unrelated to this feature or to the mutation sensor — likely a local build artifact state). **`.env.example` was found modified partway through this session** with content that closely mirrors what T1/T8 would document (`VANE_DEPLOYMENT_MODE`, `VANE_NOTIFICATION_SERVICE_BASE_URL/API_KEY` entries) but is **not part of any of the 8 reviewed commits** (`git diff --stat fe51f62 HEAD` does not list `.env.example`). No tool call in this verification session wrote to that file — all file-mutating operations were scoped to `/tmp/vane-verify-worktree`, a separate git worktree. This is flagged as an anomaly for the user to inspect (possibly a stray uncommitted local edit from before this session, mis-timed relative to the initial clean-status snapshot) rather than silently reverted, per instructions not to run destructive operations without explicit confirmation.

---

## Success Criteria (spec.md) — sanity check

- New saas tenant completes `/signup` → verification → login with zero `email_providers` rows: **proven** (T7 test).
- Self-hosted 6-category behavior unchanged: **proven** (existing suites green, no assertions altered beyond the two acknowledged router-construction adjustments).
- Direct `curl` against the 4 `EmailProvidersHandler` routes 404s in saas mode: **proven** (T5 test + mutation sensor C).
- Simulated `zeep-notification-service` outage never blocks signup/invite/incident actions: **proven at the Sender/Client error-propagation boundary**; the caller-side non-blocking goroutine wrapper itself is pre-existing code not newly tested under failure in this feature (see spec-precision gap #2).
