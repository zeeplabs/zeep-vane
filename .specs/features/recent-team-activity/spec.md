# Recent Team Activity Specification

## Problem Statement

`OverviewPage.tsx`'s "Atividade recente do time" card is a static placeholder (`ACTIVITY_FEED`, hardcoded array), shipped with an explicit "no `ActivityEvent` model exists yet" decision on 2026-09-14. A real audit trail already exists (`internal/audit.Log`, `admin_audit_log` table) recording 8 administrative actions — invites, role changes, removals, domain/status-page verification and deletion — but has no read endpoint and no way to render a human-readable target name (the table's `target_id` is a bare UUID with no type discriminator). This feature wires the card to real data: a new `target_label` column captured at write time, a new read endpoint, and the frontend swapped from the mock array to a live query.

## Goals

- [ ] "Atividade recente do time" shows the tenant's real last 5 administrative actions, not mock data.
- [ ] Every existing `audit.Log.Record` call site (8 total) is updated to also capture a human-readable target label at the moment it already has that information in hand — never a later best-effort join.
- [ ] A deleted target (domain, status page, removed admin) still renders correctly in the history — the label is a snapshot, not a live lookup.
- [ ] An unrecognized action string never crashes the endpoint or the card — falls back to a generic phrase.

## Out of Scope

| Feature | Reason |
| --- | --- |
| A dedicated, paginated audit-log screen | User confirmed via discuss: this feature only feeds the Overview summary card (last 5, no pagination) — a full audit browser is a separate future feature. |
| `target_type` column / runtime join to resolve target names | User confirmed via discuss: `target_label` (write-time snapshot) instead — survives target deletion, avoids a fragile multi-table join. |
| Retroactively backfilling `target_label` for historical rows | Existing rows predate this feature and have no label source to derive it from; they render via the generic fallback phrase (target-less). No data migration/backfill attempted. |
| Role-gating the card | User confirmed via discuss: stays visible to any authenticated role, matching the card's current (mock) visibility. |
| New actions beyond the 8 already recorded | No new call sites added elsewhere in the app — this feature only touches the 8 existing `Record` call sites. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Target name resolution strategy | New `target_label TEXT` column on `admin_audit_log`, populated by the caller at `Record()` time | User confirmed via discuss — see `context.md` for the full call-site-by-call-site mapping and the rejected `target_type`+join alternative. | y |
| Display volume | Last 5 entries, no pagination | User confirmed via discuss — matches the existing mock's own item count; the Overview card is a summary, not a browser. | y |
| Endpoint shape | New `GET /api/audit-log?limit=5` (own handler), `OverviewHandler`/`OverviewResponse` unchanged | User confirmed via discuss — keeps Overview's existing payload untouched, reusable if a dedicated audit view is ever built. | y |
| Authorization | Any authenticated role, no gate | User confirmed via discuss — matches the card's current (mock) visibility; audit entries carry no secrets. | y |
| Actor name resolution | Live join to the actor's current name at read time (not a snapshot) | Unlike targets, actors are essentially never deleted mid-session, and a live name is more accurate (reflects a rename) than a frozen snapshot would be; if the actor's account was later hard-deleted, the read path falls back to a fixed "Usuário removido"/"Removed user" placeholder rather than crashing or leaking a raw UUID. | y (derived from discuss's target-labeling reasoning applied symmetrically, not separately re-confirmed) |
| Unknown/future action string | Generic "{{actor}} performed {{action}} on {{target}}" fallback phrase | Mirrors the codebase's existing "never crash on unrecognized enum" posture (e.g. billing's unrecognized-plan display). Prevents a future `Record` call site that forgets to update the i18n map from breaking the card. | n/a (derived engineering safeguard, not discussed) |
| `removed`/`domain_deleted`/`status_page_deleted` label-capture ordering | Label fetched/captured BEFORE the delete operation runs, not after | The target row (user, domain, status page) is gone once its delete call succeeds; capturing the label afterward would always be empty. | n/a (derived from existing handler code order — see `context.md`'s per-call-site table) |
| `TenantInviteRepository.Cancel` signature | Changes from `(ctx, tenantID, id) error` to `(ctx, tenantID, id) (*TenantInvite, error)`, using the same `RETURNING` clause `Refresh` already uses | `Cancel`'s current call site (`admins.go:518`) has no invite data in scope to use as the `canceled` action's label; `Refresh`'s existing `RETURNING`-based pattern is the established precedent in this same file for "mutate and return the row". | y (derived from `context.md`'s call-site table, not separately re-confirmed) |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Persist a human-readable label with every audit entry ⭐ MVP

