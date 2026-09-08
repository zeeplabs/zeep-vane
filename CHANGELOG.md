# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- The public status page could show a service stuck as "interrupção" for hours after it had actually recovered: the poller derived a service's status from Datadog's SLO `state`, which reflects error-budget compliance over the SLO's fixed configured timeframe (30 days for every SLO in use) rather than current health — an old error burst kept the 30-day budget negative long after the service was healthy again. The poller now fetches a short, recent window of SLO history from Datadog instead, with a minimum-request-volume guard that carries the previous status forward when there isn't enough recent traffic to trust a recompute, so a quiet service doesn't flap on noise (`AD-019`).
- A non-leader replica in a multi-replica deployment could start a second, unmanaged SLO poller if an admin connected or rotated the Datadog integration through it — `PollerManager.Restart` had no leadership check, only `RunLeaderLoop` did. Restart is now a no-op on a non-leader replica; the actual leader still picks up the change without a process restart. A narrower race in the leadership check itself (found by a second independent review of this exact fix) was also closed — the check and the leadership-loss/stop transition now share one lock.
- A leader-election heartbeat check could block past its own interval on a Postgres restart or network partition (no per-probe timeout), risking two replicas both believing they hold poller leadership at once. Each heartbeat probe is now bounded to the heartbeat interval.
- The rate limiter's Postgres-backed token bucket could let a burst of concurrent first-ever requests from the same new IP all through, briefly exceeding the configured burst ceiling — the very first request for an IP had nothing to lock, so concurrent requests didn't serialize against each other. The bucket row is now seeded before it's locked, so even the first requests for a new IP serialize correctly. Each request's wait for that lock is now also bounded to 3 seconds, so a burst hammering one IP can't indefinitely pin a connection out of the process's small connection pool.
- `FetchSLOStatus` picked an arbitrary threshold (Target/Timeframe) on every call for any Datadog SLO configured with more than one threshold, because Go map iteration order is randomized — the pick is now deterministic. Also corrected `ErrorBudgetRemaining`'s sign to match Datadog's own convention (positive when healthy); still an internal-only approximation, not read by any handler or the frontend.
- CertMagic's Postgres-backed certificate storage matched key prefixes with an unescaped `LIKE 'prefix/%'`, which would misinterpret a literal `_`/`%` in a key as a wildcard. Switched to `starts_with()`.
- `VANE_ADMIN_BASE_URL` set without a scheme (e.g. `admin.example.com` instead of `https://admin.example.com`) was silently accepted and produced admin-facing password-reset/invite emails linking to plain `http://`. Now rejected at startup with a clear error, and documented in `.env.example`/`docker-compose.yml`, where it was previously missing.
- The public status page could flap between "operacional" and "interrupção" several times an hour on a high-traffic service, even with a healthy 30-day SLO: Datadog computes a requested window's `state` against the SLO's configured target (typically 99.5% over 30 days) without rescaling it for the window's actual width, so a single burst of errors well within normal 30-day noise could trip "breached" on its own 5-minute window - live-measured at 2 out of 6 consecutive windows on a real, healthy SLO. A service now only flips to "outage" after 2 consecutive breached polling cycles; a lone breached cycle carries the previous status forward instead (`AD-019 addendum 3` - a stopgap, not a statistically definitive fix).
- `VANE_ADMIN_BASE_URL` validation only checked for a `http://`/`https://` prefix, so a value that trims to empty (e.g. `/`) still passed and was silently treated as unconfigured instead of rejected. Now parsed as a URL and requires both a scheme and a non-empty host - and also rejects a malformed host/port, a query string, a fragment, or embedded credentials, none of which the prefix check could catch and any of which would have produced a broken or credential-leaking admin-facing email link.
- Every rate-limit integration test cleaned up its Postgres rows with an unscoped `DELETE FROM rate_limit_buckets`, which could delete another concurrently-running test package's in-progress bucket rows under `go test ./...`'s package parallelism, occasionally causing an unrelated test to see an already-exhausted burst. Each test now deletes only the specific IP(s) it used.

### Changed

- `POLL_INTERVAL_SECONDS`'s default example value changed from `60` to `120` to match the public status page's own 2-minute refresh cadence. It now controls poll frequency only — each fetch always requests a fixed 5-minute window of recent SLO history from Datadog, lagged 60 seconds behind "now" so it never reads data Datadog hasn't finished aggregating yet (part of `AD-019`).

