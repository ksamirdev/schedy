---
id: bulk-delete
title: Bulk delete
sidebar_position: 5
---

# Bulk delete

```
DELETE /tasks
```

Hard-deletes tasks matching a filter (across all statuses). At least one filter
is required.

| Query param | Description                                            |
|-------------|--------------------------------------------------------|
| `url`       | Delete tasks targeting this exact URL.                 |
| `before`    | Delete tasks scheduled before this time (RFC3339).     |
| `after`     | Delete tasks scheduled after this time (RFC3339).      |

```bash
# Everything for one URL
curl -X DELETE "http://localhost:8080/tasks?url=https://example.com/webhook" \
  -H "X-API-Key: your-secret"

# Combine filters
curl -X DELETE "http://localhost:8080/tasks?url=https://example.com/webhook&before=2025-05-26T15:00:00Z" \
  -H "X-API-Key: your-secret"
```

Returns `{"deleted": N}`, or `400 Bad Request` if no filter is given or a
timestamp is malformed.
