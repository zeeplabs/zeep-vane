# Service Groups and Dependencies Specification

## Problem Statement

A status page (`internal/api/status_pages_handler.go`'s `SetServices`, `internal/db/status_page_repository.go`) today shows every attached service as a flat, unordered list — `status_page_services` is a plain join table (`status_page_id`, `service_id`), no position, no grouping, no relationship between services. For an operator with many services on one page (e.g. per-product or per-environment breakdown), everything renders as one undifferentiated list, with no way to express "these N services belong together" or "this service depends on that one."

This spec adds two independent, optional pieces of structure to a service's placement on a given status page: an optional **group** (a named, purely visual container — no status of its own) and an optional **parent service** (a dependency link — purely informational, the child keeps its own independent status). Both are scoped to the status-page↔service placement, not to the service globally: the same service can be grouped differently (or not at all) on different status pages.

## Goals

- [ ] An operator SHALL be able to create, rename, reorder, and delete named groups on a given status page.
- [ ] An operator SHALL be able to assign a service (as placed on a given status page) to at most one group on that page, or to no group.
- [ ] An operator SHALL be able to mark a service (as placed on a given status page) as depending on at most one other service on that same page ("has a parent"), or on none.
- [ ] A service that already has a parent SHALL NOT itself be usable as a parent for another service (depth capped at 2: parent → child, never parent → child → grandchild).
- [ ] Group membership and the parent-dependency link SHALL combine freely — a grouped service can also have a parent, and a parent/child pair can exist outside any group.
- [ ] The public status page SHALL render groups as labeled sections, and a child service nested/indented under its parent's card, while every service (grouped or not, parent or child) keeps rendering its own independent status card exactly as today.
- [ ] Deleting a group or a service SHALL NOT be blocked by this feature — members of a deleted group fall back to "no group"; a service that was a parent or child falls back to "no parent" (`ON DELETE SET NULL`), same no-friction-delete posture the rest of the domain model already has.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Group-level aggregated status (a group showing a "worst of its members" summary status) | Explicitly rejected this session — group is a pure visual container, never computed/shown a status of its own. |
| Parent-driven status aggregation (parent's status computed from its child) | Explicitly rejected — each service, parent or child, keeps its own independently-computed status (SLO or polling), unaffected by the link. |
| More than 2 levels of service nesting (parent → child → grandchild) | Explicitly rejected — depth is capped at 2; a service with a parent cannot itself be a parent. |
| Nested groups (a group inside a group) | Not requested — groups are a single flat list per status page, services/children nest under them, groups do not nest under each other. |
| A service with more than one parent | Explicitly rejected — at most one `parent_service_id` per service-on-a-page; this is a tree, not a graph. |
| Group or parent-link scoped globally to the service (instead of per status page) | Explicitly rejected — the same service can be organized differently across different status pages it's attached to. |
| Changing how `status_page_services`' full-replace write pattern works (optimistic locking, partial/incremental updates) | Out of scope — this spec keeps the existing last-write-wins full-replace semantics `SetServices` already has today; introducing concurrency control is a separate, unrequested change. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Group/dependency scope | Per status page (on `status_page_services`), not global on `Service` | Confirmed explicitly by the user this session — the same service may be organized differently on different pages. | y |
| Parent-child semantic | Informational dependency only, each service keeps its own independently-computed status | Confirmed explicitly — rejected both "group has aggregated status" and "parent status computed from child" alternatives. | y |
| Nesting depth | Fixed 3 conceptual levels: group → service → child-service; a child cannot itself be a parent | Confirmed explicitly over an arbitrary-depth tree, to keep the model and UI simple and match the user's own stated example exactly. | y |
| Group and dependency combination | Independent, freely combinable attributes of the same `status_page_services` row | Confirmed explicitly — a grouped service can also have a parent/child. | y |
| Multiplicity of parent | At most one parent per service-on-a-page (tree, not graph) | Confirmed explicitly over allowing a service to have multiple parents. | y |
| Admin UI interaction model | Drag-and-drop (`@dnd-kit/core`/`@dnd-kit/sortable`) | Confirmed explicitly by the user over a simpler dropdown+buttons control, despite the latter needing zero new dependencies. This is the first drag-and-drop UI in the codebase — Design should scope the dependency addition and accessibility (keyboard operability) explicitly. | y |
| `parent_service_id` FK target | References `services(id)` directly, not `status_page_services` | The existing PK on `status_page_services` is a composite `(status_page_id, service_id)`, not independently referenceable by a single FK; "same page" and "no grandparent" are validated in Go, not via a Postgres `CHECK` against sibling rows. | y (mechanism), n (Design confirms exact validation placement — handler vs repository) |
| Write pattern for service placement (group/parent/position) | Extends the existing full-replace `PUT /api/status-pages/{id}/services` (today `service_ids: string[]`) to a richer per-service object, same last-write-wins replace-the-whole-set semantics | Matches the existing `SetServices`/`sameServiceSet` pattern already in the codebase (`StatusPageEditorContent.tsx`) rather than inventing incremental/patch semantics for one sub-feature. | y (pattern), n (Design confirms the exact request/response shape) |
| Group CRUD surface | Separate endpoints (`POST`/`PATCH`/`DELETE /api/status-pages/{id}/groups[/{groupId}]`), since a group must exist (and have an id) before a service placement can reference it | Matches REST convention already used elsewhere in the admin API (domains, incidents) for first-class sub-resources. | y (pattern), n (Design confirms exact routes/payloads) |

**Open questions:** none blocking Design.

---

## User Stories

### P1: Organize a status page's services into named groups ⭐ MVP

**User Story**: As an operator with many services on one status page, I want to group related services under a named heading, so visitors can scan the page by area instead of one flat list.

**Why P1**: This is the core organizational need driving the whole feature — without it, the page stays a flat list regardless of what else ships.

**Acceptance Criteria**:

1. WHEN an operator creates a group on a status page THEN the system SHALL persist a named, ordered group scoped to that status page. <!-- event-driven -->
2. WHEN an operator assigns a service (as placed on that status page) to a group THEN the public status page SHALL render that service's card under that group's labeled section. <!-- event-driven -->
3. WHERE a service on a status page has no group assigned, the public status page SHALL render its card outside any group section, exactly as today. <!-- state-driven -->
4. WHEN an operator deletes a group THEN every service that was in it SHALL become ungrouped (not deleted, not left dangling) on that status page. <!-- event-driven -->
5. WHEN an operator renames a group or reorders groups THEN the public status page SHALL reflect the new name/order. <!-- event-driven -->
6. WHEN an operator reorders services within a group, or within the ungrouped set, THEN the public status page SHALL render them in that order. <!-- event-driven -->

**Independent Test**: Create a status page with 3 services, create a group, assign 2 of the 3 services to it, verify the public page shows a labeled section with those 2 and the 3rd service outside it. Delete the group, verify all 3 render ungrouped. Reorder the 2 grouped services, verify the public page reflects the new order.

### P2: Express a dependency between two services on the same status page

**User Story**: As an operator, I want to mark one service as depending on another on the same status page, so visitors see the relationship (e.g. "API" depends on "Database") without that relationship affecting either service's own computed status.

**Why P2**: Direct consequence of the same underlying need as P1 (structuring a busy page), but independent of grouping — a dependency link can exist with or without a group.

**Acceptance Criteria**:

1. WHEN an operator sets a service's parent to another service on the same status page THEN the system SHALL persist that link and render the child nested/indented under the parent's card on the public page. <!-- event-driven -->
2. WHERE a service already has a parent, the system SHALL reject an attempt to set that service as the parent of another service. <!-- state-driven -->
3. WHERE a service has a parent-child link, each of the two services SHALL continue rendering its own independently-computed status (SLO or polling), unaffected by the link. <!-- state-driven -->
4. WHEN an operator sets a parent-child link combined with group membership (grouped parent, or grouped child, or both) THEN the system SHALL persist and render both relationships simultaneously, without either overriding the other. <!-- event-driven -->
5. WHEN an operator attempts to set a service's `parent_service_id` to a service not attached to the same status page THEN the system SHALL reject the request (422) without persisting a cross-page reference. <!-- event-driven -->
6. WHEN an operator attempts to set a service's `parent_service_id` to itself THEN the system SHALL reject the request (422). <!-- event-driven -->
7. WHEN a parent service is removed from the status page (or deleted entirely) THEN every service that depended on it SHALL become parentless (not deleted, not left dangling) on that status page. <!-- event-driven -->

**Independent Test**: On a status page with services A and B, set B's parent to A, verify the public page nests B under A while both show independent status badges. Attempt to set a 3rd service C's parent to B (already a child), verify rejection. Remove A from the page, verify B renders parentless.

---

## Requirement Traceability

| ID | Acceptance Criterion (User Story) | Status |
| --- | --- | --- |
| SGD-01 | P1-AC1: group created, named, ordered, scoped to status page | pending |
| SGD-02 | P1-AC2: grouped service renders under group's section | pending |
| SGD-03 | P1-AC3: ungrouped service renders outside any section | pending |
| SGD-04 | P1-AC4: deleting a group ungroups its members, doesn't delete them | pending |
| SGD-05 | P1-AC5: rename/reorder group reflected on public page | pending |
| SGD-06 | P1-AC6: reordering services within a group/ungrouped set reflected on public page | pending |
| SGD-07 | P2-AC1: parent-child link persisted and rendered nested | pending |
| SGD-08 | P2-AC2: a child cannot itself become a parent (depth cap) | pending |
| SGD-09 | P2-AC3: parent/child each keep independently-computed status | pending |
| SGD-10 | P2-AC4: group membership and parent-child link combine freely | pending |
| SGD-11 | P2-AC5: cross-page `parent_service_id` rejected (422) | pending |
| SGD-12 | P2-AC6: self-referential `parent_service_id` rejected (422) | pending |
| SGD-13 | P2-AC7: removing/deleting a parent leaves children parentless, not dangling | pending |
