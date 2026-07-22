---
id: installation
title: Install & Run
sidebar_position: 2
---

# Install & Run

## Docker

```bash
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:latest
```

Also available on Docker Hub as `ksamirdev/schedy`, and pinned to a version tag:

```bash
docker run -p 8080:8080 ghcr.io/ksamirdev/schedy:v0.1.0
```

## Binary

Grab a prebuilt binary from the [Releases](https://github.com/ksamirdev/schedy/releases)
page, or build from source:

```bash
go build -o schedy ./cmd/schedy
./schedy --port 8080
```

Requires Go 1.23+.

## Kubernetes

Deploy schedy on Kubernetes with health probes:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: schedy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: schedy
  template:
    metadata:
      labels:
        app: schedy
    spec:
      containers:
      - name: schedy
        image: ghcr.io/ksamirdev/schedy:v0.1.0
        ports:
        - containerPort: 8080
        env:
        - name: SCHEDY_API_KEY
          valueFrom:
            secretKeyRef:
              name: schedy-secrets
              key: api-key
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
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: schedy-data
```

For more details on health probes, see [Health checks](./api/health.md).

## Persistence

Data persists to the `data/` directory (BadgerDB), so tasks survive restarts.
Back it up to preserve scheduled work.
