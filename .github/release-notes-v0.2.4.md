## Vane v0.2.4

### Fixed

- **Critical**: `v0.2.3` broke every SLO poll immediately on deploy. Datadog's SLO history response serializes `series.denominator.sum` as a JSON float (e.g. `45.0`) even though it's always a whole request count, but this field decoded straight into a Go `int64` — every real poll against Datadog failed with `json: cannot unmarshal number 45.0 into ... of type int64`, freezing every service's cached status at whatever it was before the upgrade. No automated test fixture used a float literal for this field, so none of `v0.2.3`'s pre-release checks caught it against a real Datadog response shape. Now decodes as a float and rounds to the nearest integer.

### Upgrading from v0.2.3

- **If you already deployed v0.2.3, upgrade to this version immediately** (or roll back `image.tag` to `0.2.2` in the meantime) — v0.2.3's poller cannot successfully complete a single Datadog fetch, so every service's status silently stops updating on that version.
- No new configuration, no database migrations.
- Everything else from `v0.2.3`'s release notes still applies — see below.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.4
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

---

## Vane v0.2.3 (superseded by the fix above — see v0.2.3's own release notes for the full list of fixes it shipped)
