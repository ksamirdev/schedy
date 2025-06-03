### 🚀 Try Schedify: [schedify.dev](https://schedify.dev)

No auth required. No infrastructure to manage. Just simple, reliable task scheduling for developers.

- 📖 [Read our Dead Simple Integration Guide](https://schedify.dev/5-min-guide)
- 🆓 [Start Scheduling - It's free](https://schedify.dev/schedules)

---

# Schedy

> **A self-hostable, ultra-lightweight HTTP task scheduler for the weird and wonderful automation you want.**

Schedy lets you schedule HTTP POST requests to any endpoint at any time, with custom headers and payloads. Perfect for webhooks, bots, reminders, integrations, and all sorts of automation—without the bloat.

## 🐳 Try Schedy in 1 Minute

You can run Schedy instantly using Docker from either GitHub Container Registry or Docker Hub:

**From GitHub Container Registry:**

```sh
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:latest
```

**From Docker Hub:**

```sh
docker run -p 8080:8080 ksamirdev/schedy:latest
```

You can also use a specific version tag (e.g., `v0.0.1`):

```sh
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:v0.0.1
# or
docker run -p 8080:8080 ksamirdev/schedy:v0.0.1
```

Set an API key for security (optional but recommended):

```sh
docker run -p 8080:8080 -e SCHEDY_API_KEY=your-secret ghcr.io/ksamirdev/schedy:latest
```

---

## Features

- 🕒 **Schedule HTTP tasks** for any time in the future
- 🪶 **Ultra-lightweight**: single binary, no external dependencies except BadgerDB
- 🏠 **Self-hostable**: run anywhere Go runs (Linux, macOS, Windows, ARM, x86)
- 🔒 **Custom headers**: add auth, content-type, or anything else
- 🧬 **Flexible payloads**: send JSON, form data, or plain text
- 🦄 **Weirdly simple**: no UI, no cron, just HTTP

---

## Quick Start

### 1. Download the Binary

Head to [Releases](https://github.com/ksamirdev/schedy/releases) and grab the latest `schedy` binary for your OS/architecture. No build required!

### 2. Run

```bash
SCHEDY_API_KEY=your-secret ./schedy --port 8081
```

Schedy will listen on the port you specify with `--port` (default: `8080`).
If you set the `SCHEDY_API_KEY` environment variable, all API endpoints require the `X-API-Key` header.

---

## API

### Schedule a Task

Send a POST to `/tasks` (requires `X-API-Key` header if enabled):

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://webhook.site/your-endpoint",
    "headers": {"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Bearer TOKEN"},
    "payload": "foo=bar&baz=qux"
  }'
```

#### Request Fields

- `execute_at`: When to run (RFC3339, UTC)
- `url`: Where to POST
- `headers`: (optional) Map of HTTP headers
- `payload`: (optional) Anything: JSON, string, bytes, form data, etc.

#### Examples

**JSON payload (default):**

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"hello": "world"}
  }'
```

**Form data:**

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/form",
    "headers": {"Content-Type": "application/x-www-form-urlencoded"},
    "payload": "foo=bar&baz=qux"
  }'
```

**Plain text:**

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/text",
    "headers": {"Content-Type": "text/plain"},
    "payload": "hello world!"
  }'
```

### List Scheduled Tasks

Send a GET to `/tasks/list` (requires `X-API-Key` header if enabled):

```bash
curl -X GET http://localhost:8080/tasks \
  -H "X-API-Key: your-secret"
```

Returns a JSON array of all scheduled tasks.

---

## Why Schedy?

- No cron, no YAML, no UI, no cloud lock-in
- Just HTTP, just works
- For hackers, tinkerers, and anyone who wants to automate the weird stuff

---

## Contributing

PRs, issues, and weird use-cases welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
