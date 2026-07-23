---
id: status
title: Status & History
sidebar_position: 1
---

# Task Status & History

Every task carries a lifecycle `status` and a per-attempt log, so you can always
answer "did it run, how many tries, and why did it fail?".

| Status      | Meaning                                            | Terminal |
|-------------|----------------------------------------------------|----------|
| `pending`   | Accepted, no attempt started yet.                  | No       |
| `running`   | At least one attempt fired, not yet terminal.      | No       |
| `succeeded` | An attempt got a 2xx response.                     | Yes      |
| `failed`    | Retries exhausted; last attempt non-2xx or error.  | Yes      |
| `cancelled` | Cancelled via `DELETE /tasks/{id}` before running. | Yes      |

`pending` is the only mutable state - see [Update a task](../api/update.md).
Note that it does not mean "never fired": a task interrupted mid-delivery by a
crash is re-queued as `pending` with its earlier attempts still logged, which is
why an update never clears the attempt history.

Terminal tasks are retained for history and auto-purged after
`SCHEDY_HISTORY_TTL`. A completed task carries the full attempt log:

```json
{
  "id": "b1e2c3...",
  "url": "https://example.com/webhook",
  "execute_at": "2025-05-26T15:00:00Z",
  "status": "failed",
  "finished_at": "2025-05-26T15:00:06Z",
  "attempts": [
    { "n": 1, "fired_at": "2025-05-26T15:00:00Z", "status_code": 500, "error": "unexpected status code: 500", "duration_ms": 42 },
    { "n": 2, "fired_at": "2025-05-26T15:00:02Z", "status_code": 0,   "error": "dial tcp: connection refused",   "duration_ms": 5 }
  ]
}
```
