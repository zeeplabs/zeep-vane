## Vane v0.2.5

### Added

- The public status page's per-service history chart now has a page-wide time-range selector: **24h** (1h bars, unchanged default), **7d** (6h bars), **30d** (1d bars), and **90d** (1d bars). Switching ranges refetches without a page reload, applies to every service's chart at once, and recomputes the uptime % figure for the selected window instead of leaving it pinned to 24h. Works identically on the authenticated preview endpoint used by the admin dashboard.

### Fixed

- An interval ending shortly before a chart's window start could bleed its status into the window's first bucket instead of being excluded — a pre-existing off-by-one in the bucket-index math, previously a 1-hour leak band invisible in practice, now fixed before it became reachable on the new 30d/90d range tiers (which use 24-hour buckets).

### Changed

- **Breaking (public JSON)**: the public status API's per-service `hourly_history` field is renamed to `history`, since it no longer holds only-ever-hourly buckets once a longer range is selected. No dual-field compatibility shim — any third-party integration reading Vane's public status JSON directly (not through the shipped frontend) needs to update the field name it reads.
- `status_intervals` retention raised from 35 to 95 days, so the new 90-day range tier actually has up to 90 real days of history to show instead of being silently mostly empty on any instance older than 35 days. Operational implication: closed `status_intervals` rows now accumulate for up to 95 days before being pruned instead of 35 — more storage for instances with many services/frequent status changes.

### Upgrading from v0.2.4

- No database migrations. No new required configuration.
- If your deployment (or a third-party integration) reads the public status JSON directly rather than through Vane's own frontend, update it for the `hourly_history` → `history` field rename before upgrading.
- Storage: closed `status_intervals` rows are now retained for up to 95 days instead of 35 — expect proportionally more rows in that table on instances with many services or frequent status changes.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.5
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-vane/helm
helm repo update
helm upgrade --install zeep-vane zeeplabs/zeep-vane \
  --set secrets.databaseUrl="postgres://user:pass@host:5432/vane?sslmode=require" \
  --set secrets.vaneMasterKey="$(openssl rand -hex 32)" \
  --set secrets.vaneSessionSecret="$(openssl rand -hex 32)" \
  --set config.adminBaseUrl="https://admin.example.com"
```

See [README.md](https://github.com/zeeplabs/zeep-vane/blob/main/README.md) for full configuration and [charts/zeep-vane](https://github.com/zeeplabs/zeep-vane/tree/main/charts/zeep-vane) for chart values.
