# Security Policy

## Supported versions

Schedy is pre-1.0 and ships from `main`.
Security fixes land on the latest release only; please reproduce on the newest
release (or `main`) before reporting.

## Reporting a vulnerability

Please **do not open a public issue** for security problems.

Report privately through GitHub:

1. Go to the [Security tab](https://github.com/ksamirdev/schedy/security/advisories) of the repository.
2. Click **Report a vulnerability** to open a private advisory.

Include what you'd put in any good bug report: affected version, a minimal
reproduction, the impact, and any relevant configuration (env vars, task
payloads, deployment shape).

This is a small, best-effort project. We aim to acknowledge a report within a
week and will keep you updated as we investigate. Once a fix is out, we're happy
to credit you in the advisory unless you'd rather stay anonymous.

## Scope

Schedy delivers HTTP requests on your behalf and persists them to a local
database, so the areas most worth scrutiny are:

- **Egress / SSRF** - the dial-time guard that blocks task URLs resolving to
  private, loopback, link-local, and cloud-metadata addresses (and the
  `SCHEDY_ALLOW_PRIVATE_TARGETS` opt-out).
- **Authentication** - the `SCHEDY_API_KEY` check on the HTTP API.
- **Request signing** - the `SCHEDY_SIGNING_SECRET` HMAC applied to outgoing
  deliveries.
- **Admin surface** - the backup/restore path.

Reports about configurations that deliberately disable a protection (for
example, running with `SCHEDY_ALLOW_PRIVATE_TARGETS` set on an untrusted
network, or with no API key) are expected behavior, not vulnerabilities.
