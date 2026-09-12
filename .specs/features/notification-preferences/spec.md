# Notification Preferences and Incident Email Delivery Specification

**Scope: Large.** The event-driven toggles (incident opened/resolved) are Medium-shaped (reuse the existing synchronous send pattern), but the weekly digest requires a new periodic-execution mechanism with multi-replica leader dedup — real architecture work, not CRUD. A formal Design phase runs before Execute for the digest story; the two event-driven stories can implement inline.

## Problem Statement

The redesigned Meu Perfil screen (`handoff-new-layout/Meu Perfil.dc.html`) shows three notification toggles: "Novo incidente", "Incidente resolvido", and "Resumo semanal". Today no `NotificationPreference` model exists, and — more importantly — nothing in this codebase ever sends an email when an incident opens or resolves (`internal/email/service.go` has exactly three fixed-purpose methods: `SendAdminInvite`, `SendSignupVerification`, `SendPasswordReset`). A preference toggle with no real send behind it would be decorative UI. This spec adds the preference model, wires real email delivery into the two incident lifecycle events, and adds a weekly digest as a new scheduled job. It also builds the `Notificações` section of Meu Perfil itself (the three toggles), which `profile-page` deferred on purpose — without it the endpoints have no UI to drive them.

## Goals

- [ ] Every user has a stored, readable/writable preference per notification type (`incident_opened`, `incident_resolved`, `weekly_digest`), defaulting to the mock's own defaults (first two on, digest off).
- [ ] When any incident is created in a tenant, every `owner`/`operator` member of that tenant with `incident_opened` enabled receives a real email.
- [ ] When any incident's status becomes `resolved` (via either `Transition` or `ConfirmClose`), every `owner`/`operator` member with `incident_resolved` enabled receives a real email.
- [ ] Once a week, every `owner`/`operator` member with `weekly_digest` enabled receives a real email summarizing uptime and incident activity for their tenant — sent exactly once per tenant per week, even with multiple running replicas.
- [ ] A failed email send never blocks or fails the incident action itself (matches the existing non-fatal send pattern already used elsewhere).
- [ ] The Meu Perfil screen has a working `Notificações` section whose three toggles read and write the calling user's preferences (`GET`/`PATCH /api/auth/notification-preferences`), shipped in pt-BR and English.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Notifying `viewer`-role members | Decision confirmed with the user: only `owner`/`operator` receive these emails — notifications here are framed around incident response action, which is what owner/operator do; a viewer already sees incidents live on the dashboard without needing a push. |
| In-app notification center / bell icon | Covered by the separate, already-deferred `Notificações` screen spec (`.specs/features/new-layout-migration/gap-analysis.md` §11) — this spec is email-only. |
| Per-service "follow" scoping (only notify about services I care about) | The mock's toggle is a flat "any incident" switch, not a per-service subscription; no per-service follow model exists or is requested. |
| Any other notification type beyond the three the mock shows | Domain/status-page/billing events are explicitly out of this cycle per the gap analysis; adding notification types for them now would be speculative. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Preference storage | New `notification_preferences` table: `user_id` (FK), `notification_type` (`incident_opened`/`incident_resolved`/`weekly_digest`, CHECK constraint), `enabled` (bool), unique on `(user_id, notification_type)` | One narrow, queryable row per (user, type) — matches the mock's per-toggle model exactly, no JSON blob to parse. | y — agent default, no objection raised |
| Default values for a user with no row yet | `incident_opened: true`, `incident_resolved: true`, `weekly_digest: false` — a missing row is read as these defaults, not as "notifications off" | Matches the mock's own seeded state (`NOTIF_DEFS` defaults in the handoff) and existing users (created before this feature existed) get sensible defaults without a backfill migration writing a row per existing user. | y — agent default, no objection raised |
| Recipient scope for event-driven notifications | `TenantMembershipRepository.ListForTenant(tenantID)`, filtered to `role IN (owner, operator)`, then filtered again to those with the relevant preference enabled (default-applied per the row above) | User decision: `viewer` excluded. `ListForTenant` already exists and returns every membership row for a tenant — no new repository method needed for the membership side. | y |
| Send mechanism for event-driven notifications | Reuses `internal/email/service.go`'s existing active-provider-lookup-and-send pattern (a new `Service.SendIncidentOpened`/`SendIncidentResolved` method, mirroring `SendSignupVerification`'s shape), called synchronously from the handler right after the state change commits, exactly like `SignupHandler` already treats `SendSignupVerification` as non-fatal | No queue or async worker exists anywhere in this codebase; introducing one for two email types is disproportionate. A slow provider call adding latency to `POST /api/incidents` is an accepted, pre-existing tradeoff (the same one `SendSignupVerification` already has). | y — agent default, no objection raised |
| Hook points for event-driven sends | `IncidentsHandler.Create` (after a successful create, "opened"); `IncidentsHandler.Transition` when `req.Status == "resolved"`, and `IncidentsHandler.ConfirmClose` (unconditionally, since it always ends in resolved) — both are "resolved" hook points, since resolution is reachable by two different handler methods today | Matches the two real code paths that can set `status = "resolved"` (`internal/db/incident_repository.go`'s `Transition` and `ConfirmPendingClose`), confirmed by re-reading the handler in this same spec-writing session (same discipline as the earlier `incident-severity-and-timeline` correction — verify against the actual code, not a partial read). | y — agent default, no objection raised |
| Weekly digest scheduling mechanism | A new in-process scheduler goroutine, started at boot alongside `PollerManager`, computing the next Monday 00:00 **UTC** and firing then and every 7 days after | No per-tenant timezone field exists anywhere (`Tenant` has `Locale` but no `timezone` column — confirmed by re-reading `tenant_repository.go`), so a per-tenant-local "Monday morning" is not computable today. UTC is a documented, explicit default, not a silent guess. Adding a per-tenant timezone field is a larger change belonging to a future spec if this default proves wrong for real users. | y — agent default, no objection raised |
| Weekly digest multi-replica dedup | A second, distinct Postgres advisory-lock key (not `pollerLeaderLockKey` — a different fixed integer constant) acquired via the same `internal/pglock` package `PollerManager` already uses, held by whichever replica leads the digest job; only the leader fires the weekly send | Reuses the exact, already-verified (`ha-multi-replica`, AD-013) leader-election primitive instead of inventing a second HA mechanism — this codebase already solved "exactly one replica does X" once, and that solution is a generic package (`internal/pglock`), not poller-specific. | y — agent default, no objection raised |
| Weekly digest content | Per tenant: uptime summary (derived from `status_intervals`, same data `poller-status-real-state` reads) and count of incidents opened/resolved in the last 7 days (`incidents` filtered by `created_at`/`resolved_at`) | Directly answers "uptime and principais eventos da semana" from the mock's own toggle description, using only data that already exists — no new metric invented. | y — agent default, no objection raised |
| Read/write endpoints | `GET /api/auth/notification-preferences` (self, returns all 3 types with resolved values, defaults applied) and `PATCH /api/auth/notification-preferences` (self, body `{incident_opened?, incident_resolved?, weekly_digest?}`, partial update — omitted keys unchanged) | Matches `profile-self-service`'s self-scoped pattern; partial-update body matches how the mock's individual toggles fire one change at a time, not a full-form submit. | y — agent default, no objection raised |
| RBAC | `anyRole`, self only, on both endpoints | Personal preference, same posture as every other Meu Perfil endpoint this cycle. | y — agent default, no objection raised |
| Frontend placement | New `web/src/features/notifications/` module (`NotificationsSection` + hooks + types), consumed by `ProfilePage`, mirroring `features/sessions/` and `features/two-factor/` | User decision (2026-09-12): keeps `features/profile/` focused on its own cards and gives the notification UI an independently testable boundary; the alternative (a card inside `features/profile/`) grows that module for no benefit. | y (user) |
| Toggle interaction | Each toggle fires its own optimistic `PATCH` of a single key; on failure the toggle reverts and shows an error toast, leaving the other toggles untouched | Matches the mock's per-toggle behavior and this spec's partial-update contract; a full-form save would contradict the individual toggle model. | y (user) |
| Frontend testing | Component + hook tests via vitest; MSW handlers for `GET`/`PATCH /api/auth/notification-preferences` mirroring the backend response shape (defaults applied) | `AGENTS.md` §5 requires MSW mocks to mirror the real backend shape; hooks own the network call so tests intercept at MSW, consistent with every other feature this cycle. | y — agent default, no objection raised |
| i18n | All new user-facing strings under `profile.notifications.*` in both pt-BR and en | `AGENTS.md` §5: no hardcoded strings; the app ships both locales. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: User manages their own notification preferences ⭐ MVP

**User Story**: As any authenticated user, I want to turn each notification type on or off for myself, so I only get emails I actually want.

**Why P1**: Every other story in this spec is meaningless without a real place to read/write the toggle state the mock shows.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `GET /api/auth/notification-preferences` THEN the system SHALL respond `200` with all three types, applying the documented defaults for any type with no stored row.
2. WHEN an authenticated user calls `PATCH /api/auth/notification-preferences` with one or more of `{incident_opened, incident_resolved, weekly_digest}` THEN the system SHALL upsert exactly those rows for that user and leave any omitted type unchanged.
3. The system SHALL scope both endpoints to the caller's own preferences — no request body or parameter can target another user's rows.

**Independent Test**: `GET` for a brand-new user with no rows, confirm the documented defaults; `PATCH {weekly_digest: true}`, `GET` again and confirm only `weekly_digest` changed.

---

### P1: Notification preferences are edited from Meu Perfil ⭐ MVP

**User Story**: As an authenticated user, I want a `Notificações` section on the Meu Perfil screen with the three toggles, so I can manage my preferences without leaving the dashboard or touching an API.

**Why P1**: The endpoints only have value with a UI to drive them; `profile-page` deliberately deferred this section, and this feature closes that gap in the same cycle.

**Acceptance Criteria**:

1. WHEN an authenticated user opens Meu Perfil THEN the system SHALL render a `Notificações` section with three toggles (novo incidente, incidente resolvido, resumo semanal) reflecting the values returned by `GET /api/auth/notification-preferences`. <!-- event-driven -->
2. WHEN the user flips one toggle THEN the system SHALL send a `PATCH` for that single key and reflect the new state immediately, reverting the toggle and surfacing an error toast if the request fails. <!-- event-driven -->
3. WHILE the initial preferences request is in flight THEN the section SHALL show a loading state with its toggles disabled. <!-- state-driven -->
4. IF the initial preferences request fails THEN the section SHALL show an inline error state without affecting the other Meu Perfil cards. <!-- unwanted-behavior -->

**Independent Test**: Render Meu Perfil against MSW with a user at defaults; confirm the three toggles match, flip "resumo semanal" and confirm exactly one PATCH carrying `{weekly_digest: true}`, then make MSW return 500 and confirm the toggle reverts with a toast.

---

### P1: Tenant owners/operators are emailed when an incident opens ⭐ MVP

**User Story**: As an owner/operator who opted in, I want an email the moment any incident opens in my tenant, so I find out even if I'm not looking at the dashboard.

**Why P1**: This is the concrete behavior the "Novo incidente" toggle promises; without it the toggle does nothing.

**Acceptance Criteria**:

1. WHEN `POST /api/incidents` successfully creates an incident THEN the system SHALL send an email to every `owner`/`operator` member of that tenant whose `incident_opened` preference resolves to `true` (stored or default).
2. IF sending fails for the active email provider (matches `SendSignupVerification`'s existing failure semantics) THEN the system SHALL still respond `201` for the incident creation — the email failure SHALL NOT roll back or fail the incident creation.
3. The system SHALL NOT email a member whose `incident_opened` preference resolves to `false`.
4. The system SHALL NOT email a `viewer`-role member regardless of their preference value.

**Independent Test**: Seed a tenant with an owner (`incident_opened: true`), an operator (`incident_opened: false`), and a viewer (`incident_opened: true`), create an incident, confirm exactly one email was sent (to the owner). Disconnect/invalidate the active email provider and confirm incident creation still returns `201`.

---

### P1: Tenant owners/operators are emailed when an incident resolves ⭐ MVP

**User Story**: As an owner/operator who opted in, I want an email when an incident I might have been tracking gets resolved, so I know it's over without re-checking the dashboard.

**Why P1**: Mirrors the opened-notification story for the "Incidente resolvido" toggle; both are needed for the screen's two event toggles to be real.

**Acceptance Criteria**:

1. WHEN `PATCH /api/incidents/{id}` (`Transition`) sets an incident's status to `resolved` THEN the system SHALL send an email to every `owner`/`operator` member of that tenant whose `incident_resolved` preference resolves to `true`.
2. WHEN `POST /api/incidents/{id}/confirm-close` (`ConfirmClose`) resolves an incident THEN the system SHALL send the same notification, using the same recipient rule.
3. The system SHALL NOT send this notification for any other status transition (e.g. `investigating` → `identified`).

**Independent Test**: Transition an incident directly to `resolved` and confirm the email fires; separately, run the AI-assisted `ConfirmClose` flow on a different incident and confirm the same notification fires; transition a third incident `investigating` → `identified` and confirm no email fires.

---

### P2: Tenant owners/operators receive a weekly digest email

**User Story**: As an owner/operator who opted in, I want a weekly summary of uptime and incident activity, so I get a periodic pulse-check without watching the dashboard daily.

**Why P2**: Real value, but the least urgent of the three toggles (the mock defaults it to off) and the only one requiring new scheduling infrastructure rather than reusing the existing send-on-request pattern.

**Acceptance Criteria**:

1. WHEN the weekly digest job fires (Monday 00:00 UTC, once per 7-day period) THEN the system SHALL send one email per tenant to every `owner`/`operator` member with `weekly_digest` enabled, containing that tenant's uptime summary and incident counts for the prior 7 days.
2. WHILE more than one replica is running THE system SHALL ensure the digest for a given tenant and week is sent exactly once — dedup via the advisory-lock leader mechanism, not via a "did I already send this" table.
3. IF a tenant has zero `owner`/`operator` members with `weekly_digest` enabled THEN the system SHALL send no email for that tenant that week — not an error, not an empty email.

**Independent Test**: Run two `PollerManager`-style instances against the same database with the digest scheduler active, advance/trigger the weekly fire, confirm exactly one instance's send executes (assert via a test hook or log count, not by asserting real wall-clock delay) and no duplicate emails are attempted by the non-leader.

---

## Edge Cases

- IF the active email provider is disconnected (`ErrNoActiveProvider`) THEN every notification type in this spec SHALL fail exactly the way `SendSignupVerification` already fails today — logged, non-fatal to the triggering action, no retry queue.
- WHEN an incident is auto-created (`AutoCreated = true`, from `SLOAnalyzer`) THEN the "incident opened" notification SHALL still fire — the mock's toggle says "quando um incidente é aberto em qualquer serviço", with no carve-out for how it was opened.
- WHEN a user is removed from a tenant (`TenantMembershipRepository.Delete`) THEN they SHALL no longer receive that tenant's notifications on the very next event, since `ListForTenant` no longer returns them — no separate cleanup of their `notification_preferences` row is needed (it's a per-user row, not per-membership, and stays valid if they're re-added later or belong to another tenant).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| NOTIFPREF-01 | P1: Manage own preferences (read with defaults) | T1, T2, T3 | Implementing |
| NOTIFPREF-02 | P1: Manage own preferences (partial update) | T2, T3 | Implementing |
| NOTIFPREF-03 | P1: Manage own preferences (self-scoped) | T2, T3 | Implementing |
| NOTIFPREF-04 | P1: Emailed on incident opened | T4, T5, T6 | Implementing |
| NOTIFPREF-05 | P1: Opened email non-fatal on send failure | T4, T5, T6 | Implementing |
| NOTIFPREF-06 | P1: Opened email respects preference/role | T5, T6 | Implementing |
| NOTIFPREF-07 | P1: Emailed on incident resolved (Transition) | T4, T5, T6 | Implementing |
| NOTIFPREF-08 | P1: Emailed on incident resolved (ConfirmClose) | T4, T5, T6 | Implementing |
| NOTIFPREF-09 | P1: No email on non-resolving transition | T5, T6 | Implementing |
| NOTIFPREF-10 | P2: Weekly digest sent per tenant | T7, T8, T9 | Pending |
| NOTIFPREF-11 | P2: Weekly digest deduped across replicas | T8 | Pending |
| NOTIFPREF-12 | P2: No digest email when nobody opted in | T7, T8 | Pending |
| NOTIFPREF-13 | P1: Notifications section reflects stored preferences | T10, T12, T13 | Pending |
| NOTIFPREF-14 | P1: Single-key optimistic toggle with revert on failure | T10, T12 | Pending |
| NOTIFPREF-15 | P1: Loading state while preferences load | T12 | Pending |
| NOTIFPREF-16 | P1: Inline error state does not break the page | T12 | Pending |

**Coverage:** 16 total, 16 mapped to tasks, 0 unmapped

---

## Success Criteria

- [ ] Preferences are per-user, per-type, with documented defaults for users with no stored row.
- [ ] Incident-opened and incident-resolved emails reach every opted-in owner/operator, never a viewer, and never block the triggering action on send failure.
- [ ] The weekly digest fires exactly once per tenant per week even with multiple replicas running.
- [ ] No new queue/worker infrastructure is introduced for the two event-driven sends.
- [ ] The `Notificações` section on Meu Perfil reads and writes the caller's preferences with per-toggle optimistic updates, and is localized in pt-BR and English.
