# Schedy

> **A self-hostable, ultra-lightweight HTTP task scheduler for the weird and wonderful automation you want.**

Schedy lets you schedule HTTP POST requests to any endpoint at any time, with custom headers and payloads. Perfect for webhooks, bots, reminders, integrations, and all sorts of automation—without the bloat.

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
./schedy
```

Schedy will listen on `:8080` by default.

---

## API

### Schedule a Task

Send a POST to `/tasks`:

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
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
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/text",
    "headers": {"Content-Type": "text/plain"},
    "payload": "hello world!"
  }'
```

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
