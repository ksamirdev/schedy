---
id: idempotency
title: Idempotency
sidebar_position: 4
---

# Idempotency

A `POST /tasks` that Schedy recognises as a repeat returns the existing task with
`200 OK` instead of scheduling a second one.
There are two ways it recognises a repeat, and the `Idempotency-Key` header
decides which.

Both only ever match against **pending** tasks.
A task that has already run is history rather than a live schedule, and history
expires under [`SCHEDY_HISTORY_TTL`](../configuration.md) - matching against it
would make deduplication quietly depend on your retention window.

## With an `Idempotency-Key`

Send the header and the key alone decides.
It is your name for the task, so a retried request returns the task the first
one created regardless of what the new body says.

```bash
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-key-123" \
  -d '{
    "execute_at": "2030-05-26T15:00:00Z",
    "url": "https://example.com/webhook",
    "payload": {"event": "user.created"}
  }'
```

Repeat that call with the same key and you get the original task back with
`200 OK`, even if you changed the url or the time.
Use a fresh key when you mean a genuinely new schedule.

The key is recorded on the task as `idempotency_key` and never changes.
[Updating a task](../api/update.md) leaves it alone.

## Without an `Idempotency-Key`

With no key, an identical schedule counts as a repeat: the same `url`, at an
`execute_at` less than a second away from an existing pending task.

This is a safety net against accidental double-submits, not a substitute for a
key.
Two deliberately-distinct tasks pointing at the same url within the same second
will collapse into one, so send a key when that matters.
