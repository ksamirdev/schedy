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
