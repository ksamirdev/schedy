---
id: intro
title: Introduction
slug: /
sidebar_position: 1
---

# Schedy

A self-hostable, ultra-lightweight HTTP task scheduler. Schedule an HTTP POST to
any endpoint at any time - with headers, payloads, retries, and full status
tracking.

Schedy is a single Go binary with an embedded [BadgerDB](https://github.com/dgraph-io/badger)
store. No external database, no message queue, no cron files. You talk to it over
a small HTTP API, it fires your requests when they are due, and it keeps a record
of what happened.

## Quick start

```bash
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:latest
```

Schedule a task:

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"hello": "world"}
  }'
```

That's it. From here:

- **[Install & Run](./installation.md)** - Docker, binaries, or build from source.
- **[Status & History](./concepts/status.md)** - how tasks report what happened.
- **[API Reference](./api/create.md)** - every endpoint.
