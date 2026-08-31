## Vane v0.1.0

First release. Self-hosted status-page and uptime dashboard connected to Datadog SLOs, distributed as a single Go binary with the admin SPA embedded, or as a Docker image / Helm chart.

### Highlights

- **Status pages & incidents**: custom domains with automatic TLS (CertMagic, on-demand ACME) issued the first time a hostname is requested, no manual certificate step; manual incident timeline with resolved/active state; hourly uptime history derived from polled status.
- **Datadog integration**: connect via API/App key, map services to SLOs, poller derives status on an interval and writes to Postgres — the public status page never calls Datadog live.
- **Admin dashboard**: three fixed roles (owner/operator/viewer), admin invites over email (SendGrid or Resend), audit log, paginated list screens across the admin API.
- **Kubernetes-ready**: Helm chart with two Services (admin `ClusterIP`, public `LoadBalancer` for CertMagic's own TLS-terminated listener). Poller leader election, cross-replica rate limiting, and certificate storage are all Postgres-backed — no local disk, no shared volume required to run more than one replica.
- **Single mandatory dependency**: Postgres. No Redis/etcd/object storage anywhere in the stack.

### Upgrading

Nothing to upgrade from — this is the first tagged release.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.1.0
```

```bash
helm repo add zeeplabs https://zeeplabs.github.io/zeep-vane/helm
helm install zeep-vane zeeplabs/zeep-vane \
  --set secrets.databaseUrl="postgres://user:pass@host:5432/vane?sslmode=require" \
  --set secrets.vaneMasterKey="$(openssl rand -hex 32)" \
  --set secrets.vaneSessionSecret="$(openssl rand -hex 32)"
```

See [README.md](https://github.com/zeeplabs/zeep-vane/blob/main/README.md) for full configuration and [charts/zeep-vane](https://github.com/zeeplabs/zeep-vane/tree/main/charts/zeep-vane) for chart values.

### Known gaps

Tracked in [`.specs/STATE.md`](https://github.com/zeeplabs/zeep-vane/blob/main/.specs/STATE.md) and the README's Known gaps section — most notably: no email delivery configured out of the box (password-reset/admin-invite tokens have nowhere to go without a mail provider connected), no auto-discovery of Datadog services/SLOs, and the multi-replica Postgres-backed coordination shipped in this release has been validated with unit/integration tests but not yet against a real multi-pod cluster.
