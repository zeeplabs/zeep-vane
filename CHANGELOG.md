# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
