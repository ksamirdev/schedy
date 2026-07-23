---
id: retries
title: Retries
sidebar_position: 2
---

# Retries

Set `retries` and `retry_interval` (milliseconds) on a task. A delivery fails on
any non-2xx response or transport error; Schedy waits and tries again until the
count is exhausted, then marks the task `failed`.

Each try is recorded as an [attempt](./status.md) in the task's log, with its
status code, error, and duration.

```json
{
  "url": "https://example.com/webhook",
  "execute_at": "2030-05-26T15:00:00Z",
  "retries": 3,
  "retry_interval": 5000
}
```

## Retry mode

`retry_mode` selects how the delay between retries is computed. It defaults to
`fixed`.

- `fixed` waits `retry_interval` between every attempt.
- `exponential` waits `min(retry_interval * 2^n, cap)` with full jitter: each
  wait is a random point in `[0, that ceiling]`, so a struggling endpoint gets
  backed off instead of hammered at a constant rate, and retries from many
  clients spread out instead of synchronising. The cap is fixed at 5 minutes.

```json
{
  "url": "https://example.com/webhook",
  "execute_at": "2030-05-26T15:00:00Z",
  "retries": 5,
  "retry_interval": 1000,
  "retry_mode": "exponential"
}
```

Retries happen inline within a single run, so the backoff horizon is bounded by
the process lifetime: if the process restarts mid-backoff, the task is re-queued
from the start rather than resuming its remaining waits.
