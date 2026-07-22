# Schedy

> **A self-hostable, ultra-lightweight HTTP task scheduler for the weird and wonderful automation you want.**

Schedy schedules HTTP POST requests to any endpoint, at any time, with custom headers and payloads.
Perfect for webhooks, bots, reminders, and integrations - without the bloat.

📖 **Full documentation: [ksamirdev.github.io/schedy](https://ksamirdev.github.io/schedy)**

## Features

- 🕒 **Schedule HTTP tasks** for any time in the future
- 🔁 **Retries** with configurable count and interval
- 📊 **Status & history** - every task tracks its lifecycle and per-attempt log
- 🪶 **Ultra-lightweight** - single binary, embedded BadgerDB, no external services
- 🏠 **Self-hostable** - runs anywhere Go runs (Linux, macOS, Windows, ARM, x86)
- 🦄 **Weirdly simple** - no UI, no cron, just HTTP

## Run in 1 minute

```sh
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:latest
```

Also on Docker Hub (`ksamirdev/schedy`) and as prebuilt binaries on the
[Releases](https://github.com/ksamirdev/schedy/releases) page.

Set an API key to require the `X-API-Key` header on every endpoint:

```sh
docker run -p 8080:8080 -e SCHEDY_API_KEY=your-secret ghcr.io/ksamirdev/schedy:latest
```

## Schedule a task

```sh
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"hello": "world"}
  }'
```

That's the gist. Retries, status tracking, history, filtering, bulk delete, and the
full API reference live in the **[docs](https://ksamirdev.github.io/schedy)**.

## Build from source

```sh
go build -o schedy ./cmd/schedy
SCHEDY_API_KEY=your-secret ./schedy --port 8080
```

Requires Go 1.23+. Tasks persist to the `data/` directory (BadgerDB), so they
survive restarts - back it up to preserve scheduled tasks.

## Contributing

PRs, issues, and weird use-cases welcome. See [CONTRIBUTING.md](CONTRIBUTING.md)
and our [Code of Conduct](CODE_OF_CONDUCT.md).

## License

MIT. See [LICENSE](LICENSE).
