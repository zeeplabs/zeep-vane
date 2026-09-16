# Service Delete (Soft Delete) Specification

## Problem Statement

There is no way to remove a monitored service today — `ServiceRepository`/`ServicesHandler` expose no delete path at all. A service created by mistake, or one that's no longer relevant, stays in the list forever, and the poller keeps checking it forever. A hard delete (the pattern `DomainRepository.Delete` uses: `DELETE` + catch the FK-violation as a blocking error) is impractical here specifically: `status_intervals`/`incidents` reference `services(id)` with no `ON DELETE CASCADE`, and any service that has ever been polled even once already has `status_intervals` rows — a hard delete would be permanently blocked the moment monitoring produces its first data point. Soft delete (a `deleted_at` column, service hidden from every read/poll path but its historical rows preserved) is the only workable shape.

## Goals

- [ ] Owner can remove a monitored service from the active list; it stops appearing everywhere and stops being polled.
- [ ] Historical data (`status_intervals`, `incidents`) tied to the deleted service is never destroyed.
- [ ] No poller in-memory state (goroutines, hysteresis counters) outlives the delete — closes the same class of bug found earlier this session in the poller leader-election path (state that isn't torn down when its owning row disappears).

## Out of Scope

| Feature | Reason |
| --- | --- |
| Restore / undelete a soft-deleted service | No stated need yet; can be added later as a straightforward reversal (`deleted_at = NULL`) without touching this feature's shape. |
| Hard/permanent delete (purge) | Not requested; soft delete is the only mechanism this feature ships. |
| Auto-detaching a deleted service from a published status page | User chose to block delete instead (see Assumptions) — a deleted service is never silently removed from a status page's composition. |
| Deleting a service via bulk/batch action | No stated need; single-service delete only. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Delete referenced by a published status page | Block with `409`, same shape as `DomainRepository.Delete`'s `ErrDomainInUse` | User confirmed via discuss: don't silently change a published page's composition. | y |
| Authorization | `ownerOnly` | User confirmed via discuss: same tier as `DELETE /api/admins/{id}` / `DELETE /api/tenants/current`. | y |
| Polling-manual goroutine teardown | Implement now: `ManualScheduler.reconcile` tracks a `map[string]context.CancelFunc` alongside its existing `known` map; a service ID present in `known` but absent from the current `ListPollingManual` result gets its per-service context canceled and is removed from both maps. | User confirmed via discuss: leaving this as a known limitation repeats the exact "orphaned in-memory state" bug class fixed earlier today in `poller_manager.go`. | y |
| SLO poller (`poller.go`) teardown | No new mechanism needed | `poller.go`'s main loop calls `p.services.List(ctx)` fresh every cycle (no persistent goroutine per service) — filtering `deleted_at IS NULL` there means a deleted service simply stops appearing in the very next cycle's slice. `breachStreak[svc.ID]` becomes a harmless orphaned map entry (bounded: one `int` per ever-deleted service ID, never read again since the ID never reappears in `List()`'s output) — cleaning it is optional and NOT required for correctness, only for memory hygiene. | n/a (derived from reading `poller.go`, not a product decision) |
| Historical data | Never touched — `status_intervals`/`incidents` rows referencing the deleted service's ID stay exactly as they are | User's own instruction was "soft delete"; destroying history defeats that intent, and public-status/incident-history read paths already tolerate a service ID with no corresponding live `services` row query today only if we're careful — see SVCDEL-08 for the one path that needs an explicit `deleted_at` filter to avoid resurrecting a deleted service on the public status page. | y |
| `GET /api/services/{id}` on a deleted ID | `404`, identical body to "never existed" (`serviceNotFoundBody`) | Consistent with the codebase's existing "never leak existence" posture (AGENTS.md §4) applied to `Delete`/`Get`/`Update` uniformly. | n/a (codebase convention) |
| `status_page_services` rows for a service later deleted from a status page it was NOT attached to at delete time | N/A — delete is blocked entirely while attached (see first row above), so this state cannot occur. | Direct consequence of the blocking decision. | n/a |
| Data lifecycle / expiry dimension | No TTL or auto-purge added; soft-deleted rows persist indefinitely until a future purge feature (out of scope) | No stated retention requirement from the user. | n/a (assumption, not discussed — logged per closure gate) |
| Idempotency of delete | Deleting an already-deleted service returns `404` (same as "never existed") on the second call, not `200`/`409` | A soft-deleted service is invisible to `Get` too (see above), so the delete handler's existence check naturally 404s on retry — consistent, not a special case. | n/a (derived from the 404-on-deleted decision above) |
| UI entry point | Row-level action (kebab menu) with a confirm dialog before the destructive call, same pattern as `AdminsPage.tsx`'s `confirmRemove` + `Dialog.tsx` | Existing precedent in this codebase for a destructive action with confirmation; `service-edit` introduces the same row-action-menu affordance, this feature reuses it for the delete entry. | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Soft-delete a monitored service ⭐ MVP

**User Story**: As an owner, I want to remove a monitored service I no longer need so that it stops cluttering the list and stops being polled, without losing its history.

**Why P1**: The entire feature — there is no smaller independently-demoable slice.

**Acceptance Criteria**:

1. WHEN an owner sends `DELETE /api/services/{id}` for a service not attached to any status page THEN the system SHALL set that service's `deleted_at` to the current time and respond `204`.
2. WHEN a service has `deleted_at` set THEN the system SHALL exclude it from `GET /api/services` (list), `GET /api/services/{id}` (404s as if never existed), `internal/poller.Poller`'s per-cycle `List(ctx)` result, and `ManualScheduler`'s `ListPollingManual(ctx)` result.
3. IF the target service is currently referenced by any row in `status_page_services` THEN the system SHALL respond `409` with a fixed generic error body and SHALL NOT set `deleted_at`.
4. IF `{id}` does not match any existing (non-deleted) service THEN the system SHALL respond `404` with the same fixed generic body `GET`/`PATCH` use.
5. IF the requester's role is not `owner` THEN the system SHALL respond `403` and SHALL NOT modify the row.
6. WHEN a polling-manual service (`monitor_mode = "polling"`) is soft-deleted AND `ManualScheduler` currently has a running goroutine for it THEN the system SHALL cancel that goroutine's context within one `discoveryInterval` reconciliation tick and SHALL NOT write any further `status_intervals` rows for that service ID afterward.
7. The system SHALL preserve every existing `status_intervals` and `incidents` row referencing the deleted service's ID unchanged (no cascade, no cleanup).
8. WHILE a service is soft-deleted, the public status page (`PublicStatusHandler`) SHALL NOT display it, for any status page it was attached to before being blocked from deletion — N/A in practice since AC3 blocks delete while attached, but the read path itself SHALL still filter `deleted_at IS NULL` defensively (defense in depth, not reachable via the normal flow given AC3).
9. WHEN the delete succeeds THEN the frontend `ServiceListPage` SHALL remove the row from the visible list without a full page reload (react-query cache invalidation/refetch of the current page).

**Independent Test**: Create a service (not attached to any status page), delete it, confirm `GET /api/services` no longer lists it, `GET /api/services/{id}` 404s, and (for a polling-mode service) confirm no new `status_intervals` row is written for it after the next discovery tick. Separately: attach a service to a status page, attempt delete, confirm `409` and the service still appears in `GET /api/services`.

---

## Edge Cases

- IF a delete request targets a service ID that belongs to a different tenant (SaaS mode multi-tenancy) THEN the system SHALL 404 (same tenant-scoping the rest of the services routes already enforce) rather than leak cross-tenant existence.
- IF `ManualScheduler.reconcile` runs mid-delete (race between the DB write and the next tick) THEN the goroutine teardown SHALL be eventually consistent within one tick — no requirement for the DELETE call itself to synchronously wait for teardown.
- WHEN a service is soft-deleted and its status page attachment is removed later (separate action, in a future feature or manually), the service remains soft-deleted — deletion is not implicitly reversed by unlinking.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SVCDEL-01 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-02 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-03 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-04 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-05 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-06 | P1: Soft-delete a monitored service | Implementing | Implementing (fix applied: `failedTenantIDs` guard test added, `TestManualScheduler_Reconcile_TenantListFailsOnLaterRound_DoesNotTearDownTrackedServices` - pending re-verify) |
| SVCDEL-07 | P1: Soft-delete a monitored service | Implementing | Implementing (fix applied: `TestServiceRepository_SoftDelete_PreservesStatusIntervalsAndIncidents` added - pending re-verify) |
| SVCDEL-08 | P1: Soft-delete a monitored service | Implementing | Verified |
| SVCDEL-09 | P1: Soft-delete a monitored service | Implementing | Verified |

**Coverage:** 9 total, 9 mapped to tasks (repository + handler + poller + frontend layers) ✅

---

## Success Criteria

- [ ] Owner can delete an unattached service from the UI in under 10 seconds, with a confirm step, no page reload.
- [ ] Deleting a service attached to a status page is blocked with a clear message, never silent.
- [ ] No goroutine, poller cycle, or public status page ever touches a soft-deleted service again after one reconciliation tick.
- [ ] `status_intervals`/`incidents` history for deleted services is provably untouched (row counts before/after delete are identical).