**User Story**: As a developer of this codebase, I want every `audit.Log.Record` call to also capture a human-readable target label at the moment it's recorded, so a later read never has to guess what a bare `target_id` UUID refers to.

**Why P1**: Every other story depends on this data existing — without it, the read endpoint has nothing meaningful to render.

**Acceptance Criteria**:

1. The `admin_audit_log` table SHALL gain a nullable `target_label TEXT` column via a new migration; existing rows are left with `NULL` and are unaffected.
2. `audit.Log.Record`'s signature SHALL change to accept a `targetLabel` argument (`Record(ctx, actorID, targetID, targetLabel, action string) error`) and SHALL persist it in the new column.
3. Each of the 8 existing call sites (`invited`, `resent`, `canceled`, `role_changed`, `removed` in `admins.go`; `domain_verified`, `domain_deleted` in `domains_handler.go`; `status_page_deleted`, `status_page_domain_verified` in `status_pages_handler.go`) SHALL pass the human-readable identifier already available at that call site (email, domain string, status page name, or admin name/email — see `context.md`'s mapping table) as `targetLabel`.
4. WHERE a call site's delete operation runs before its `Record` call today (`removed`, `domain_deleted`, `status_page_deleted`), the target's label SHALL be captured/fetched BEFORE that delete call executes, so the label is never empty due to the row already being gone.
5. `TenantInviteRepository.Cancel` SHALL return the canceled `*TenantInvite` (in addition to `error`), using a `RETURNING` clause, so its `Email` is available to the `canceled` action's `Record` call.

**Independent Test**: Trigger each of the 8 actions (invite/resend/cancel invite, change role, remove admin, verify/delete domain, delete status page, verify status page domain) against a real Postgres instance, then `SELECT target_label FROM admin_audit_log ORDER BY created_at DESC LIMIT 8` and confirm every row has a non-null, human-readable label matching the entity acted on.

---

### P1: Read endpoint for recent activity

**User Story**: As the Overview page's frontend, I want a `GET /api/audit-log` endpoint returning the tenant's most recent audit entries with actor names resolved, so I can render them without knowing anything about the underlying table shape.

**Why P1**: The endpoint is the only way the frontend can stop using the mock array.

**Acceptance Criteria**:

6. WHEN an authenticated user of any role calls `GET /api/audit-log?limit=5` THEN the system SHALL respond `200` with the tenant's most recent `admin_audit_log` rows (ordered by `created_at DESC`, capped at the given `limit`, default `5` when omitted, hard-capped at `20` regardless of a larger requested value), each entry including: `actor_name` (resolved from the actor's current user record; `"Usuário removido"`/`"Removed user"` fallback — locale-appropriate — if the actor's user row no longer exists), `target_label` (verbatim from the column, possibly `null` for historical pre-feature rows), `action`, and `created_at`.
7. IF the request has no valid session THEN the system SHALL respond `401`, same as every other authenticated endpoint.
8. The endpoint SHALL be tenant-scoped: a request only ever returns rows for the caller's active tenant (enforced by the table's existing RLS policy — no new application-level filter needed beyond the existing `tenant_id` session-scoped connection).

**Independent Test**: As an authenticated viewer (no special role), call `GET /api/audit-log?limit=5` after seeding a few audit rows across two different tenants; confirm only the caller's own tenant's rows are returned, in `created_at DESC` order, capped at 5.

---

### P1: Wire the Overview card to real data

**User Story**: As a user looking at Overview, I want "Atividade recente do time" to show what actually happened in my organization, not example content.

**Why P1**: This is the feature's entire user-facing value.

**Acceptance Criteria**:

