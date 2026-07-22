---
id: list
title: List tasks
sidebar_position: 2
---

# List tasks

```
GET /tasks
```

Returns a JSON array of tasks. The optional `status` query parameter filters by
[lifecycle state](../concepts/status.md).

```bash
# All tasks
curl http://localhost:8080/tasks -H "X-API-Key: your-secret"

# Only failures
curl "http://localhost:8080/tasks?status=failed" -H "X-API-Key: your-secret"
```

Valid `status` values: `pending`, `running`, `succeeded`, `failed`, `cancelled`.
