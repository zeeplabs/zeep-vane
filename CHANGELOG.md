# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-08-31

### Added

- First release: self-hosted status-page platform connecting Datadog SLOs to public status pages with automatic TLS (CertMagic, on-demand ACME) for operator-registered domains, admin dashboard with role-based access (owner/operator/viewer), incident management, and email-based admin invites (SendGrid/Resend).
- Postgres-backed coordination for running more than one `vane serve` replica (`internal/pglock`): the poller now elects a single leader via a Postgres advisory lock instead of every replica polling Datadog independently, the per-IP rate limiter (login/password-reset/invite-accept/bootstrap) enforces its limit across all replicas sharing one database instead of an in-memory map, and CertMagic's certificate storage moved from local disk (`certmagic.FileStorage`) to a Postgres table (`internal/tls.PostgresStorage`) so any replica can serve TLS for any registered domain.
- Helm chart (`charts/zeep-vane`) for Kubernetes deployment, defaulting to 2 replicas to exercise the above in a real cluster.

### Removed

- `CERTMAGIC_STORAGE_PATH` environment variable and the Helm chart's `persistence.*` values / `templates/pvc.yaml` (`ReadWriteOnce` PVC) — no longer needed now that certificate storage lives in Postgres.