9. `OverviewPage.tsx`'s `RecentActivity` component SHALL fetch from `GET /api/audit-log?limit=5` (its own `useQuery`, independent of `useOverview`) instead of rendering the hardcoded `ACTIVITY_FEED` array, which SHALL be deleted.
10. Each of the 8 known actions SHALL render via the i18n phrase mapping in `context.md` (interpolating `actor_name` and `target_label`); an action string not in the map SHALL render the generic fallback phrase instead of crashing or omitting the row.
11. WHERE `target_label` is `null` (historical pre-feature row) THEN the rendered phrase SHALL omit the target clause gracefully (e.g. "{{actor}} changed a role" rather than "{{actor}} changed {{undefined}}'s role") — never render the literal string `"null"`/`"undefined"`.
12. WHILE the query is loading, the card SHALL show the shared `Skeleton` primitive (loading-skeletons feature, already built) instead of a blank card; WHILE it errors, the card SHALL show the same error-row pattern already used elsewhere on this page.
13. WHEN there are zero audit entries for the tenant (a brand-new tenant) THEN the card SHALL render an empty-state message instead of an empty white box.

**Independent Test**: Seed a tenant with a handful of admin actions across different types (including one predating this feature, i.e. `target_label = NULL`), load Overview as any role, confirm the card shows up to 5 real entries with correct actor/target phrasing and the null-label entry renders gracefully; seed a brand-new tenant with zero entries and confirm the empty state renders instead of a blank card.

---

## Edge Cases

- IF the actor's user row was hard-deleted after the audit entry was written (the `removed` action's own existence proves this is possible) THEN the endpoint SHALL substitute the fixed "Usuário removido"/"Removed user" placeholder for `actor_name`, never a raw UUID or a crash.
- IF `limit` is requested above 20 THEN the system SHALL silently cap it at 20 rather than erroring (consistent with this codebase's existing pagination conventions elsewhere, e.g. `parsePage`).
- IF `limit` is 0, negative, or non-numeric THEN the system SHALL fall back to the default of 5, not error.
- WHEN a future action string is added to `audit.Log.Record` without a corresponding i18n entry THEN the frontend SHALL render the generic fallback phrase for it, never break the whole card's render.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| ACTIVITY-01 | P1: Persist a human-readable label with every audit entry | Specified | Implementing |
| ACTIVITY-02 | P1: Persist a human-readable label with every audit entry | Specified | Implementing |
| ACTIVITY-03 | P1: Persist a human-readable label with every audit entry | Specified | Pending |
| ACTIVITY-04 | P1: Persist a human-readable label with every audit entry | Specified | Pending |
| ACTIVITY-05 | P1: Persist a human-readable label with every audit entry | Specified | Pending |
| ACTIVITY-06 | P1: Read endpoint for recent activity | Specified | Pending |
| ACTIVITY-07 | P1: Read endpoint for recent activity | Specified | Pending |
| ACTIVITY-08 | P1: Read endpoint for recent activity | Specified | Pending |
| ACTIVITY-09 | P1: Wire the Overview card to real data | Specified | Pending |
| ACTIVITY-10 | P1: Wire the Overview card to real data | Specified | Pending |
| ACTIVITY-11 | P1: Wire the Overview card to real data | Specified | Pending |
| ACTIVITY-12 | P1: Wire the Overview card to real data | Specified | Pending |
| ACTIVITY-13 | P1: Wire the Overview card to real data | Specified | Pending |

**Coverage:** 13 total, 13 mapped to tasks (migration + audit package + 8 call sites + repository + handler + frontend) ✅

---

## Success Criteria

- [ ] "Atividade recente do time" shows real, tenant-scoped, correctly-labeled recent actions — `ACTIVITY_FEED` mock array is deleted, not just unused.
- [ ] Every one of the 8 `Record` call sites captures its label before any delete it depends on runs.
- [ ] Deleting a domain, status page, or admin still leaves a readable history entry afterward (label survives target deletion).
- [ ] `go build ./... && go test ./... && go vet ./... && gofmt -l` clean; `npx tsc -b --noEmit` + full vitest suite clean.
