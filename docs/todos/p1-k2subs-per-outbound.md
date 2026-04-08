# P1: k2subs as wire.Dialer (per-outbound transport)

## Status: Blocked on engine per-outbound refactor

## Context

k2subs:// subscription URL basics are implemented (fetch, weighted pick, cache, refresh).
Current integration: daemon resolves k2subs → k2v5 before engine.Start.

This breaks for multi-subscription (e.g. Japan route + US route) because **all k2v5
outbounds share one TransportManager**. engine.Start step 6.5:

```go
case strings.HasPrefix(via, "k2v5://"):
    outbounds[target] = core.NewWireOutbound("k2v5", tm, e)  // same tm for all
```

## Target Architecture

k2subs as `wire.Dialer` — each subscription is an independent outbound with its own
transport, similar to how k2p already works:

```go
case strings.HasPrefix(via, "k2subs://"):
    subsDialer := wire.NewSubsDialer(via, cacheDir)
    outbounds[target] = core.NewWireOutbound("k2subs", subsDialer, e)
```

SubsDialer wraps Subscription + TransportManager:
- DialTCP/DialUDP → delegate to current TM
- On transport failure → exclude server, Pick next, race new TM, retry
- Close → stop refresh, close TM

## Prerequisite: Engine per-outbound refactor

Engine internals coupled to single-TM assumption:
- healthMonitor reads one TM's WireStats
- echoLoop probes one QUIC connection
- reRaceLoop replaces one TM's primary
- doNetworkReconnect resets one TM
- DNS transport uses one tcpwsDialer from race result
- client.Server assumes one server URL

Must decouple these before k2subs-as-Dialer can work.

## What exists today (keep)

- `config/subscription.go` — Subscription struct, fetch, pick, cache, refresh
- `config/subscription_test.go` — 24 tests
- `daemon/daemon.go` — resolveSubscriptions (single-subscription resolve-before-start)
- CLI integration: `k2 setup <k2subs://>`, `k2 up <k2subs://>`
