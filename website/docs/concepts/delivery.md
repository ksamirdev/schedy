---
id: delivery
title: Delivery Guarantee
sidebar_position: 3
---

# Delivery Guarantee

Schedy is **at-least-once**.

Attempts are retried, and on restart any task left mid-run is re-queued rather
than dropped. A crash between a receiver's `2xx` and the status write can
therefore redeliver.

:::tip Make receivers idempotent
Send an `Idempotency-Key` header (Schedy forwards it downstream) or dedupe on
your side, so a redelivery is harmless.
:::

Tasks that came due while the server was down are caught up on the next scan
rather than skipped.

## Blocked targets

So Schedy cannot be turned into an SSRF proxy into its host's network, task URLs
that resolve to private, loopback, link-local (including the `169.254.169.254`
cloud-metadata endpoint), or unspecified addresses are rejected at dial time.
The check runs on the resolved IP, so a public DNS name that points at one of
those ranges is blocked too.

Set `SCHEDY_ALLOW_PRIVATE_TARGETS` to allow them on a trusted self-hosted
network.
