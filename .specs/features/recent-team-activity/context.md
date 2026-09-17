# Recent Team Activity — Discuss Context

## Gray area: how to resolve a human-readable target name for each audit entry

`admin_audit_log.target_id` is a bare UUID with no `target_type` column — it can point at an admin, an invite, a domain, or a status page depending on which `action` the row records, and there is no way to tell which from the row alone.

**Decision (user-confirmed via `AskUserQuestion`): store a `target_label` column, filled in by the caller at `Record()` time, instead of resolving the name later via a join.**

Rejected alternatives and why:
- **`target_type` + runtime join**: normalizes the schema better, but breaks the moment the target row is deleted (a domain or status page can be deleted after the audit entry is written — the whole point of an audit log is that it survives the thing it describes disappearing). Would need a `LEFT JOIN` plus a fallback label anyway, which is strictly more complexity for a worse guarantee.
- **Generic action only, no target name**: simplest, but throws away the most useful part of the display (the mock, and the user's own expectation, is "Fulano verificou domínio X", not "Fulano verificou um domínio").

`target_label` is a plain `TEXT`, nullable (existing rows have none), populated at the exact moment each `audit.Log.Record` call already has the human-readable value in hand:

| Action | Call site | Label value | Note |
| --- | --- | --- | --- |
| `invited` | `admins.go:222` | `req.Email` | already in scope |
| `resent` | `admins.go:484` | `invite.Email` | already in scope (`invite` from `Refresh`) |
| `canceled` | `admins.go:518` | invite's email | **not currently fetched** — `TenantInviteRepository.Cancel` only takes/returns `error` today; needs to return the canceled `*TenantInvite` (same `RETURNING` shape `Refresh` already uses) so its `Email` is available at the call site |
| `role_changed` | `admins.go:608` | target admin's name/email | **not currently fetched** — `UserFromContext`/`req` don't carry it; needs a `h.users.GetByID(ctx, targetID)` lookup before the call |
| `removed` | `admins.go:675` | target admin's name/email | **must be fetched before `h.users.Delete` runs** (line 668, a few lines above `Record`) — the row is gone by the time `Record` executes today; the label lookup has to happen before delete |
| `domain_verified` | `domains_handler.go:312` | the domain string | already in scope (in-handler `domain`/`id` context) |
| `domain_deleted` | `domains_handler.go:349` | the domain string | must be captured **before** the delete call runs, same ordering concern as `removed` |
| `status_page_deleted` | `status_pages_handler.go:272` | the status page's name | must be captured before delete |
| `status_page_domain_verified` | `status_pages_handler.go:355` | the status page's name | already in scope |

**`audit.Log.Record`'s signature changes** from `Record(ctx, actorID, targetID, action string) error` to `Record(ctx, actorID, targetID, targetLabel, action string) error` — every existing call site is touched (8 total), each passing whatever human-readable value it already has (or now fetches) in hand. This is the feature's actual migration-blast-radius: one column, one signature change, 8 call-site updates, all mechanical once each site's label source is identified (table above).

## Gray area: display volume / pagination

**Decision: last 5 entries, no pagination.** The Overview page's card is a summary, not an audit trail browser — matches the current mock's own item count. A future dedicated audit-log screen (if ever built) is a separate feature with its own pagination needs; this feature does not build one.

## Gray area: new endpoint vs. embedding in `GET /api/overview`

**Decision: new dedicated `GET /api/audit-log?limit=5` endpoint.** Keeps `OverviewHandler`/`OverviewResponse` unchanged (already composes services/incidents/domains) and is reusable if a future dedicated audit-log view is ever built. `OverviewPage.tsx`'s `RecentActivity` component makes its own `useQuery` call, same pattern the page's other cards already follow (`useOverview` isn't the only hook the page uses — e.g., none currently, but the pattern of "one card, one hook" is idiomatic here and matches how other multi-card pages like `SettingsPage` are composed).

## Gray area: authorization

**Decision: any authenticated role, no gate — same as today.** The card is already visible to every role on Overview; audit-log entries describe administrative actions but aren't secret-bearing (no tokens/passwords in `target_label`), so this doesn't raise the exposure bar the rest of Overview already sits at.

## Action → display copy mapping (i18n)

8 actions need a pt-BR/en phrase, each interpolating `actor` (audit entry's actor name, resolved via a join to `users`/`tenant_memberships` — NOT stored as a label, since the actor is virtually never deleted mid-session and a real-time name is more accurate than a snapshot) and `target` (the new `target_label` column, used verbatim):

| Action | pt-BR | en |
| --- | --- | --- |
| `invited` | "{{actor}} convidou {{target}}" | "{{actor}} invited {{target}}" |
| `resent` | "{{actor}} reenviou convite para {{target}}" | "{{actor}} resent the invite to {{target}}" |
| `canceled` | "{{actor}} cancelou convite de {{target}}" | "{{actor}} canceled the invite for {{target}}" |
| `role_changed` | "{{actor}} alterou o papel de {{target}}" | "{{actor}} changed {{target}}'s role" |
| `removed` | "{{actor}} removeu {{target}}" | "{{actor}} removed {{target}}" |
| `domain_verified` | "{{actor}} verificou o domínio {{target}}" | "{{actor}} verified the domain {{target}}" |
| `domain_deleted` | "{{actor}} excluiu o domínio {{target}}" | "{{actor}} deleted the domain {{target}}" |
| `status_page_deleted` | "{{actor}} excluiu a página {{target}}" | "{{actor}} deleted the page {{target}}" |
| `status_page_domain_verified` | "{{actor}} verificou o domínio da página {{target}}" | "{{actor}} verified the domain for page {{target}}" |

An action string encountered that isn't in this map (future action added to `audit.Log.Record` without updating this map) falls back to a generic "{{actor}} performed {{action}} on {{target}}" phrase — never a crash, never a blank row (mirrors this codebase's existing "never leak/never crash on unknown enum" posture, e.g. billing's unrecognized-plan handling).

## Actor resolution

`actor_id` joins to `users`/the tenant's membership to get a display name. If the actor's user row is gone (hard-deleted account — rare but the `removed` action's own existence proves accounts can be deleted), fall back to a fixed placeholder ("Usuário removido" / "Removed user") — never a raw UUID, never a crash.