## [0.2.2] — 2026-08-31

### Fixed

- A published status page's own custom domain served the raw status JSON instead of the actual rendered page: production traffic on a custom domain was never given anywhere to get HTML/JS from — only the JSON endpoint and the logo file were routed. The public listener now also serves the embedded SPA, with the JSON endpoint moved to its own `/api/public-status` path (`AD-018`).

## [0.2.1] — 2026-08-31

### Fixed

- A status page with a domain attached could never actually publish: `AttachDomain` left the page in the `draft` state, but on-demand TLS issuance refuses to even attempt a certificate for a `draft` page — an unconditional deadlock present since the original TLS design, only surfaced now by the first real-world DNS/certificate attempt. Attaching a domain now moves the page to a new `pending_tls` state, which issuance is allowed to act on (`AD-017`).

### Added

- A status page's detail screen now shows a persistent DNS/certificate panel, visible to owners/operators — the DNS record to configure and a "Verificar DNS/certificado" button that performs a real DNS lookup and TLS handshake against the page's public hostname on demand, similar to the custom-domain verification flow on platforms like Vercel or Render. DNS is matched by resolved IP overlap (robust to CNAME chains and plain A records) and the served certificate is validated against the system root pool, not just "a TLS handshake completed." The panel stays visible until the page is published.

## [0.2.0] — 2026-08-31

### Security

- Fixed a host-header injection vulnerability in `POST /api/auth/password-reset/request` and admin-invite emails: the emailed link was built from the incoming request's `Host` header, which is attacker-controlled on this unauthenticated endpoint — an attacker could email a real victim a password-reset link pointing at a host of their choosing. Links are now built exclusively from the new `VANE_ADMIN_BASE_URL` config value; see `AD-014`.
- Closed the account-enumeration timing oracle this fix could otherwise reopen: token generation, persistence, and email dispatch all run detached from the request/response cycle, so `Request`'s response time and status no longer differ between a known and an unknown email.
- Password reset (`POST /api/auth/password-reset/confirm`) now invalidates every other pending reset token for the admin and revokes all of the admin's existing sessions, so a still-valid sibling reset link or a session obtained before the reset can no longer be used afterward.

### Added

- Admin accounts now have a required `name` and optional international `phone` number, collected at invite/bootstrap time (`AD-015`). The invite dialog's role picker is three buttons instead of a dropdown.
- Domains and status pages can now be deleted from the admin dashboard, with a confirmation dialog and a 409 response when a domain is still attached to a status page.
- The logged-in admin's name and email are shown in the sidebar above the sign-out button.
- Integrations, Services, Domains & Status Pages, Poller Status, and Admins were redesigned from table layouts to card-based lists; every modal's footer is now consistently right-aligned with a border separating it from the modal's content.

### Fixed

- A 422 (weak password) response during account activation or password reset previously surfaced the backend's raw English error string; it now shows a translated message like every other error on those screens.
- `AcceptInvitePage` now falls back to the Vane logo when no company logo is configured, matching every other auth screen, instead of a generic star icon.
- `VANE_ADMIN_BASE_URL` (introduced by the host-header injection fix above) had no way to be set through the Helm chart; added `config.adminBaseUrl` to `values.yaml`.

## [0.1.0] — 2026-08-31

### Added

- First release: self-hosted status-page platform connecting Datadog SLOs to public status pages with automatic TLS (CertMagic, on-demand ACME) for operator-registered domains, admin dashboard with role-based access (owner/operator/viewer), incident management, and email-based admin invites (SendGrid/Resend).
- Postgres-backed coordination for running more than one `vane serve` replica (`internal/pglock`): the poller now elects a single leader via a Postgres advisory lock instead of every replica polling Datadog independently, the per-IP rate limiter (login/password-reset/invite-accept/bootstrap) enforces its limit across all replicas sharing one database instead of an in-memory map, and CertMagic's certificate storage moved from local disk (`certmagic.FileStorage`) to a Postgres table (`internal/tls.PostgresStorage`) so any replica can serve TLS for any registered domain.
- Helm chart (`charts/zeep-vane`) for Kubernetes deployment, defaulting to 2 replicas to exercise the above in a real cluster.

### Removed

- `CERTMAGIC_STORAGE_PATH` environment variable and the Helm chart's `persistence.*` values / `templates/pvc.yaml` (`ReadWriteOnce` PVC) — no longer needed now that certificate storage lives in Postgres.
