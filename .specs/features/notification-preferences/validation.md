# notification-preferences Validation

**Result**: PASS

**Date**: 2026-09-12
**Spec**: `.specs/features/notification-preferences/spec.md`
**Diff range**: `13aa241..60dcc91` (15 feature commits; 13 tasks + 2 validation fixes)
**Verifier**: standalone fresh-eyes pass (author ≠ verifier not achievable in this harness - only read-only sub-agents are exposed, so the `validate.md` standalone fallback was used; limitation recorded here)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | migration 0032; applies + reverses cleanly on a disposable Postgres |
| T2   | ✅ Done | `NotificationPreferenceRepository`; 9 integration tests |
| T3   | ✅ Done | self-service GET/PATCH endpoints + routes; 8 integration tests |
| T4   | ✅ Done | `SendIncidentOpened`/`Resolved` + templates; 6 unit tests |
| T5   | ✅ Done | `internal/notify` `NotificationService`; 11 unit tests (9 incident + 2 digest) |
| T6   | ✅ Done | handler hooks + auto-created (poller) hook; 9 tests (5 api + 4 poller) |
| T7   | ✅ Done | `SendWeeklyDigest` + pure assembly; 8 tests (5 email + 3 digest) |
| T8   | ✅ Done | `DigestScheduler` + leader election; 6 tests (5 cli + 1 db) |
| T9   | ✅ Done | boot wiring; 1 test |
| T10  | ✅ Done | frontend hooks + MSW; 5 tests |
| T11  | ✅ Done | `Switch` primitive; 5 tests |
| T12  | ✅ Done | `NotificationsSection` + i18n; 6 tests |
| T13  | ✅ Done | `ProfilePage` wiring; page test extended |

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| NOTIFPREF-01 read defaults | incident_opened/resolved true, weekly_digest false | `internal/api/auth_handler_test.go:1976` - `resp.IncidentOpened == true && resp.WeeklyDigest == false`; `internal/db/notification_preference_repository_test.go:160` - `opened[userID] == true`, `digest[userID] == false` | ✅ PASS |
| NOTIFPREF-02 partial update | only provided keys change | `internal/api/auth_handler_test.go:2034` - GET after PATCH keeps `incident_opened == false`; `internal/db/notification_preference_repository_test.go:110` - omitted key stays true | ✅ PASS |
| NOTIFPREF-03 self-scoped | no field targets another user | `internal/api/auth_handler_test.go:2095` - user B's `weekly_digest == false` after A's PATCH | ✅ PASS |
| NOTIFPREF-04 emailed on opened | owner/operator with pref true emailed | `internal/notify/service_test.go:94` - exactly `[owner-on@example.com]`; `internal/api/incidents_handler_test.go:1240` - summary.IncidentID==created.ID; `internal/poller/analyzer_test.go:645` - auto-created notifies tenant-1 | ✅ PASS |
| NOTIFPREF-05 non-fatal send failure | action still succeeds | `internal/api/incidents_handler_test.go:1278` - 201 with `openErr`; `internal/notify/service_test.go:162` - call returns nil, other recipient still emailed; `internal/email/service_test.go:550` - `ErrNoActiveProvider`, 0 sends | ✅ PASS |
| NOTIFPREF-06 pref/role filter | viewer never emailed | `internal/notify/service_test.go:94` - viewer-on not sent; `:142` - all-disabled → no send | ✅ PASS |
| NOTIFPREF-07 resolved via Transition | resolved notification fires | `internal/api/incidents_handler_test.go:1310` - 1 resolved summary, IncidentID match | ✅ PASS |
| NOTIFPREF-08 resolved via ConfirmClose | same notification fires | `internal/api/incidents_handler_test.go:1352` - 1 resolved summary | ✅ PASS |
| NOTIFPREF-09 no email on non-resolving transition | no resolved notification | `internal/api/incidents_handler_test.go:1334` - 0 resolved notifications for `identified` | ✅ PASS |
| NOTIFPREF-10 digest per tenant | 1 email/recipient with uptime + counts | `internal/cli/digest_scheduler_test.go:159` - 2 sends, counts 4/3, period; `internal/email/service_test.go:749` - body has 99.95% + counts; `internal/notify/digest_test.go:8` - mean 75 | ✅ PASS |
| NOTIFPREF-11 dedup across replicas | exactly one replica runs | `internal/cli/digest_scheduler_test.go:96` - follower's tenants.List calls == 0, leader == 1 | ✅ PASS |
| NOTIFPREF-12 no digest when nobody opted in | no email, no content | `internal/cli/digest_scheduler_test.go:134` - content builder 0 calls, 0 sends; `internal/notify/service_test.go:306` - empty recipient list | ✅ PASS |
| NOTIFPREF-13 section reflects stored prefs | three toggles match | `web/src/features/notifications/NotificationsSection.test.tsx:36` - aria-checked true/true/false; `hooks.test.ts:31` - defaults object | ✅ PASS |
| NOTIFPREF-14 single-key optimistic + revert | PATCH one key; revert on failure | `NotificationsSection.test.tsx:97` - captured body `{weekly_digest:true}`; `hooks.test.ts:76` - optimistic true then false through a gated failure | ✅ PASS |
| NOTIFPREF-15 loading disables toggles | switches disabled in flight | `NotificationsSection.test.tsx:62` - `toBeDisabled()` | ✅ PASS |
| NOTIFPREF-16 inline error doesn't break page | role=alert, rest intact | `NotificationsSection.test.tsx:80` - alert text + switches disabled | ✅ PASS |

