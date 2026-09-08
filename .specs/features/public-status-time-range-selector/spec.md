# Public Status Time-Range Selector Specification

## Problem Statement

The public status page's per-service history chart is fixed at 24 hours with 1-hour bars (`public-status-hourly-history`, AD-019's predecessor feature). Every comparable status page (Atlassian Statuspage, Better Uptime, UptimeRobot) defaults to a much longer view — typically 90 days at daily granularity — so a visitor can see incident trends over months, not just the last day. Vane's 24h/1h view is more granular than the market default but hides everything older than a day: a visitor cannot tell "was this down last week" from the public page at all. Julio wants to keep the 24h/1h view (it's genuinely useful for watching something recent) and add a selector so a visitor can pull back to 7, 30, or 90 days when they want the longer trend.

## Goals

- [ ] A visitor can switch the per-service history chart between 24h, 7d, 30d, and 90d views from the public status page, without a page reload.
- [ ] Each range renders with a bar count and bucket width appropriate to that range (24h stays 1h/24 bars; longer ranges use coarser buckets so the chart never renders more bars than fit legibly).
- [ ] `status_intervals` retention is extended so 90 days of history actually exists to show (today's 35-day retention would make the 90d tier silently mostly empty).

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Custom/arbitrary date range picker (pick any start/end date) | Julio asked for a small fixed set of tiers matching market convention, not a full date picker. Bigger UI/backend surface, not requested. |
| Per-service independent range selection (each service's chart on its own range) | Every comparable status page uses one page-wide selector. Per-service selectors would be unusual UX and quadruple the number of chart states to reason about, with no stated user need. |
| Changing the resolved-incidents list's own 90-day window to track the selector | `incidentRetentionDays = 90` (`internal/api/public_status_handler.go`) already governs a separate table (`incidents`) with its own fixed window, unrelated to `status_intervals`/the bar chart. Coupling them is a different, unrequested change. |
| Ranges or bucket widths beyond the four confirmed tiers (24h/7d/30d/90d) | Not requested; the four tiers were chosen to match market convention and Julio's own framing ("2 ou 3 opções até 90 dias"). |
| Historical data older than the new retention window | `status_intervals` rows are pruned past retention regardless of this feature; a self-hosted instance younger than 90 days, or one that hasn't yet reached the new retention window since upgrading, legitimately has less history to show — handled as `no_data` buckets (existing behavior), not a new requirement. |
| Persisting the visitor's selected range (URL param, localStorage, cookie) across visits/reloads | Not requested. Each page load starts at the 24h default. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Range tiers offered | 24h, 7d, 30d, 90d | User-confirmed via `AskUserQuestion`: matches market convention (Atlassian Statuspage, Better Uptime) and the "2 ou 3 opções até 90 dias" framing. | y |
| Bucket width per tier | 24h → 1h (24 bars, unchanged from today); 7d → 6h (28 bars); 30d → 1d (30 bars); 90d → 1d (90 bars) | User-confirmed: keeps 24h's existing per-hour detail; 7d's 6h buckets show intra-day pattern without 168 bars; 30d/90d land in the ~30-90 bar range every comparable status page uses for its default (daily) view. | y |
| `status_intervals` retention | 95 days (bumped from today's 35 days, `internal/cli/serve.go`'s `pruneRetention` constant) | User-confirmed: 90-day tier's longest possible request is exactly `now - 90d`; a flat 90-day retention risks trimming the oldest visible day if the pruner's hourly tick runs slightly ahead of a request, or under minor clock skew between the pruner and the handler. 5 days of margin absorbs that without meaningfully changing storage growth (closed intervals only; open intervals are never pruned regardless of age). | y |
| Selector scope: one page-wide selector vs. per-service | One page-wide selector, applies to every service's chart at once | Not asked - matches every reference status page (Statuspage, Better Uptime, UptimeRobot all use one page-level range control), and per-service selectors were never requested. Logged here rather than re-litigated as a question since the "Out of Scope" table above already excludes the per-service alternative for the same reason. | n (default, not asked - see Out of Scope) |
| Uptime percentage figure tracks the selected range | Yes - `UptimePercent` is recomputed for whichever range/window is currently selected, not pinned to 24h | Not asked - a visitor selecting "90 dias" and seeing an uptime % still labeled/computed for the last 24h would be misleading and inconsistent with the bars they're looking at. Every reference status page shows the uptime % for the same window as its bars. | n (default, reasoned from existing UX pattern - flag if wrong) |
| Preview parity (authenticated `public-preview` endpoint) | The range selector works identically pre-publish, same as `AD-008`'s existing preview-parity guarantee for the 24h view today | Not asked - `composeResponse` already shares the same code path for both the public listener and the authenticated preview (`public-status-hourly-history`'s AD); extending that path to accept a range parameter carries the parity forward for free unless a reason emerges to diverge. | n (default, follows an existing precedent) |
| Bucket boundary alignment for widths >1h (6h, 1d) | Buckets align to fixed local-clock boundaries for their width (e.g. 6h buckets start at 00:00/06:00/12:00/18:00 local time; 1d buckets start at local midnight), the same way today's 1h buckets already align to clock-hour boundaries (`BuildHourly`'s `currentStart` truncation) - not an arbitrary "N units back from the exact request timestamp" sliding window | Not asked - this is a direct generalization of the existing 1h behavior (`internal/history/hourly.go`), not a new decision; keeping it consistent means bucket boundaries for a given (timezone, width) pair are stable across repeated requests within the same clock unit, which is what makes bar-by-bar tooltips meaningful. Precise implementation left to Design. | n (mechanical extension of existing behavior, not a product decision) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Visitor switches between 24h/7d/30d/90d views ⭐ MVP

