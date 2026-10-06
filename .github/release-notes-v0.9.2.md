# v0.9.2

## Changed

**Root domain ownership verification switched from CNAME/TLS to a TXT record.** Registering a root domain in `vane` now generates a unique verification token and asks you to create a `_vane-verify.<hostname>` TXT record with that token, instead of a CNAME on the bare apex plus a TLS handshake. The old flow was unsatisfiable for any root domain already in production use (MX, SPF/DKIM, a marketing site, etc.) — a CNAME can't coexist with other records at the zone apex, and following the old instructions literally would have broken the domain's real DNS. `POST /api/domains/{id}/verify` now does a real DNS TXT lookup and marks the domain `verified` only on an exact token match, with separate "not found" / "value mismatch" error messages.

The subdomain-attach flow you use to actually publish a status page (`Anexar domínio`, CNAME to `PUBLIC_DNS_TARGET`) is unchanged.

**`ssl_status` removed from root domains.** A root domain never had its own TLS certificate — only the attached subdomain does. The SSL card/column is gone from the domain detail view and list, and `ssl_status` no longer appears in domain API responses.

## Upgrading

- Migrations `0044`/`0045` run automatically on startup (`domains.verification_token` added and backfilled for every existing row, `domains.ssl_status` dropped).
- If you already verified a root domain under the old CNAME scheme, that domain stays `verified` as-is — no forced re-check. The *next* time you run `Verify` on it, the TXT check runs instead of the old CNAME check, so add the new `_vane-verify.<hostname>` TXT record first or that next verification attempt will fail.
- No new environment variables or configuration.