**Status**: ✅ All 16 ACs covered with spec-matched outcomes; no spec-precision gaps.

---

## Discrimination Sensor

Scratch `git worktree` at HEAD (node_modules symlinked for the frontend); each mutation reverted in the scratch; real-tree `git status --porcelain` confirmed empty before and after.

| # | File:line | Mutation | Killed? |
| - | --- | --- | --- |
| 1 | `internal/db/notification_preference_repository.go` | flipped the resolved default (`notificationDefaultEnabled` → negated) | ✅ Killed |
| 2 | `internal/notify/service.go` | removed the owner/operator recipient filter | ✅ Killed |
| 3 | `internal/notify/service.go` | inverted the enabled-preference filter | ✅ Killed |
| 4 | `internal/api/incidents_handler.go` | flipped `req.Status == "resolved"` | ✅ Killed |
| 5 | `internal/poller/analyzer.go` | disabled the auto-created notifier guard | ✅ Killed |
| 6 | `internal/cli/digest_scheduler.go` | removed the zero-recipients early return | ✅ Killed |
| 7 | `web/src/components/ui/Switch.tsx` | inverted `aria-checked` | ✅ Killed |
| 8 | `web/src/features/notifications/hooks.ts` | removed the optimistic rollback (`onError`) | ❌ Survived → fixed → ✅ Killed |
| 9 | `web/src/features/notifications/NotificationsSection.tsx` | forced `disabled = false` | ✅ Killed |

**Sensor depth**: lightweight-plus (9 targeted behavior-level mutations, backend + frontend).
**Result**: 9/9 killed after the M8 fix. M8 initially survived because the rollback test asserted the post-failure value equal to the pre-mutation value; the test was strengthened to hold the failure behind a gate, observe the optimistic `true`, then the rolled-back `false` (commit `3f4e4df`), and M8 was re-run and killed.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code / no speculative features | ✅ |
| Surgical changes (files scoped to each task) | ✅ |
| Matches existing patterns (repository/handler/hook/MSW shapes) | ✅ |
| Spec-anchored outcome check | ✅ |
| Per-layer coverage (routes happy+edge+error; repo integration; domain unit) | ✅ |
| Every test maps to an AC / edge case / done-when (no unclaimed tests) | ✅ |
| Guidelines: `AGENTS.md` (§3 gates, §4 backend, §5 frontend/MSW, §7 risk) | ✅ |

**Deliberate refinements vs. the original design** (documented in `design.md`):
- `ListMembersWithEmail` was added to `TenantMembershipRepository` (the design's `ListForTenant` returns no email, so the fan-out needed one).
- Auto-created incidents are notified from `SLOAnalyzer` via a per-tenant context value + `SetNotifier` (the design only covered the HTTP handler; the spec edge case requires auto-created incidents to notify too).

---

## Edge Cases

- [x] `ErrNoActiveProvider` / send failure is non-fatal to the triggering action - covered (`incidents_handler_test.go:1278`, `notify/service_test.go:162`, `email/service_test.go:550`).
- [x] Auto-created incident (`AutoCreated=true`, SLOAnalyzer) still notifies - covered (`poller/analyzer_test.go:645`).
- [x] A member removed from the tenant no longer receives notifications - covered (`db/tenant_membership_repository_test.go:442`), added during validation.

---

## Gate Check

- **Backend gate**: `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./...` on a fresh disposable Postgres → all packages `ok`, 0 failed, 0 skipped. `go build ./...` and `go vet ./...` clean.
- **Frontend gate**: `cd web && npx tsc -b --noEmit && npm run test` → `tsc` clean, 75 files / 413 tests passed, 0 failed.
- **Test count before feature**: frontend 397.
- **Test count after feature**: frontend 413 (+16); backend ~59 new tests across `internal/db`, `internal/api`, `internal/notify`, `internal/email`, `internal/poller`, `internal/cli`.
- **Skipped tests**: none.
- **Failures**: none.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| NOTIFPREF-01..16 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 16/16 ACs matched the spec-defined outcome; 0 spec-precision gaps.
**Sensor**: 9/9 mutations killed (after the M8 test-strengthening fix).
**Gate**: backend integration suite green on a fresh disposable DB; frontend 413/413.

**What works**: per-user notification preferences with defaults and a self-scoped partial update; real incident-opened/resolved emails to opted-in owner/operators (manual, auto-created, and both resolve paths), non-fatal on provider failure; a weekly digest sent exactly once per tenant per replica set, with uptime (reusing `history.UptimePercent`) and incident counts; and the Meu Perfil `Notificações` section with optimistic per-toggle updates, loading/error states, and pt-BR/en copy.

**Issues found**: none outstanding. Two refinements (new repository method, poller auto-created hook) were made deliberately and recorded in `design.md`.

**Next steps**: none required for this feature. Optional: cut a release from `develop`.
