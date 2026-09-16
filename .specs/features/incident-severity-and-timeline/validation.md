# Validation: incident-severity-and-timeline

**Result**: PASS

**Diff range verified:** `473fc49..2fab020` (commits `7d43489`, `fd022d4`, `55a8437`, `2fab020`)

**Verifier:** independent (did not author the implementation).

---

## Per-AC evidence

| Req | Assertion (file:line) | Spec-defined outcome | Covered? |
|---|---|---|---|
| INCSEV-01 | `internal/api/incidents_handler_test.go:983-1005` `TestCreateIncident_ValidSeverityAndDescription_201EchoesBoth` — asserts `rec.Code == 201`, `created.Severity == "critical"` | 201, severity echoed exactly | Yes |
| INCSEV-02 | `internal/api/incidents_handler_test.go:1007-1029` `TestCreateIncident_MissingOrInvalidSeverity_422NoRowCreated` — table cases `""` and `"urgent"`, asserts `rec.Code == 422` and `SELECT COUNT(*) ... = 0` | 422, no row created, both empty and invalid values | Yes |
| INCSEV-03 | Same test as INCSEV-01 (`created.Description == "root cause unknown"`); also `internal/api/incidents_handler_test.go:1031-1055` `TestCreateIncident_EmptyDescription_StoresNil` asserts `created.Description == nil` for `""` input | Persist as given; empty string → `NULL`, not empty string | Yes |
| INCSEV-04 | `internal/db/incident_repository_test.go:347-367` `TestIncidentRepository_SetSeverity_UpdatesAndReturnsIncident` (incl. on a `resolved` incident); `:369-379` `TestIncidentRepository_SetSeverity_UnknownIncident_ErrNotFound`; API-level: `internal/api/incidents_handler_test.go:1057-1149` covers 200/422-unchanged/404/resolved-200 | 200 update, 422 no-op on invalid, 404 unknown ID, severity mutable regardless of status | Yes |
| INCSEV-05 | `internal/db/incident_repository_test.go:381-412` `TestIncidentRepository_AddUpdate_WithAuthorID_PersistsAuthorshipNotAISummary`; `internal/api/incidents_handler_test.go:1151-1176` `TestAddIncidentUpdate_AttributesAuthorID_NotAISummary` — asserts `AuthorID == adminID`, `IsAISummary == false`, using the actor from `UserFromContext` | author_id = authenticated actor, is_ai_summary = false, both fields present in response | Yes |
| INCSEV-06 | `internal/db/incident_repository_test.go:414-437` `TestIncidentRepository_ConfirmPendingClose_AppendsUpdate_NoAuthorIsAISummary`; `internal/api/incidents_handler_test.go:1178-1211` `TestConfirmClose_TimelineEntry_NoAuthorIsAISummary` — asserts `AuthorID == nil`, `IsAISummary == true` | author_id = NULL, is_ai_summary = true on the AI-drafted close entry | Yes |

No spec-precision gaps found — every AC's exact expected status code / field value is asserted literally (not "some error", but the specific `422`/`404`/`200`/`201` and exact string/nil values).

## Schema/migration check

- `7d43489`'s `up.sql` adds `incidents.severity TEXT NOT NULL DEFAULT 'moderate' CHECK (severity IN ('minor','moderate','critical'))` and `incident_updates.author_id UUID NULL REFERENCES users(id)` / `is_ai_summary BOOLEAN NOT NULL DEFAULT false` — matches spec's chosen defaults exactly.
- `55a8437` amends the same migration's `up.sql` (not yet released) to add `ON DELETE SET NULL` to `author_id`'s FK — reasonable, and safe to amend since the migration had not shipped.
- `down.sql` drops both new columns cleanly.

## Auto-created incidents (SLOAnalyzer)

`internal/poller/analyzer.go:227-231` builds `&db.Incident{Title, Description, AutoCreated: true}` without setting `Severity`. `Create`'s `COALESCE(NULLIF($4,''),'moderate')` correctly defaults these to `moderate`, matching the spec's explicit out-of-scope note. Verified via `TestIncidentRepository_Create_NoSeverity_DefaultsToModerate`.

## Discrimination sensor (mutation testing)

Performed in an isolated git worktree (`git worktree add ... 2fab020`), never touching the working tree; discarded via `git worktree remove --force` afterward.

| # | Mutant | Result |
|---|---|---|
| 1 | `incidents_handler.go` `Create`: severity validation short-circuited (`if false && !validIncidentSeverities[...]`) so any/no severity value is accepted | **Caught** — `TestCreateIncident_MissingOrInvalidSeverity_422NoRowCreated` failed (got 201/1-row for `""`, got 500 for `"urgent"` since the DB CHECK constraint rejected it) |
| 2 | `incident_repository.go` `AddUpdate`: insert always passes `nil` for `author_id`, ignoring the caller's `authorID` param | **Caught** — `TestIncidentRepository_AddUpdate_WithAuthorID_PersistsAuthorshipNotAISummary` and `TestAddIncidentUpdate_AttributesAuthorID_NotAISummary` both failed (`AuthorID = <nil>`, want the actor's ID) |
| 3 | `incident_repository.go` `SetSeverity`: query changed from `UPDATE ... SET severity = $2` to a plain `SELECT` (no persistence) | **Caught** — `TestIncidentRepository_SetSeverity_UpdatesAndReturnsIncident` and `TestSetIncidentSeverity_ValidValue_200UpdatesSeverity` both failed (`Severity = "moderate"`, want `"critical"`) |

All 3 injected mutants were caught by the existing test suite. No survivors.

## Build/test gate

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on all changed files (`internal/db/incident_repository.go`, `internal/db/incident_repository_test.go`, `internal/api/incidents_handler.go`, `internal/api/incidents_handler_test.go`, `internal/cli/routes.go`) — no output, all formatted.
- Integration tests against a disposable Postgres 16 container (`verifier-test-pg`, `max_connections=300`, port 5434, destroyed after the run):
  `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/db/... ./internal/api/... ./internal/cli/...` → all packages passed (`internal/db` 15.6s, `internal/api` 34.6s, `internal/cli` 8.7s).

## Notes / minor observations (non-blocking)

- `AddUpdate`'s handler now requires `UserFromContext` to resolve an actor (403 if absent) — not explicitly required by the spec text but necessary to satisfy INCSEV-05 ("authenticated actor's user ID"); the route is already `writeRoles`-gated so this is unreachable in practice, consistent with existing patterns cited in the spec's assumptions table.
- `2fab020` is a docs-only commit (spec.md status flips) — no functional risk, reviewed for consistency with the actual implementation and found accurate.
