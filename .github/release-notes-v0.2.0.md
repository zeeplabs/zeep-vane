## Vane v0.2.0

### Security

- Fixed a host-header injection vulnerability in `POST /api/auth/password-reset/request` and admin-invite emails: the emailed link was built from the incoming request's `Host` header, which is attacker-controlled on this unauthenticated endpoint. Links are now built exclusively from the new `VANE_ADMIN_BASE_URL` config value.
- Closed the account-enumeration timing oracle this could otherwise reopen: token generation, persistence, and email dispatch all run detached from the request/response cycle.
- Password reset now invalidates every other pending reset token for the admin and revokes all of the admin's existing sessions.

### Added

- Admin accounts now have a required `name` and optional international `phone` number, collected at invite/bootstrap time.
- Domains and status pages can now be deleted from the admin dashboard, with a confirmation dialog and a 409 response when a domain is still attached to a status page.
- The logged-in admin's name and email are shown in the sidebar above the sign-out button.
- Integrations, Services, Domains & Status Pages, Poller Status, and Admins were redesigned from table layouts to card-based lists; every modal's footer is now consistently right-aligned with a border separating it from the modal's content.

### Fixed

- A 422 (weak password) response during account activation or password reset previously surfaced the backend's raw English error string; it now shows a translated message.
- `AcceptInvitePage` now falls back to the Vane logo when no company logo is configured.

### Upgrading from v0.1.0

- **New config, strongly recommended**: set `VANE_ADMIN_BASE_URL` to the admin dashboard's public base URL (e.g. `https://admin.example.com`). Password-reset and admin-invite email links no longer derive their host from the incoming request (that was the vulnerability fixed above); if left unset, those links point at an obviously-invalid placeholder host instead of failing silently. See the [Configuration table in README.md](https://github.com/zeeplabs/zeep-vane/blob/main/README.md#-configuration).
- No database migration requires manual action; migrations run automatically on boot.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.0
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
