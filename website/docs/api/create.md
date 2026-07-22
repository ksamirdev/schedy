---
id: create
title: Create a task
sidebar_position: 1
---

# Create a task

```
POST /tasks
```

Schedule an HTTP POST for a future time.

## Request fields

| Field            | Type   | Description                                                        |
|------------------|--------|--------------------------------------------------------------------|
| `execute_at`     | string | **Required.** When to run (RFC3339, UTC). Must be in the future.   |
| `url`            | string | **Required.** Where to POST.                                       |
| `headers`        | object | Optional map of HTTP headers to send.                              |
| `payload`        | any    | Optional body: JSON object, string, or form data.                 |
| `retries`        | int    | Optional number of retries.                                        |
| `retry_interval` | int    | Optional ms between retries (default `2000`).                      |

## Example

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "headers": {"Authorization": "Bearer TOKEN"},
    "payload": {"event": "user.created"},
    "retries": 3,
    "retry_interval": 5000
  }'
```

Payloads are flexible - a JSON object (default `Content-Type: application/json`),
form data (`application/x-www-form-urlencoded`), or plain text; set the
`Content-Type` header to match.

## Responses

| Response         | Meaning                                              |
|------------------|------------------------------------------------------|
| `201 Created`    | Task scheduled; returns the task.                    |
| `200 OK`         | Idempotent match; returns the existing pending task. |
| `400 Bad Request`| Invalid body, bad time format, or time in the past.  |
