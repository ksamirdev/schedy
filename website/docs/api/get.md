---
id: get
title: Get a task
sidebar_position: 3
---

# Get a task

```
GET /tasks/{id}
```

Returns a single task including its `status`, `attempts`, and `finished_at`.
Responds `404 Not Found` if the id is unknown.

```bash
curl http://localhost:8080/tasks/b1e2c3... -H "X-API-Key: your-secret"
```
