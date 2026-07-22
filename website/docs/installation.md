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

## Persistence

Data persists to the `data/` directory (BadgerDB), so tasks survive restarts.
Back it up to preserve scheduled work.
