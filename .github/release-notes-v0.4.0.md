## What's new in v0.4.0

### Added

- **Service rename and delete** — monitored services can be renamed inline and soft-deleted from the service detail drawer. Poller checks for a deleted service stop cleanly, and concurrent delete/attach operations are serialized to avoid races.
- **Loading skeletons everywhere** — every list and detail screen now shows a real loading skeleton instead of a "Carregando…" text placeholder.
- **Recent team activity** — the Visão geral overview's "Atividade recente do time" card now shows real audit-log entries (who did what, when) instead of mock data.
- **Copy CNAME button** — copy the DNS target value shown in the status page's domain verification panel with one click.

### Changed

- The public-preview link for a status page moved from the edit drawer to the view-details drawer.
- "Planos & Faturamento" only shows in the sidebar for `saas`-mode deployments; self-hosted installs no longer see it.

### Fixed

- Loading skeletons and a few other UI elements no longer follow the OS's dark-mode preference instead of the app's own theme toggle.
- The poller now retries starting on every leader heartbeat, fixing a case where a service could get stuck in `not_configured` after a delayed Datadog integration connect.
- A handful of visual polish fixes across the auth layout, add-service drawer, and service list.

### Upgrade notes

No breaking changes, no manual migration steps beyond the usual `helm upgrade` (migrations run automatically on boot). If you run a public-facing status page behind an AWS `LoadBalancer` Service, double-check `charts/zeep-vane/values.yaml`'s `publicService.annotations` — see the README's LoadBalancer section if `status.yourdomain.com` fails TLS issuance with "no valid A/AAAA records found".
