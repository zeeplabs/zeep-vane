# LESSONS - auto-maintained by scripts/lessons.py

> Machine-owned. Do NOT hand-edit. Changes are overwritten on the next `lessons.py` write.
> Canonical state lives in `.specs/lessons.json`. Edit lessons only via the script.
> promote_threshold=2 distinct features · window_days=45 · quarantine_threshold=2

## Confirmed (load these at Specify/Design)

Corroborated across multiple features. Safe to apply as guidance.

_none_

## Candidates (under observation - do NOT load as guidance yet)

Seen once or not yet corroborated. Tracked, not trusted.

### L-001 - Test every route in the production route-mounting file, not one representative route per category
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: admin-dashboard
- evidence: validation.md M5/M6 - internal/cli/routes.go:69,72 (routes)
- last seen: 2026-08-20T00:25:53Z

### L-002 - Exercise authorization through the production router assembly, not a router rebuilt inside the test
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: admin-dashboard
- evidence: validation.md M8 - internal/cli/routes.go:60 (routes)
- last seen: 2026-08-20T00:25:53Z

### L-003 - When a record is deleted, assert the resulting access denial on a live request instead of only asserting the row is gone
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `auth` · harmful: 0
- features: admin-dashboard
- evidence: ADM-07 + spec.md:97 - internal/api/admins_test.go:620 (auth)
- last seen: 2026-08-20T00:25:53Z

### L-004 - Assert the exact token lifetime the spec states, reading it from the row the handler wrote
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `auth` · harmful: 0
- features: admin-dashboard
- evidence: ADM-01 - internal/api/admins.go:22 (auth)
- last seen: 2026-08-20T00:25:53Z

### L-005 - Reconcile the spec text with the committed schema before implementing, instead of documenting a deviation in the handler
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `spec` · harmful: 0
- features: admin-dashboard
- evidence: SPEC_DEVIATION internal/api/admins.go:84-93 (spec)
- last seen: 2026-08-20T00:25:53Z

### L-006 - Never size a boundary test's payload from the constant under test; assert the spec's literal limit so widening it fails a test.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `go/handlers/validation` · harmful: 0
- features: company-settings
- evidence: internal/api/company_settings_handler_test.go:314 (mutant M1: maxLogoBytes 10<<20 -> 100<<20 survived) (go/handlers/validation)
- last seen: 2026-08-23T15:02:30Z

### L-007 - When a task's Done-when list is narrower than its layer's Test Coverage Matrix row, treat the matrix as binding and add the missing status-code case.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `tasks/coverage-matrix` · harmful: 0
- features: company-settings
- evidence: SET-13 (no test for uploads.Save failure -> 500) (tasks/coverage-matrix)
- last seen: 2026-08-23T15:02:31Z

### L-008 - Verify a stdlib sniffing/parsing helper actually supports every format the spec allowlists before designing validation around it.
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `go/design/mime` · harmful: 0
- features: company-settings
- evidence: internal/api/company_settings_handler.go:189 SPEC_DEVIATION (http.DetectContentType has no SVG signature) (go/design/mime)
- last seen: 2026-08-23T15:02:31Z

### L-009 - MSW handlers under jsdom cannot read a multipart body, so verify upload field-name/payload contracts by inspecting the FormData in-process instead of in the handler.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `web/msw/uploads` · harmful: 0
- features: company-settings
- evidence: web/src/test/msw/handlers.ts:566 (multipart body unreadable under jsdom; formData() and text() both hang) (web/msw/uploads)
- last seen: 2026-08-23T15:02:31Z

### L-010 - When an AC names two or more views (list AND detail), allocate a task per view: fixing only one leaves the AC half-met and the untouched view's existing test silently pins the forbidden behavior.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `web/src/features` · harmful: 0
- features: status-page-domain-attach
- evidence: SPD-12/SPD-13 - web/src/features/status-pages/StatusPagesSection.tsx:74-79 (web/src/features)
- last seen: 2026-08-23T18:50:39Z

### L-011 - A negative DOM assertion (not.toContain) only discriminates if the fixture actually reaches the guarded code path; assert it on a fixture that would render the bad value, not on one where the branch is never entered.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `web/src frontend tests` · harmful: 0
- features: status-page-domain-attach
- evidence: mutation 5 - StatusPagesSection.tsx:20 + StatusPageDetail.tsx:12 null-safety guards deleted, tests still passed (web/src frontend tests)
- last seen: 2026-08-23T18:50:39Z

