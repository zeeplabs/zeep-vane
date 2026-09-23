## Added

- **Full pt-BR/English internationalization.** The admin SPA now ships a real language selector (Meu Perfil / Configurações), persisted per browser — every screen (Billing, Admins, Incidents, Domains & Status Pages, Integrations, Services, Poller, Settings) is fully translated via `react-i18next`. The public status page (`/status/:id`) detects the visitor's browser language independently of the logged-in admin's own choice. A new `npm run i18n:check` CI gate keeps `pt-BR.json`/`en.json` in parity going forward.
- **LLM root-cause enrichment from Datadog Error Tracking.** When a monitored service's linked SLO is `metric`-type with a clean single-service tag, an operator can opt in from the Integrations page (off by default) to have LLM-generated degraded/outage descriptions enriched with the real top error (`error_type`/`error_message`) pulled from Datadog Error Tracking for that service's last 10 minutes — instead of only SLO numbers. Composite (`flow:`-tagged) SLOs and any lookup failure fall back silently to today's behavior; the LLM dispatch is never blocked by this.

## Fixed

- Dark/light theme could desynchronize from the toggle after a page reload — the app's own Content-Security-Policy silently blocked the inline theme-boot script in production. It now runs as a real imported module, so the theme is correct on first paint.
- Timestamps across the SPA stayed in Portuguese formatting even after switching the UI to English. Formatting is now locale-aware everywhere, consolidated into one shared helper.

## Known issues

- **SaaS-only**: a tenant created via public `/signup` on a SaaS deployment has no email provider connected yet, so the verification email currently fails to send, leaving the new owner unable to complete login. This does not affect self-hosted installs (the default deployment mode), which never depend on this flow. A fix is designed (`.specs/features/saas-transactional-email/`) but not yet implemented — tracked for a following release.

## Upgrade notes

One additive migration (`0039_slo_root_cause_enrichment`): two new nullable `services` columns (`slo_type`, `datadog_service_tag`) and one new `llm_settings` boolean (`root_cause_enrichment_enabled`, defaults to `false`). No backfill required, no existing data touched. Standard rolling upgrade.
