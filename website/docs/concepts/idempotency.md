---
id: idempotency
title: Idempotency
sidebar_position: 4
---

# Idempotency

To avoid accidental duplicate schedules, a `POST /tasks` that matches an existing
**pending** task by `url` + `execute_at` (within 1 second) returns that task with
`200 OK` instead of creating a new one.

Send an `Idempotency-Key` header for explicit deduplication:

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{
    "execute_at": "2025-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"event": "user.created"}
  }'
```