### L-012 - A goroutine-based concurrency test usually serializes and passes even with the lock removed: force contention deterministically (hold a competing transaction, or inject a hook between the read and the write) instead of trusting scheduling.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `concurrency tests / DB write paths` · harmful: 0
- features: status-page-domain-attach
- evidence: mutation 1 - internal/db/status_page_repository.go:110 FOR UPDATE removed, TestAttachDomain_Concurrent passed 20/20 under -race (concurrency tests / DB write paths)
- last seen: 2026-08-23T19:06:09Z

### L-013 - When a spec's described mechanism turns out to be wrong, grep the whole spec for the old wording before declaring the fix done: the same mechanism is usually restated in the Edge Cases or Success Criteria section and a half-corrected spec re-invites the exact regression it caused.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `.specs/features/*/spec.md` · harmful: 0
- features: status-page-domain-attach
- evidence: .specs/features/status-page-domain-attach/spec.md:102 (.specs/features/*/spec.md)
- last seen: 2026-08-23T19:20:45Z

### L-014 - When a spec edge case says 'first-ever X fails leaves no side effect', add a test starting from zero prior state, not just a test that seeds success before the failure.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `internal/poller` · harmful: 0
- features: service-status-intervals
- evidence: internal/poller/poller_test.go:137 (internal/poller)
- last seen: 2026-08-24T18:50:32Z

### L-015 - When multiple packages each add their own full-table clear/restore helper for a shared integration-test table, serialize them with a cross-process Postgres advisory lock (mirror internal/dbtest/lock.go's LockDatadogIntegration) - otherwise go test ./...'s default cross-package parallelism races their clear windows against other packages' tests on the same table, and it's flaky, not deterministic.
- signal: `gate_fail` · recurrence: 1 feature(s) · scope: `internal/dbtest` · harmful: 0
- features: self-hosted-docker-bootstrap
- evidence: internal/db/admin_repository_test.go:330 (internal/dbtest)
- last seen: 2026-08-24T21:06:25Z

### L-016 - A tasks.md Requirement Coverage table's own summary count (e.g. '22 total, 22 mapped') is not self-verifying prose - cross-check it against every task's Requirement field, since a build-tooling AC (frontend-build convention) and a Dockerfile-shape AC were both silently missing from the coverage summary despite both being correctly implemented.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `tasks.md` · harmful: 0
- features: self-hosted-docker-bootstrap
- evidence: tasks.md Requirement Coverage section (tasks.md)
- last seen: 2026-08-24T21:06:25Z

### L-017 - TestPublicStatusPreview_PublishedPage_200Unaffected has been reported once as a rare full-suite-parallelism flake (500, unreproduced in isolation and unreproduced across 16 fresh -count=1 full-gate runs by the next Verifier) in a file untouched by the feature under test - treat as a known, non-blocking, unconfirmed flake candidate for internal/api, not a gate blocker, until it recurs with enough evidence to investigate further.
- signal: `gate_fail` · recurrence: 1 feature(s) · scope: `internal/api` · harmful: 0
- features: self-hosted-docker-bootstrap
- evidence: internal/api/public_status_preview_handler_test.go (TestPublicStatusPreview_PublishedPage_200Unaffected) (internal/api)
- last seen: 2026-08-24T23:44:47Z

### L-018 - A test double for an outbound side effect must capture and assert the payload, not just count invocations.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `test-doubles` · harmful: 0
- features: admin-invite-resend-cancel
- evidence: internal/api/admins_test.go:75 (fakeEmailProvider.Send discards email.Message; sensor mutant 4 survived) (test-doubles)
- last seen: 2026-08-28T19:26:31Z

### L-019 - A path parameter bound to a typed database column must be validated in the handler; an invalid literal raises a driver error, not a no-rows result.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: admin-invite-resend-cancel
- evidence: internal/api/admins.go:314 (non-UUID id yields a driver error, handler returns 500; spec Edge Case requires 404) (routes)
- last seen: 2026-08-28T19:26:31Z

### L-020 - When an acceptance criterion says a state variant behaves identically, write a test that actually seeds that variant.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `test-coverage` · harmful: 0
- features: admin-invite-resend-cancel
- evidence: internal/api/admins_test.go:1177 (every resend test seeds a future TTL; P2 AC2 expired-invite resend uncovered) (test-coverage)
- last seen: 2026-08-28T19:26:31Z

### L-021 - Assert the recomputed value of a refreshed timestamp against its documented TTL, not merely that the operation returned success.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `api` · harmful: 0
- features: admin-invite-resend-cancel
- evidence: internal/api/admins_test.go:1181 (resend asserts status and body but never the recomputed expires_at window) (api)
- last seen: 2026-08-28T19:26:31Z

### L-022 - A test asserting the exact contents of a shared singleton row must reset that row before asserting, not only on cleanup.
- signal: `gate_fail` · recurrence: 1 feature(s) · scope: `integration-tests` · harmful: 0
- features: admin-invite-resend-cancel
- evidence: internal/db/company_settings_migration_test.go:56 (asserts the shared singleton is blank without resetting it first; fails at baseline fa661cc too) (integration-tests)
- last seen: 2026-08-28T19:26:31Z

### L-023 - Assert a submit control's disabled state while the request is still in flight, not just that the success side effect eventually fired
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `web` · harmful: 0
- features: accept-invite-page
- evidence: validation.md sensor M6 - web/src/features/auth/AcceptInvitePage.test.tsx:113 (web)
- last seen: 2026-08-28T20:06:59Z

### L-024 - Verify that a stale validation error is cleared by submitting the form a second time, not by asserting the first error appeared
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `web` · harmful: 0
- features: accept-invite-page
- evidence: validation.md sensor M8 - web/src/features/auth/AcceptInvitePage.tsx:27 (web)
- last seen: 2026-08-28T20:06:59Z

### L-025 - Assert a never-leak requirement explicitly by checking the secret is absent from the rendered DOM and from console output, never by code inspection alone
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `web` · harmful: 0
- features: accept-invite-page
- evidence: AIP-04 - spec.md:55 (web)
- last seen: 2026-08-28T20:06:59Z

### L-026 - When the API client converts every non-2xx response into a typed error, never attribute 5xx to the untyped-error fallback branch in a spec or a test
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `web` · harmful: 0
- features: accept-invite-page
- evidence: AIP-09 - spec.md:86 vs web/src/lib/apiClient.ts:82 (web)
- last seen: 2026-08-28T20:06:59Z

### L-027 - A Postgres-computed threshold derived from now()-vs-stored-timestamp arithmetic needs a test that seeds the exact boundary via SQL (zero refill rate or same-transaction now()), not a wall-clock-timed test call - real elapsed time always nudges the value past an exact-equality boundary before it's re-read.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `internal/ratelimit` · harmful: 0
- features: ha-multi-replica
- evidence: internal/ratelimit/postgres_bucket_store.go:69 (internal/ratelimit)
- last seen: 2026-08-31T00:48:06Z

### L-028 - A Release/Unlock method that both signals release AND closes its own connection can mask a key/salt mismatch, because connection teardown auto-releases session-scoped state regardless of whether the explicit release targeted the right key - always check the release call's own success signal (e.g. pg_advisory_unlock's boolean return) instead of relying on teardown as an implicit safety net.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `internal/pglock` · harmful: 0
- features: ha-multi-replica
- evidence: internal/pglock/pglock.go:106-125 (internal/pglock)
- last seen: 2026-08-31T00:48:06Z

### L-029 - When Design reinterprets a spec AC's mechanism (e.g. per-cycle lease renewal simplified to an independent heartbeat ticker) for a good documented reason, update the AC's own wording in spec.md in the same pass - a Tech Decisions table entry in design.md is not enough, the Verifier checks spec.md's literal text against the code.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `spec-writing` · harmful: 0
- features: ha-multi-replica
- evidence: HA-03 (spec-writing)
- last seen: 2026-08-31T00:48:06Z

### L-030 - An AC framed around 'abort without partial writes' needs a test with real multi-item state and a real out-of-band failure trigger, not a single boolean flag assertion - a flag-only test can pass while the actual multi-step write path it claims to cover is never exercised under the failure condition.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `testing` · harmful: 0
- features: ha-multi-replica
- evidence: HA-05 (testing)
- last seen: 2026-08-31T00:48:06Z

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
