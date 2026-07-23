# Schedy

Schedule an HTTP request for later.

Tell Schedy a URL and a time; it fires the request when the time comes, retries if it fails, and remembers what happened.
It's one Go binary with an embedded database - no Redis, no Postgres, no cron daemon to babysit.
Point it at a directory and run it.

```sh
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:latest
```

```sh
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/webhook",
    "execute_at": "2030-01-01T09:00:00Z",
    "payload": {"hello": "world"}
  }'
```

You get back a task id.
At `execute_at`, Schedy POSTs your payload to the URL, retries on failure, and keeps the outcome so you can look it up later.

## What it does

- Fires an HTTP request (any method) at a scheduled time, with your headers and body.
- Retries failures on a fixed or exponential-backoff schedule.
- Tracks each task's status and logs every delivery attempt.
- Repeats on an interval if you want it to - `"schedule": "15m"`.

Also there when you need it: HMAC request signing, idempotency keys, online backup/restore, and an SSRF egress guard.
Full reference lives at **[schedy.mintlify.site](https://schedy.mintlify.site)**.
The whole HTTP API is also described by a machine-readable [OpenAPI spec](openapi.yaml) - point your codegen, Postman, or Insomnia at it instead of hand-writing a client.

## What it deliberately isn't

Schedy is not cron and not a workflow engine.
There is no cron syntax, no timezones or DST, no DAGs, no fan-out.
If you need a calendar or Temporal-grade orchestration, reach for one of those - Schedy stays a "fire this HTTP request later" box on purpose.
That constraint is the feature.

## Running it

```sh
docker run -p 8080:8080 -e SCHEDY_API_KEY=your-secret ghcr.io/ksamirdev/schedy:latest
```

Set `SCHEDY_API_KEY` and every endpoint requires the `X-API-Key` header.
Images are also on Docker Hub (`ksamirdev/schedy`), and prebuilt binaries are on the [Releases](https://github.com/ksamirdev/schedy/releases) page.

From source (Go 1.23+):

```sh
go build -o schedy ./cmd/schedy
./schedy --port 8080
```

Tasks persist to `data/` (BadgerDB) and survive restarts.
Snapshot them with `GET /admin/backup` instead of copying the live directory - see the [backup docs](https://schedy.mintlify.site/backup).

## Contributing

Issues and PRs are welcome, odd use-cases especially.
See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md).
If Schedy saved you a cron box or a queue, [sponsoring](https://github.com/sponsors/ksamirdev) helps keep it maintained.

## License

MIT. See [LICENSE](LICENSE).
