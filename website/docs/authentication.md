---
id: authentication
title: Authentication
sidebar_position: 4
---

# Authentication

When `SCHEDY_API_KEY` is set, every request must include the matching key:

```bash
curl http://localhost:8080/tasks -H "X-API-Key: your-secret"
```

| Situation              | Response           |
|------------------------|--------------------|
| Missing `X-API-Key`    | `401 Unauthorized` |
| Wrong key              | `403 Forbidden`    |

If `SCHEDY_API_KEY` is unset, all endpoints are open - fine for local use. Put a
reverse proxy in front for anything exposed to the internet.
