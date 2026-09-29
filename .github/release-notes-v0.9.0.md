# v0.9.0

## Added

- **Domain health monitoring** (AD-039) — Vane now runs a daily, passive check on every domain you register: RDAP for the expiration date and registrar, and a DNS lookup for nameserver drift. It alerts **once per expiration threshold crossed** (30 / 15 / 7 days) and on NS drift, reusing the existing notification pipeline. NS drift is the signal behind registrar/DNS-host mismatches — the class of problem that made a real incident's recovery slow. This is observability only: Vane never routes traffic on the root domain. The domain detail drawer gained a health section and the domains table an at-risk indicator. No new configuration — the check runs daily at 00:00 UTC. Adds health columns to `domains` and two new notification-preference types (`domain_expiring`, `domain_ns_drift`).
- **Domain expiration and NS-drift notification preferences** — per-user toggles in Meu Perfil, alongside the existing incident and weekly-digest preferences.

## Fixed

- **SaaS first access no longer shows the self-hosted bootstrap screen** (AD-041) — on a fresh `saas` instance with no users yet, the app redirected every anonymous visitor to `/bootstrap` ("Crie a conta do primeiro administrador"). In `saas` mode the app now always opens `/login`, and accounts come through the public `/signup` flow; the first-admin bootstrap screen is `self_hosted`-only. `POST /api/bootstrap` returns `404` in `saas` too; `GET /api/bootstrap/status` stays public in every mode.
- **Domain health: expiration threshold crossing no longer masked by a failed RDAP lookup** — the crossing calculation compared a domain's expiration against the state from the last *attempt*, so a lookup that failed on the day a domain crossed a threshold hid the crossing until the next band. A new `domains.last_rdap_success_at` (migration `0043`) tracks the last *successful* lookup, and the daily check compares against that. The same change stops a transient DNS failure from clearing a previously detected NS drift, and an RDAP response with no expiration event from wiping the stored `expires_at`.
- **Raw RDAP errors no longer leak into the API/UI** — `domains.rdap_last_error` carries a stable, internals-free message; the underlying cause is kept for server-side logging only.
- **Domain expiration alerts only fire when the expiration band narrows** — a renewal that moved a domain from a 5-day to a 20-day window wrongly raised a fresh "30 days" alert.
- **Domain-expired email copy** — renders expired or same-day counts as readable copy ("expired 3 days ago", "expires today").
- **Advisory-lock key collision between background jobs** (AD-040) — the domain-health scheduler reused an advisory-lock key already held by the retention pruner and the TLS key backfill; because the loser silently skips its work, a colliding job could be skipped for a cycle. All production advisory-lock keys now come from one registry, guarded by a uniqueness test.

## Upgrade instructions

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.9.0
```

This release adds migrations `0041`, `0042`, and `0043`. Run `vane migrate up` after upgrading (or restart `vane serve`, which migrates automatically at boot).

Helm:

```bash
helm repo update zeeplabs
helm upgrade zeep-vane zeeplabs/zeep-vane --version 0.9.0
```
