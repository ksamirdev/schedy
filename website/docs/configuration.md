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

Example:

```bash
SCHEDY_API_KEY=your-secret SCHEDY_HISTORY_TTL=168h ./schedy --port 8080
```
