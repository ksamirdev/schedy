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
- `retries`: (optional) Number of retries.
- `retry_interval`: (optional) Wait time between retries in milliseconds (default: 2000).

**Idempotency:**
To prevent duplicate task creation, Schedy automatically detects when you try to schedule a task with the same `url` and `execute_at` (within 1 second). If a matching task exists:
- Returns `200 OK` (instead of `201 Created`)
- Returns the existing task instead of creating a duplicate
- You can also send an optional `Idempotency-Key` header for stricter deduplication

```bash
# This won't create a duplicate if the same task already exists
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"event": "user.created"}
  }'
```

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

Send a GET to `/tasks` (requires `X-API-Key` header if enabled):

```bash
curl -X GET http://localhost:8080/tasks \
  -H "X-API-Key: your-secret"
```

Returns a JSON array of all scheduled tasks.

### Get a Single Task

Send a GET to `/tasks/{id}` (requires `X-API-Key` header if enabled):

```bash
curl -X GET http://localhost:8080/tasks/{task-id} \
  -H "X-API-Key: your-secret"
```

**Responses:**
- `200 OK`: Returns the task details
- `404 Not Found`: Task doesn't exist

### Delete a Single Task

Send a DELETE to `/tasks/{id}` (requires `X-API-Key` header if enabled):

```bash
curl -X DELETE http://localhost:8080/tasks/{task-id} \
  -H "X-API-Key: your-secret"
```

**Responses:**
- `204 No Content`: Task successfully deleted
- `404 Not Found`: Task doesn't exist
- `401 Unauthorized`: Missing API key
- `403 Forbidden`: Invalid API key

### Bulk Delete Tasks

Send a DELETE to `/tasks` with query parameters (requires `X-API-Key` header if enabled):

```bash
# Delete all tasks for a specific URL
curl -X DELETE "http://localhost:8080/tasks?url=https://example.com/webhook" \
  -H "X-API-Key: your-secret"

# Delete tasks scheduled before a specific time
curl -X DELETE "http://localhost:8080/tasks?before=2025-05-26T15:00:00Z" \
  -H "X-API-Key: your-secret"

# Delete tasks scheduled after a specific time
curl -X DELETE "http://localhost:8080/tasks?after=2025-05-26T15:00:00Z" \
  -H "X-API-Key: your-secret"

# Combine filters
curl -X DELETE "http://localhost:8080/tasks?url=https://example.com/webhook&before=2025-05-26T15:00:00Z" \
  -H "X-API-Key: your-secret"
```

**Query Parameters:**
- `url`: (optional) Delete tasks targeting this exact URL
- `before`: (optional) Delete tasks scheduled before this time (RFC3339 format)
- `after`: (optional) Delete tasks scheduled after this time (RFC3339 format)

**Responses:**
- `200 OK`: Returns `{"deleted": N}` with count of deleted tasks
- `400 Bad Request`: No filters provided or invalid time format
- `401 Unauthorized`: Missing API key
- `403 Forbidden`: Invalid API key

---

## Persistence & Behavior

### Storage
Schedy uses **BadgerDB** for persistent storage. All tasks are saved to disk in the `data/` directory (configurable). This means:
- ✅ Tasks survive server restarts
- ✅ No data loss on crash
- ✅ No external database required
- ⚠️ Backup the `data/` directory to preserve scheduled tasks

### Task Execution
- Tasks are checked every 10 seconds by default
- Once a task's `execute_at` time passes, it's executed immediately
- Failed tasks are retried based on the `retries` configuration
- Successfully executed tasks are deleted from storage
- Failed tasks (after all retries) remain in storage for inspection

### Rate Limits
No built-in rate limits. You can use a reverse proxy (nginx, Caddy) or API gateway to enforce rate limiting if needed.

### Authentication
When `SCHEDY_API_KEY` is set:
- All endpoints require the `X-API-Key` header
- Missing key returns `401 Unauthorized`
- Invalid key returns `403 Forbidden`

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