**User Story**: As a status page visitor, I want to switch a service's history chart between 24h, 7d, 30d, and 90d views, so that I can see either recent detail or a longer incident trend, matching what I'm used to on other status pages.

**Why P1**: This is the entire feature - without it there is no selector, just the existing fixed 24h view.

**Acceptance Criteria** (each line is one EARS pattern):

1. The public status page SHALL default to the 24h/1h view on initial load, unchanged from today's behavior.
2. WHEN a visitor selects the 7d, 30d, or 90d option THEN the system SHALL fetch and render that service's history using that tier's bucket width (7d → 6h buckets / 28 bars, 30d → 1d buckets / 30 bars, 90d → 1d buckets / 90 bars) without a full page reload.
3. WHEN a visitor selects a different range THEN the system SHALL apply the new range to every service's chart on the page (page-wide selector, not per-service).
4. WHILE a range other than 24h is selected, the system SHALL recompute and display the uptime percentage for that same range's window, not the 24h window.
5. The system SHALL apply the selected range identically on the authenticated `public-preview` endpoint used by the admin dashboard, matching the public listener's behavior for the same range (parity with `AD-008`).
6. WHERE a service has no `status_intervals` data covering part of the selected range (self-hosted instance younger than the range, or a service linked after that point in time) THE system SHALL render those buckets as `no_data`, consistent with the existing 24h view's handling of missing data.
7. IF a request specifies a range value outside the four supported tiers THEN the system SHALL reject it (400) rather than silently falling back to a default or an unbounded query.

**Independent Test**: On the public status page, click each of the 4 range options in turn and confirm the chart's bar count and width change to match the table above, the "X atrás / agora" labels update, and the uptime % figure changes correspondingly for a service with real incident history spanning more than 24h.

---

### P2: 90 days of real history actually exists to show

**User Story**: As a status page visitor selecting the 90-day view, I want to see up to 90 real days of history (not history that was silently deleted before I could ever see it), so that the 90-day tier isn't misleadingly empty on an instance that's been running for a while.

**Why P2**: Without this, P1's UI works but the 90d tier shows mostly `no_data` on any instance older than 35 days - not a UI bug, a data-retention gap that makes the feature look broken. Separated from P1 because it's a one-constant backend change with its own operational implication (more storage) worth calling out on its own.

**Acceptance Criteria**:

1. The system SHALL retain closed `status_intervals` rows for at least 95 days before the retention pruner deletes them (`internal/cli/serve.go`'s `pruneRetention`, up from today's 35 days).
2. The system SHALL continue to never prune an open interval (`ends_at IS NULL`), regardless of its age - unchanged from today's pruner behavior.

**Independent Test**: Insert a closed `status_intervals` row with `ends_at` 90 days in the past, run the pruner's `prune()` once, and confirm the row still exists (still within the new 95-day retention).

---

## Edge Cases

- IF the selected range's window start predates the oldest data the retention window guarantees (e.g. a fresh self-hosted instance with only 3 days of history, viewing the 90d tier) THEN the system SHALL render the missing leading buckets as `no_data`, not an error - same principle as AC P1-6.
- WHEN a visitor rapidly switches between range options THEN the system SHALL show only the result of the most recently selected range once its fetch completes, not an out-of-order stale response overwriting a newer one (a real race given each range change is a fresh request).
- WHEN the poller has stalled (no confirmed status past `LastSeenAt`) and a visitor selects a longer range THEN the system SHALL still clamp bars after `asOf` to `no_data`, extending today's `asOf` clamp (H7) to every range tier, not just 24h.

---

## Requirement Traceability

Each requirement gets a unique ID for tracking across design, tasks, and validation.

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| TRS-01 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-02 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-03 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-04 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-05 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-06 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-07 | P1: Visitor switches between 24h/7d/30d/90d views | Design | Pending |
| TRS-08 | P2: 90 days of real history actually exists to show | Design | Pending |
| TRS-09 | P2: 90 days of real history actually exists to show | Design | Pending |

**ID format:** `TRS-NN` (Time-Range Selector)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 9 total, 0 mapped to tasks, 9 unmapped ⚠️ (expected pre-Design)

---

## Success Criteria

How we know the feature is successful:

- [ ] A visitor can select each of the 4 range tiers and see a correctly-sized, correctly-bucketed chart for a service with real multi-week history.
- [ ] The uptime % figure visibly changes when the range changes (not pinned to 24h).
- [ ] `go test ./internal/retention/...` confirms a 90-day-old closed interval survives a prune cycle under the new retention constant.
- [ ] The authenticated `public-preview` endpoint renders identically to the public listener for the same range, for the same service data.
