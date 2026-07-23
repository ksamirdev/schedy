---
id: configuration
title: Configuration
sidebar_position: 3
---

# Configuration

The port is set with the `--port` flag (default `8080`). Behavior is tuned with
environment variables:

| Variable             | Default | Description                                                                        |
|----------------------|---------|------------------------------------------------------------------------------------|
| `SCHEDY_API_KEY`     | _unset_ | If set, all endpoints require the `X-API-Key` header.                              |
| `SCHEDY_HISTORY_TTL` | `72h`   | How long terminal tasks are retained before purge (Go duration, e.g. `24h`, `168h`). |
| `SCHEDY_ALLOW_PRIVATE_TARGETS` | _unset_ | If set, allow task URLs that resolve to private/loopback/link-local addresses. Off by default: such targets are rejected at dial time to prevent SSRF into the host's network. See [Delivery](./concepts/delivery.md#blocked-targets). |
| `SCHEDY_ON_FAILURE_URL` | _unset_ | If set, a task that exhausts its retries POSTs `{id, status, attempts, last_error, status_code}` here once, best-effort. See [Retries](./concepts/retries.md#failure-callback). |

Example:

```bash
SCHEDY_API_KEY=your-secret SCHEDY_HISTORY_TTL=168h ./schedy --port 8080
```
