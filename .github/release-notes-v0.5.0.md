## What's new in v0.5.0

### Added

- **Disconnect an email or LLM provider** — owners/operators can now disconnect a connected SendGrid, Resend, or OpenAI integration directly from the Integrations page, with a confirmation dialog before the stored API key is removed for good. Disconnecting the currently active provider is allowed and leaves the account cleanly without one until you reconnect.
- The Integrations page's provider cards now show an "Ativar" action on a connected-but-inactive provider and a distinct "Ativo" indicator on the active one.

### Security

- Two provider queries (`GetActiveProvider`, `DeleteProvider`, email and LLM) now filter explicitly by tenant instead of relying solely on Postgres row-level security, which a superuser database role bypasses entirely regardless of `FORCE ROW LEVEL SECURITY`.

### Removed

- Two admin pages that were fully built and tested but never actually reachable from any menu or link — dead code. Their behavior now lives in the Integrations page's cards instead.

### Upgrade notes

No breaking changes, no manual migration steps beyond the usual `helm upgrade` (migrations run automatically on boot). If your Postgres connection role for Vane is a superuser, consider moving to a least-privilege role — row-level security tenant isolation only holds for non-superuser roles.
