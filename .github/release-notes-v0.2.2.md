## Vane v0.2.2

### Fixed

- A published status page's own custom domain served the raw status JSON instead of the actual rendered page. Production traffic on a custom domain was never given anywhere to get HTML/JS from — the public listener only ever routed `/` straight to the JSON handler and `/uploads/` to the logo file, with no static asset serving at all. The public listener now also serves the embedded SPA at `/`, with the JSON endpoint moved to its own `/api/public-status` path.

### Documentation

- Added a note to the Kubernetes (Helm) section of the README: most cloud load-balancer controllers (notably the AWS Load Balancer Controller on EKS) provision an **internal-only** load balancer by default for the chart's public `LoadBalancer` Service, which silently breaks custom-domain TLS issuance and visitor traffic even when DNS is configured correctly — the DNS name resolves, but the connection itself times out from outside the VPC. Set `publicService.annotations` (e.g. `service.beta.kubernetes.io/aws-load-balancer-scheme: internet-facing` on EKS) before attaching a real domain.

### Upgrading from v0.2.1

No new required configuration. If a status page on this instance already shows `published` but visitors see raw JSON instead of a page, this release fixes it on upgrade with no further action needed. If a status page's domain still times out after upgrading, check that its `LoadBalancer` Service is internet-facing (see the README note above) — that is an infrastructure/annotation issue, not something this release's code changes.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.2
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
