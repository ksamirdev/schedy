---
id: update
title: Update a task
sidebar_position: 4
---

# Update a task

```
PUT /tasks/{id}
```

Replaces a scheduled task, keeping its id.
Use this to reschedule, retarget, or re-arm a task without the caller having to
track a new id the way delete-then-recreate would force.

Only `pending` tasks can be updated.
Anything else - `running`, `succeeded`, `failed`, `cancelled` - is a `409`.
See [Status & History](../concepts/status.md).

## Full replace, not a merge

`PUT` replaces every client-owned field.
Anything you omit is reset to its default, exactly as if you had created the
task with that body - omit `headers` and the old headers are gone, omit
`retries` and it drops to `0`.
Send the complete task every time.

The server-owned fields are never touched: `id`, `idempotency_key`, `status`,
`attempts` and `finished_at` carry over as they were.
The [key](../concepts/idempotency.md) in particular is set once at creation and
is not settable here - it names the task, not what the task does.
That matters because `pending` does not mean "never fired" - a task re-queued
after a crash is pending with attempt history already logged, and updating it
must not erase the evidence that a delivery may already have gone out.

## Request fields

Identical to [create](./create.md).

| Field            | Type   | Description                                                        |
|------------------|--------|--------------------------------------------------------------------|
| `execute_at`     | string | **Required.** When to run (RFC3339, UTC). Must be in the future.   |
| `url`            | string | **Required.** Where to POST.                                       |
| `headers`        | object | Optional map of HTTP headers to send. Omitted = cleared.           |
| `payload`        | any    | Optional body: JSON object, string, or form data. Omitted = cleared.|
| `retries`        | int    | Optional number of retries. Omitted = `0`.                         |
| `retry_interval` | int    | Optional ms between retries. Omitted = `2000`.                     |

Unlike create, `PUT` never deduplicates and ignores `Idempotency-Key` - the id
in the path already says which task you mean.

## Example

```bash
curl -X PUT http://localhost:8080/tasks/b1e2c3... \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2030-05-26T18:00:00Z",
    "url": "https://example.com/webhook",
    "headers": {"Authorization": "Bearer TOKEN"},
    "payload": {"event": "user.created"},
    "retries": 3,
    "retry_interval": 5000
  }'
```

## Responses

| Response         | Meaning                                                 |
|------------------|---------------------------------------------------------|
| `200 OK`         | Updated; returns the full task.                         |
| `400 Bad Request`| Invalid body, missing `url`, bad time format, or a time in the past. |
| `404 Not Found`  | Task doesn't exist.                                     |
| `409 Conflict`   | Task is not `pending`, so it can no longer be changed.  |

## Updating a task that is about to run

The scheduler picks up work slightly ahead of time, so an update can land while
a task is already queued for delivery.
That case is handled rather than raced: the scheduler re-reads the task
immediately before firing, so a `200` response means the update is what will be
delivered.

If you moved `execute_at`, the queued run is dropped and the task fires at its
new time.
If you changed anything else, the delivery goes out with the new url, headers,
payload and retry settings.
