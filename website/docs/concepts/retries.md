---
id: retries
title: Retries
sidebar_position: 2
---

# Retries

Set `retries` and `retry_interval` (milliseconds) on a task. A delivery fails on
any non-2xx response or transport error; Schedy waits the interval and tries
again until the count is exhausted, then marks the task `failed`.

Each try is recorded as an [attempt](./status.md) in the task's log, with its
status code, error, and duration.

```json
{
  "url": "https://example.com/webhook",
  "execute_at": "2025-05-26T15:00:00Z",
  "retries": 3,
  "retry_interval": 5000
}
```
