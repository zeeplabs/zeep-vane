# Security Policy

## Supported Versions

| Version  | Supported |
| -------- | --------- |
| latest   | ✅        |
| < latest | ❌        |

## Reporting a Vulnerability

Please **do not** open public issues for security vulnerabilities.

Send a private report to **security@zeeptecnologia.com.br** or reach out to the maintainers directly.

You can expect:

- Acknowledgment within 48 hours
- Regular updates on the fix progress
- Credit upon disclosure (if desired)

## Responsible Disclosure

We kindly ask that you:

1. Give us reasonable time to fix the issue before public disclosure
2. Provide sufficient details to reproduce and understand the vulnerability
3. Avoid accessing or modifying data beyond what's necessary to demonstrate the issue

## Scope

The following are in scope:

- The zeep-vane Go binary and its HTTP endpoints (admin API and public status page)
- The embedded admin dashboard UI
- Authentication and authorization mechanisms (session cookies, role enforcement, rate limiting)
- Encryption of stored Datadog credentials at rest
- Automatic TLS provisioning (CertMagic/ACME) for custom domains

Out of scope: dependencies with known CVEs (reported upstream).

## Known, accepted risk areas

These are documented tradeoffs, not unknown vulnerabilities — see the README's [Configuration](README.md#-configuration) section for detail:

- `VANE_DEV_TOKEN_LOGGING=true` logs raw password-reset/admin-invite tokens (a bearer credential for account takeover) as a stand-in for real email delivery. This is off by default and meant only for a single self-hosted operator bootstrapping their own instance.
- `VANE_SECURE_COOKIES=false` sends the session cookie unencrypted over plain HTTP. Only intended for a trusted local/internal network.
