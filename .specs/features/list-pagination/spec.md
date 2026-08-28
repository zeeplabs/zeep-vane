# List Pagination Specification

## Problem Statement

Every list endpoint in the Vane admin API today runs `SELECT ... ORDER BY ... LIMIT` with no `LIMIT`/`OFFSET` at all — the whole table comes back in one response, and every frontend list renders the full array with a plain `.map()`. Most of these stay small by construction (AD-002: 1 installation = 1 company, so admins/domains/services/status-pages/email-providers/poller-status are config-sized, tens of rows at most). `incidents` (admin list) is the one place that genuinely grows without bound — no retention, unlike the public endpoint's `retentionDays` cutoff — and its `updates` timeline can grow per long-lived incident. The public status page's incident list is retention-bounded but still uncapped by `LIMIT`.

## Goals

- [ ] No admin list endpoint returns an unbounded result set; every one of the 9 in-scope endpoints returns a fixed-size page
- [ ] `incidents` (the only endpoint with real unbounded growth today) gets a working, testable pager end-to-end (backend envelope + frontend `Pager` control)
- [ ] The pagination envelope and UI pattern established for `incidents` is mechanically replicated across the other 8 endpoints with no bespoke variation
- [ ] Public status page incidents load progressively ("Carregar mais") without ever fetching the full history in one request

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Pagination for `admin_audit_log` | No read endpoint exists today (`internal/audit/log.go` only exposes `Record`, an insert). Adding one is a new feature (new handler, new route, new UI screen), not "add pagination to an existing list" — explicitly declined by the user during brainstorming |
| Pagination for `status_intervals` (`StatusIntervalRepository.ListOverlapping`) | Already bounded by a time-window filter (`starts_at`/`ends_at` vs. the caller's window), which is the correct contract for `internal/history`'s uptime-bar computation. Swapping it for `page`/`page_size` would break that contract's alignment with the time axis the uptime chart needs — explicitly declined by the user |
| Cursor/keyset pagination | Offset-based (`LIMIT`/`OFFSET` via `page`) chosen instead — simpler to implement uniformly across 9 endpoints, sufficient at the confirmed scale (config-sized tables plus one genuinely-growing one), user-confirmed |
| Configurable `page_size` via query param | Fixed page size per endpoint instead (no `?page_size=`) — removes a validation/abuse surface (`?page_size=999999`) for no confirmed product need, user-confirmed |
| Infinite scroll / virtualization in admin screens | Classic pager (Anterior/Próximo + "Página X de Y") chosen for all 8 admin screens instead — standard admin-panel convention, user-confirmed. Public status page is the one exception (see below) |
| Changing existing `ORDER BY` clauses, adding new sort/filter options | Pagination adds `LIMIT`/`OFFSET` (and, for `admins`, in-memory slicing) on top of each endpoint's existing ordering; it does not change what "first page" means for any endpoint |
| Pagination UI for `incident updates` timeline in `IncidentDetail.tsx` beyond the backend envelope | Backend endpoint (`GET /api/incidents/{id}/updates`) gets the same paginated envelope as every other endpoint in scope (AC in P1 below); the P1 Independent Test covers only the backend contract change for this one sub-resource — a dedicated `Pager` UI for the timeline view is deferred, since the update count per incident is small enough today that showing "load more" is a nice-to-have, not the growth risk this feature targets |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Pagination style | Offset-based (`page`/`page_size`, `LIMIT`/`OFFSET`), not cursor/keyset | Simpler across 9 heterogeneous endpoints; sufficient at this scale (8 of 9 are config-sized, `incidents` grows via `ORDER BY created_at DESC` which offset pagination handles fine for an admin panel, not a real-time feed) | y |
| Response envelope shape | `{items, total, page, page_size}` | `total` lets the UI show "Página X de Y" and disable "Próximo" on the last page without a second round-trip guess | y |
| How `total` is computed | `COUNT(*) OVER()` window function in the same query as the page's rows — one query, not `SELECT` + separate `SELECT COUNT(*)` | Avoids a second round-trip and the race between two separate queries under concurrent writes | y |
| `page_size` per endpoint | Fixed, not client-configurable: `20` for `admins`, `domains`, `services`, `status-pages`, `email-providers`, `poller-status`; `25` for `incidents` and `incident updates`; `10` for public status page incidents | Fixed size removes a `?page_size=` abuse/validation surface; `incidents` gets a larger page since it is the endpoint expected to actually grow; public page gets a smaller page tuned for "Carregar mais" click cadence | y |
| Invalid/missing `?page=` handling | Missing `page` defaults to `1`. Non-positive-integer `page` (`0`, negative, non-numeric, e.g. `?page=abc`) is clamped to `1` — never a `400` | Matches the "fixed page size, no strict validation surface" philosophy already chosen for `page_size`; a bad `page` value degrades to "first page" instead of erroring | y |
| `page` beyond the last page | Returns `items: []` with the same `total`/`page`/`page_size` fields, HTTP 200 — never an error | Standard pagination behavior; the frontend `Pager` already prevents this by disabling "Próximo" past the last page, but a direct/stale request must still resolve cleanly | y |
| `total_pages` when `total == 0` | Frontend computes `total_pages = max(1, ceil(total / page_size))` — never renders "Página 1 de 0" | Not explicitly discussed with the user during brainstorming; chosen because "de 0" reads as a bug to an admin looking at an empty list, and `max(1, …)` is the smallest fix that avoids it | n |
| `admins` endpoint pagination mechanics | `AdminsHandler.List` merges active admins (`AdminRepository`) and pending invites (`AdminInviteRepository.List`) in memory (as it already does today), THEN applies `page`/`page_size=20` slicing to the merged, sorted result — no SQL `UNION`. `total` = combined count of admins + pending invites | The two sources are heterogeneous queries against different tables with different columns; a SQL-level `UNION ALL` with paginated ordering across both is real complexity for a dataset that is small by construction (AD-002: one installation per company, so total admins + invites stays in the tens) | y |
| Cache invalidation after mutations on a paginated list | Frontend mutations (create/update/delete on any in-scope resource) invalidate the query-key prefix for that resource (e.g. `["incidents"]`), not just the currently-viewed page's exact key (`["incidents", page]`) — so every cached page for that resource is dropped and refetched on next view | Not explicitly discussed with the user during brainstorming. Without this, creating an incident while viewing page 2 would leave page 1's cached `total`/`items` stale, and the admin would not see the new row even after navigating back to page 1 | n |
| Public status page pagination UI | "Carregar mais" (load next page, append to already-rendered items) instead of the classic numbered `Pager` used in admin screens | Public status page is an unauthenticated storefront-style page (matches conventions like Statuspage.io), not an admin data table — a numbered pager reads as an internal-tool affordance in that context. Presented as part of the architectural design and approved by the user alongside the rest of the design | y |
| `admin_audit_log`, `status_intervals` pagination | Out of scope entirely (see Out of Scope table) | Explicit user decision after the two exceptions were surfaced during brainstorming | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Admin navigates a growing incident list without loading everything ⭐ MVP

**User Story**: As an admin, I want the incidents list and an incident's update timeline to load in fixed-size pages, so that the admin panel stays fast and useful as the number of incidents grows over the life of the installation.

**Why P1**: `incidents` is the only in-scope endpoint with genuinely unbounded growth today (no retention, unlike the public endpoint) — it is the actual problem this feature exists to solve. It also establishes the backend envelope and frontend `Pager` pattern that every other story in this feature mechanically reuses.

**Acceptance Criteria**:

1. WHEN an admin requests `GET /api/incidents?page=N` THEN the system SHALL respond with `{items, total, page, page_size}` where `items` contains at most 25 incidents starting at offset `(N-1)*25`, ordered by `created_at DESC`
2. WHEN the `page` query parameter is absent THEN the system SHALL default `page` to `1`
3. IF the `page` query parameter is not a positive integer (zero, negative, or non-numeric) THEN the system SHALL clamp it to `1` and respond `200`, never `400`
4. WHEN the requested `page` is beyond the last page THEN the system SHALL respond `200` with `items: []` and the same `total`/`page`/`page_size` fields
5. WHEN an admin requests `GET /api/incidents/{id}/updates?page=N` THEN the system SHALL respond with the same envelope shape, `page_size` 25, `items` ordered by `created_at DESC`, scoped to that incident
6. The system SHALL compute `total` via a single query using `COUNT(*) OVER()` rather than a separate `COUNT(*)` query
7. WHILE the incidents list is displayed in the admin frontend THEN the system SHALL render a `Pager` control below the list showing "Página X de Y", disabling "Anterior" on page 1 and "Próximo" on the last page

**Independent Test**: Seed more than 25 incidents (e.g. via direct DB insert in a test), call `GET /api/incidents?page=1` and confirm exactly 25 items plus a correct `total`; call `page=2` and confirm the remaining items; call a page number past the last page and confirm an empty `items` array with `200`. Separately, seed more than 25 updates on one incident and confirm `GET /api/incidents/{id}/updates?page=2` returns the correct slice.

---

### P2: The same pagination pattern applies to every other admin list screen

**User Story**: As an admin, I want domains, services, status pages, admins, poller status, and email providers to behave the same way incidents does, so that the whole admin panel is consistent and none of these silently regresses into an unbounded query later.

**Why P2**: Mechanical replication of the P1 pattern across 6 more endpoints — no new architectural decision, but each endpoint needs its own repository/handler/hook/test changes, and `admins` has the one real variation (in-memory merge, see Assumptions).

**Acceptance Criteria**:

1. WHEN an admin requests `GET /api/domains?page=N`, `GET /api/services?page=N`, `GET /api/status-pages?page=N`, `GET /api/poller/status?page=N`, or `GET /api/integrations/email?page=N` THEN the system SHALL respond with `{items, total, page, page_size}`, `page_size` 20, using the same defaulting/clamping/out-of-range rules as P1 ACs 2-4
2. WHEN an admin requests `GET /api/admins?page=N` THEN the system SHALL merge active admins and pending invites in memory (existing merge logic), apply `page`/`page_size=20` slicing to the merged, sorted result, and report `total` as the combined count of admins plus pending invites
3. WHILE any of these 6 list screens is displayed THEN the system SHALL render the same reusable `Pager` component used by `incidents`, with identical enable/disable behavior at the boundaries
4. WHEN an admin creates, updates, or deletes a record in any of these 6 resources (or in `incidents`) THEN the system SHALL invalidate every cached page for that resource, not only the page currently in view

**Independent Test**: For each of the 6 endpoints, seed more rows than one page's `page_size`, request `page=1` and `page=2`, and confirm correct slicing and `total`. For `admins`, seed a mix of active admins and pending invites exceeding 20 combined and confirm the merged/paginated result and combined `total`. For AC4, create a new incident while the frontend has `incidents` page 2 cached, navigate back to page 1, and confirm the new incident is visible without a manual refresh.

---

### P3: Public status page visitors load incident history progressively

**User Story**: As a status page visitor, I want to click "Carregar mais" to see older incidents instead of the page loading the entire incident history at once, so that the public page stays fast even for installations with a long incident history.

**Why P3**: The public endpoint is already retention-bounded (`retentionDays`), so it is lower urgency than P1's unbounded growth — but it is still uncapped by `LIMIT` today and was explicitly included in the confirmed scope.

**Acceptance Criteria**:

1. The system SHALL apply the `{items, total, page, page_size}` envelope (`page_size` 10) to the incidents returned by `PublicStatusHandler` (both `ListPublic` and `ListPublicForStatusPage`)
2. WHILE there are more incidents than currently loaded on the public status page THEN the system SHALL render a "Carregar mais" control instead of a numbered `Pager`
3. WHEN a visitor clicks "Carregar mais" THEN the system SHALL fetch the next page and append its items to the already-rendered list, without replacing or reordering previously-loaded items
4. WHEN the last page has been loaded THEN the system SHALL NOT render the "Carregar mais" control

**Independent Test**: Seed more than 10 incidents (mixing active/resolved within retention) on a status page, load the public page, confirm 10 items and a visible "Carregar mais" control, click it, and confirm the next page's items are appended (15 total visible after 2 pages of a 15-incident seed) and the control disappears once nothing remains.

---

## Edge Cases

- IF `page` is a valid positive integer but the table is empty (`total == 0`) THEN the system SHALL respond `{items: [], total: 0, page: 1, page_size: N}` with `200`, and the frontend SHALL display "Página 1 de 1" (never "de 0")
- IF two admins request the same page concurrently while a row is being inserted between the count and the page fetch THEN the system's use of `COUNT(*) OVER()` in a single query SHALL avoid a torn read between `total` and `items` for that single request (each request is internally consistent; concurrent requests may legitimately see different snapshots, which is expected and not a bug)
- WHEN an admin navigates directly to a deep page via a bookmarked/shared URL that no longer exists (e.g. items were deleted since the link was shared) THEN the system SHALL follow AC4/P1 (empty `items`, no error) rather than redirecting or erroring

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --------------- | ------ | ------ | ------- |
| PAG-01 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-02 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-03 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-04 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-05 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-06 | P1: Admin navigates a growing incident list without loading everything | Specify | Implementing |
| PAG-07 | P1: Admin navigates a growing incident list without loading everything | Specify | Pending |
| PAG-08 | P2: The same pagination pattern applies to every other admin list screen | Specify | Implementing |
| PAG-09 | P2: The same pagination pattern applies to every other admin list screen | Specify | Implementing |
| PAG-10 | P2: The same pagination pattern applies to every other admin list screen | Specify | Pending |
| PAG-11 | P2: The same pagination pattern applies to every other admin list screen | Specify | Implementing |
| PAG-12 | P3: Public status page visitors load incident history progressively | Specify | Pending |
| PAG-13 | P3: Public status page visitors load incident history progressively | Specify | Pending |
| PAG-14 | P3: Public status page visitors load incident history progressively | Specify | Pending |
| PAG-15 | P3: Public status page visitors load incident history progressively | Specify | Pending |

**ID format:** `PAG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 15 total, 0 mapped to tasks, 15 unmapped ⚠️ (expected at Specify — mapping happens in Tasks)

---

## Success Criteria

How we know the feature is successful:

- [ ] All 9 in-scope endpoints return `{items, total, page, page_size}` and never a bare unbounded array
- [ ] `incidents` and `incident updates` (the endpoints with real unbounded growth) are verifiably paginated end-to-end, backend and frontend
- [ ] Every admin list screen (8 of them) shows the same `Pager` component with identical behavior
- [ ] The public status page never fetches its full incident history in one request
- [ ] Zero regression in existing list-endpoint tests beyond the expected envelope-shape update (array → `{items,...}`)
