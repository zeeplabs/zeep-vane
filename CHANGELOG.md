# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Postgres-backed coordination for running more than one `vane serve` replica (`internal/pglock`): the poller now elects a single leader via a Postgres advisory lock instead of every replica polling Datadog independently, the per-IP rate limiter (login/password-reset/invite-accept/bootstrap) enforces its limit across all replicas sharing one database instead of an in-memory map, and CertMagic's certificate storage moved from local disk (`certmagic.FileStorage`) to a Postgres table (`internal/tls.PostgresStorage`) so any replica can serve TLS for any registered domain.

### Removed

- `CERTMAGIC_STORAGE_PATH` environment variable and the Helm chart's `persistence.*` values / `templates/pvc.yaml` (`ReadWriteOnce` PVC) — no longer needed now that certificate storage lives in Postgres.
