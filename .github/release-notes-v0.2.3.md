## Vane v0.2.3

### Fixed

- The public status page could show a service stuck as "interrupção" for hours after it had actually recovered: the poller derived a service's status from Datadog's SLO `state`, which reflects error-budget compliance over the SLO's fixed configured timeframe (30 days for every SLO in use) rather than current health — an old error burst kept the 30-day budget negative long after the service was healthy again. The poller now fetches a short, recent window of SLO history from Datadog instead, with a minimum-request-volume guard that carries the previous status forward when there isn't enough recent traffic to trust a recompute, so a quiet service doesn't flap on noise (`AD-019`).
- The public status page could also flap between "operacional" and "interrupção" several times an hour on a high-traffic service, even with a healthy 30-day SLO: Datadog computes a requested window's `state` against the SLO's configured target (typically 99.5% over 30 days) without rescaling it for the window's actual width, so a single burst of errors well within normal 30-day noise could trip "breached" on its own 5-minute window. A service now only flips to "outage" after 2 consecutive breached polling cycles; a lone breached cycle carries the previous status forward instead. This is a deliberate stopgap, not a statistically definitive fix — see `AD-019 addendum 3` in `.specs/STATE.md` for the full analysis and what a more robust fix would need.
- A non-leader replica in a multi-replica deployment could start a second, unmanaged SLO poller if an admin connected or rotated the Datadog integration through it. Restart is now a no-op on a non-leader replica.
- A leader-election heartbeat check could block past its own interval on a Postgres restart or network partition, risking two replicas both believing they hold poller leadership at once. Each heartbeat probe is now bounded to the heartbeat interval.
- The rate limiter's Postgres-backed token bucket could let a burst of concurrent first-ever requests from the same new IP all through, briefly exceeding the configured burst ceiling.
- `FetchSLOStatus` picked an arbitrary SLO threshold on every call for any Datadog SLO configured with more than one threshold, because Go map iteration order is randomized — the pick is now deterministic.
- CertMagic's Postgres-backed certificate storage matched key prefixes with an unescaped `LIKE 'prefix/%'`, which would misinterpret a literal `_`/`%` in a key as a wildcard.
- `VANE_ADMIN_BASE_URL` validation was a loose prefix check — a value missing a scheme, missing a host, or trailing into a query string/fragment/embedded credentials could be silently accepted and produce a broken or credential-leaking admin-facing email link. Now strictly validated at startup with a clear error.

### Changed

- `POLL_INTERVAL_SECONDS`'s default example value changed from `60` to `120` to match the public status page's own 2-minute refresh cadence. It now controls poll frequency only — each fetch always requests a fixed 5-minute window of recent SLO history from Datadog, lagged 60 seconds behind "now" so it never reads data Datadog hasn't finished aggregating yet.

### Upgrading from v0.2.2

- **Check `VANE_ADMIN_BASE_URL` before upgrading, if you have it set.** Validation is now strict: it must be a full `http://` or `https://` URL with a host, and no query string, fragment, or embedded credentials (e.g. `https://admin.example.com` — not `admin.example.com`, `https://`, or a value carrying `?`/`#`/`user:pass@`). A value that previously booted despite being malformed will now cause `vane serve` to exit with a clear startup error instead of silently emailing broken links. If unset, no action needed.
- No database migrations in this release.
- If you're running multiple replicas, this release also closes a narrow window where a non-leader replica could start a duplicate poller during a Datadog credential change — no configuration change needed, it's automatic on upgrade.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.3
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
