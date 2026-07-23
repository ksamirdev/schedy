---
id: cancel
title: Cancel a task
sidebar_position: 5
---

# Cancel a task

```
DELETE /tasks/{id}
```

Cancels a task. A non-terminal task is **soft-cancelled**: marked `cancelled` and
kept in history (it expires via TTL), so the record survives for auditing.
Already-terminal tasks are a no-op.

```bash
curl -X DELETE http://localhost:8080/tasks/b1e2c3... -H "X-API-Key: your-secret"
```

## Responses

| Response         | Meaning                          |
|------------------|----------------------------------|
| `204 No Content` | Cancelled (or already terminal). |
| `404 Not Found`  | Task doesn't exist.              |
