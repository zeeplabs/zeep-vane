## Vane v0.2.1

### Fixed

- A status page with a domain attached could never actually publish: `AttachDomain` left the page in the `draft` state, but on-demand TLS issuance refuses to even attempt a certificate for a `draft` page — an unconditional deadlock present since the original TLS design, only surfaced by the first real-world DNS/certificate attempt against v0.2.0. Attaching a domain now moves the page to a new `pending_tls` state, which issuance is allowed to act on. If you have a status page stuck showing "Aguardando validação de DNS/certificado" from before this release, the embedded migration backfills it automatically on upgrade.

### Added

- A status page's detail screen now shows a persistent DNS/certificate panel (owners/operators only) — the DNS record to configure and a "Verificar DNS/certificado" button that performs a real DNS lookup and TLS handshake against the page's public hostname on demand, similar to the custom-domain verification flow on platforms like Vercel or Render. DNS is matched by resolved IP overlap (robust to CNAME chains and plain A records), and the served certificate is validated against the system root pool rather than treating a bare TLS handshake as proof of a legitimate certificate. The panel stays visible until the page is published.

### Upgrading from v0.2.0

No new configuration required. If `PUBLIC_DNS_TARGET`/`config.publicDnsTarget` (Helm) is set, the new verification panel uses it to check DNS by IP overlap; if unset, DNS resolution is still checked, just without a target to compare against.

### Installing

```bash
docker pull ghcr.io/zeeplabs/zeep-vane:0.2.1
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
