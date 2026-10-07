# v0.10.0

## Added

**Service groups and dependencies.** Status pages can now organize services into named, ordered groups and show that one service depends on another — set up with drag-and-drop in the admin editor, visible on the public status page. Existing public API consumers reading the flat `services: []` array keep working unchanged; grouping/dependency data is additive.

**Service environment tag.** Tag a service `development`/`staging`/`production` and see it as a badge across the admin service screens. Purely visual — admin-only, no effect on alerting, filtering, or permissions, never shown on the public page.

**Domain health on the public status page.** If a status page has a verified domain attached, visitors now see an `ok`/`attention` indicator reflecting the domain's existing NS/expiration monitoring — no new checks run, this just surfaces data `vane` already collects.

**Status page environment banner.** Mark a status page `staging`/`development` and its public page shows a fixed banner so it's never mistaken for the real thing.

## Fixed

- Closed a cross-tenant access gap (SaaS mode) where an operator could manipulate another tenant's status-page service groups by guessing/reading a group ID off that tenant's public page.
- The service-groups editor's "Save services" action could silently wipe configured groups and dependencies, and could re-link a service you'd just unlinked. Both fixed — the editor now reconciles the full placement state on every save.
- Added group rename, reorder, and delete directly in the editor UI (previously only reachable via the API).
- Moving a parent service now takes its children's group along with it.
- The public status page no longer 500s if the optional domain-health lookup fails — it just omits that field, same as the rest of this endpoint's fail-soft behavior.
- Smaller fixes: empty service groups no longer show a stray header on the public page; un-parenting a service works correctly; a "Depends on" tag now shows when a service's dependency isn't right next to it visually; a missing database index was added; group ordering no longer risks collisions after deleting a group; pt-BR environment banner wording fixed; the domain-health indicator is keyboard/touch accessible.

## Upgrading

- Migrations `0046`–`0050` run automatically on startup: new `status_page_service_groups` table, `group_id`/`parent_service_id`/`position` columns on `status_page_services`, `services.environment`, `status_pages.environment`, tenant scoping + an index on `status_page_service_groups`.
- No new environment variables or configuration.
- No breaking changes to the public status page API — all new fields are additive.
