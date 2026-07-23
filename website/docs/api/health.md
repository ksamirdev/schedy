---
id: health
title: Health checks
sidebar_position: 7
---

# Health checks

Schedy provides two health endpoints for Kubernetes and container orchestration platforms. Both endpoints require **no authentication**.

## Liveness probe

```
GET /healthz
```

Returns `200 OK` if the process is running. Used by Kubernetes `livenessProbe` to detect if the container should be restarted.

```bash
curl http://localhost:8080/healthz
# Returns: 200 OK (empty body)
```

This endpoint always responds successfully as long as the process is running. No dependencies are checked.

## Readiness probe

```
GET /readyz
```

Returns `200 OK` if schedy is ready to accept new tasks. Returns `503 Service Unavailable` if the database is unreachable. Used by Kubernetes `readinessProbe` to determine if the pod should receive traffic.

```bash
curl http://localhost:8080/readyz
# Returns: 200 OK if ready, 503 Service Unavailable if database fails
```

### Kubernetes example

Configure both probes in your pod spec:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

The readiness probe checks if the database (BadgerDB) is accessible. If it fails, the pod is removed from the service load balancer until the database recovers.
