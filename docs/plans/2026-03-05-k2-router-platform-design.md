# k2 Router Platform Design

Date: 2026-03-05

## Overview

Extend k2 to be a first-class router/soft-router VPN engine. Four workstreams:

1. **Rule engine** — Replace k2rule with sing-box compatible rule engine
2. **k2subs:// subscription** — Multi-tunnel management with budget_score selection
3. **DNS enhancement** — DoH/DoT, persistent cache, hosts, independent cache
4. **Build trimming** — `no_tun` tag for router builds, OpenRC support

## Design Principles

- **Don't reinvent**: sing-box rule format 100% compatible, reuse .srs rule-set ecosystem
- **Config-driven**: single config source, no operational API complexity
- **Engine unchanged**: engine manages single tunnel lifecycle; multi-tunnel orchestration lives in daemon
- **Minimal memory**: bbolt for persistence (disk-backed), no in-memory bloat

---

## 1. Rule Engine

### Current State

`k2rule` v0.6.4 — simple `Init() + Match(host) -> Target` API. Only domain/IP matching. Three targets: direct/proxy/reject.

### Target State

New `k2/rule/` package implementing sing-box compatible rule engine.

### Package Structure

```
k2/rule/
  engine.go        RuleEngine struct, Match(metadata) -> Action
  rule.go          Rule interface, DefaultRule, LogicalRule
  rule_item.go     Condition matchers (domain, ip_cidr, geoip, port, etc.)
  rule_set.go      RuleSet loading (inline/local/remote, .srs binary)
  action.go        Action types
  metadata.go      Connection metadata struct
  config.go        sing-box compatible JSON/YAML deserialization
```

### Metadata (per-connection, populated by Provider/Sniffer)

```go
type Metadata struct {
    Network     string     // "tcp" / "udp"
    Protocol    string     // "tls" / "http" / "quic" / "bittorrent" / ""
    Domain      string     // SNI / Host (from sniffer)
    DstIP       netip.Addr
    SrcIP       netip.Addr
    DstPort     uint16
    SrcPort     uint16
    WifiSSID    string     // platform callback
    WifiBSSID   string
    IPIsPrivate bool       // precomputed
}
```

### Match Flow

```
Provider receives connection -> populate Metadata (IP/port/network)
  -> Sniffer peek (optional) -> populate Protocol/Domain
  -> RuleEngine.Match(metadata) -> iterate rules, first-match-wins
  -> return Action{Outbound: "tag"}
  -> no match -> Final outbound
```

### sing-box Compatibility

| Aspect | Compatibility |
|--------|--------------|
| JSON config format (`route.rules`) | 100% field-name compatible |
| .srs rule-set files | Direct use (sing-box library for parsing) |
| geoip.db / geosite.db | Same data sources |
| Rule conditions | Full support (see below) |
| Logical rules (AND/OR) | Supported |
| Rule actions | route/direct/reject/hijack-dns/sniff |

### Supported Rule Conditions

**P0:**
- `domain` / `domain_suffix` / `domain_keyword` / `domain_regex`
- `ip_cidr` / `source_ip_cidr`
- `geoip` / `geosite`
- `port` / `port_range`
- `ip_is_private`
- `network` (tcp/udp)
- `protocol` (tls/http/quic/bittorrent — from sniffer)
- `invert`

**P1:**
- `wifi_ssid` / `wifi_bssid`
- `source_port` / `source_port_range`
- `rule_set` / `rule_set_ip_cidr_match_source`

### Dependencies

- `sing-geoip` — mmdb queries (replaces k2rule's geoip)
- `sing-geosite` — domain classification (replaces k2rule's rule_url)
- `sing-rule-set` — .srs binary format parsing
- Remove: `k2rule` v0.6.4

### Integration Points

- `engine.go` step 1: `k2rule.Init()` -> `rule.NewEngine(cfg.Route)`
- `core/router.go`: `k2rule.Match(host)` -> `engine.Match(metadata)`
- `core/tunnel.go`: hardcoded SniffBT/SniffSNI -> driven by sniff rule action
- `sniff/` package unchanged, only trigger mechanism changes

---

## 2. Config Format

### outbounds (tunnel definitions)

```yaml
outbounds:
  # k2v5 — single tunnel (type inferred from URL scheme)
  - tag: "jp"
    server: "k2v5://udid:token@jp:443?ech=..."

  # k2subs — subscription (type inferred from URL scheme)
  - tag: "auto-hk"
    server: "k2subs://api.kaitu.io/v1/tunnels?country=hk"

  # built-in types (no server field)
  - tag: "direct"
    type: direct

  - tag: "block"
    type: block
```

- `type` auto-inferred from `server` URL scheme: `k2v5://` -> k2v5, `k2subs://` -> subscription
- `type` only required for server-less outbounds: `direct`, `block`
- `direct` and `block` auto-injected if not declared

### route (sing-box compatible, inline or external file)

```yaml
route:
  config: "/etc/k2/route.yaml"    # k2 extension: external file or URL
  # OR inline (sing-box compatible):
  rules: [...]
  rule_set: [...]
  final: "auto-hk"
```

### route.yaml (standalone route config)

```yaml
rules:
  - geosite: ["cn"]
    outbound: "direct"

  - geoip: ["cn", "private"]
    outbound: "direct"

  - port: [53]
    action: hijack-dns

  - protocol: ["bittorrent"]
    outbound: "direct"

  - domain_suffix: [".jp", ".dmm.com"]
    outbound: "jp"

  - rule_set: ["streaming"]
    outbound: "auto-hk"

rule_set:
  - tag: "streaming"
    type: remote
    url: "https://cdn.example.com/streaming.srs"
    format: binary
    update_interval: "24h"

  - tag: "ads"
    type: local
    path: "/etc/k2/ads.srs"
    format: binary

geoip:
  download_url: ""
geosite:
  download_url: ""

final: "auto-hk"
```

### dns

```yaml
dns:
  # Protocol prefix support: plain UDP (default), tls:// (DoT), https:// (DoH)
  direct: ["tls://1.1.1.1", "114.114.114.114:53"]
  proxy: ["https://1.1.1.1/dns-query", "8.8.8.8:53"]
  hosts_file: "/etc/hosts"           # optional, default: system hosts
  independent_cache: true            # separate cache for direct/proxy DNS
  cache_file: "{cache_dir}/dns.db"   # bbolt persistent cache
```

### Full config.yaml example (router)

```yaml
listen: "0.0.0.0:1777"
mode: tproxy

outbounds:
  - tag: "auto-hk"
    server: "k2subs://api.kaitu.io/v1/tunnels?country=hk"
  - tag: "jp"
    server: "k2v5://udid:token@jp:443?ech=..."
  - tag: "direct"
    type: direct
  - tag: "block"
    type: block

route:
  config: "https://cdn.kaitu.io/rules/default-router.yaml"

dns:
  direct: ["tls://1.1.1.1", "223.5.5.5:53"]
  proxy: ["https://1.1.1.1/dns-query"]
  independent_cache: true

log:
  level: info
```

---

## 3. k2subs:// Subscription Manager

### Location

Daemon layer. Engine unchanged (single k2v5:// URL).

```
daemon/
  subscription.go
  subscription_test.go
```

### URL Format

```
k2subs://api.kaitu.io/v1/tunnels?country=hk&udid=xxx&token=xxx
```

### Center API Response

```json
{
  "tunnels": [
    {"name": "hk-01", "url": "k2v5://...", "budget_score": 85},
    {"name": "hk-02", "url": "k2v5://...", "budget_score": 60}
  ]
}
```

### Data Structures

```go
type Subscription struct {
    mu        sync.RWMutex
    url       string
    nodes     []Node          // sorted by budget_score descending
    current   int             // selected index
    checking  atomic.Bool
    lastFetch time.Time
    cancel    context.CancelFunc
}

type Node struct {
    Name        string
    URL         string
    BudgetScore int
    Healthy     bool
    LastCheck   time.Time
}
```

### Selection Algorithm (URLTest tolerance pattern)

```go
func (s *Subscription) Select() *Node {
    currentScore := s.nodes[s.current].BudgetScore
    best := highestHealthyByScore(s.nodes)

    // tolerance: don't switch if score gap < 10
    if s.nodes[s.current].Healthy &&
       s.nodes[best].BudgetScore - currentScore < 10 {
        return &s.nodes[s.current]
    }

    s.current = best
    return &s.nodes[best]
}
```

### Lifecycle

```
daemon.doUp(k2subs config)
  |
  +-- 1. subscription.Start()
  |     +-- Fetch() -- HTTP GET Center API -> parse tunnel list
  |     +-- Sort by budget_score descending
  |     +-- ProbeAll() -- concurrent TCP probe (5s timeout)
  |     +-- Select() -- highest healthy score
  |
  +-- 2. engine.Start(selected.URL)
  |
  +-- 3. Background loop (30m interval)
        +-- Fetch + Probe + Select
        +-- New selection != current? -> engine.Stop + engine.Start(new)
```

### Failover

```
engine reports error (503/408/502)
  -> daemon.OnStatus receives error
  -> subscription.MarkUnhealthy(current)
  -> subscription.Next() -- next highest healthy score
  -> has candidate? -> engine.Start(next.URL)
  -> no candidate? -> ProbeAll() -> all down? -> wait 30s retry
```

### Hardcoded Parameters

| Parameter | Value | Notes |
|-----------|-------|-------|
| refresh_interval | 30m | Tunnel list refresh |
| score_tolerance | 10 | Don't switch if gap < 10 |
| probe_timeout | 5s | TCP probe timeout |
| failover_cooldown | 5s | Min time between failovers |

### First Version Stub

Fetch returns mock data or extracts single k2v5 from URL. Select returns only node. Next returns error. Real Center API integration comes later.

---

## 4. DNS Enhancement

### Current State

13-step pipeline in `core/dns/`. Upstream: plain UDP only (`DirectDNSClient` + `ProxyDNSClient`).

### Changes

#### 4.1 DoH/DoT Protocol Support

Based on miekg/dns, zero new dependencies.

```
core/dns/
  transport.go        Transport interface
  transport_udp.go    Existing plain UDP (refactored)
  transport_dot.go    DNS-over-TLS (~60 lines)
  transport_doh.go    DNS-over-HTTPS (~80 lines)
```

URL scheme detection:
- `114.114.114.114:53` or `114.114.114.114` -> plain UDP
- `tls://1.1.1.1` -> DoT (port 853)
- `https://1.1.1.1/dns-query` -> DoH (HTTP POST wireformat)

#### 4.2 Persistent Cache (bbolt)

```
core/dns/
  cache.go            Existing in-memory cache (sync.Map)
  cache_store.go      bbolt persistence layer
```

- Startup: load unexpired entries from bbolt -> memory cache
- Runtime: memory cache primary, async batch write to bbolt (every 30s or 100 entries)
- Shutdown: flush memory cache to bbolt
- File: `{cache_dir}/dns.db` (~1MB cap, LRU eviction)
- Memory: same as current (only in-memory cache), bbolt is persistence backend only

#### 4.3 Hosts File Support

- Extend pipeline step 3 (existing hosts lookup)
- Load `/etc/hosts` (Linux/router) or system hosts file
- Configurable: `dns.hosts_file: "/etc/k2/hosts"`
- Hosts match -> return immediately, skip remaining steps

#### 4.4 Independent Cache

- Separate cache namespace for direct vs proxy DNS
- Prevents polluted direct DNS results from contaminating proxy DNS cache
- Implementation: cache key prefixed with transport tag

### DNS routing unchanged

Current logic: route rules determine domain -> direct/proxy outbound -> direct outbound uses direct DNS servers, proxy outbound uses proxy DNS servers. No separate DNS rule system needed.

---

## 5. Build Trimming & Platform Coverage

### New Build Tag: `no_tun`

Router uses TProxy, doesn't need TUN + gVisor network stack.

```go
// provider/tun_desktop.go
//go:build !no_tun && ((darwin && !ios) || (linux && !android) || windows)

// provider/provider.go (no_tun build)
// NewProvider only supports tproxy and proxy mode
// Requesting tun mode -> error "TUN not available in this build"
```

### Binary Size

| Build | Includes | Size |
|-------|----------|------|
| Full (desktop) | TUN + gVisor + TProxy + Proxy | ~20MB |
| Router (`-tags no_tun`) | TProxy + Proxy | ~12MB |

### Linux Init System Coverage

| Distro | Init System | Status |
|--------|------------|--------|
| OpenWrt | procd | Existing |
| Debian/Ubuntu | systemd | Existing |
| Alpine Linux | OpenRC | New |

New: `scripts/openrc/k2` init script.

`k2 service install` auto-detects: procd -> systemd -> openrc -> error.

### CI Build Matrix

Each architecture produces two binaries:

| Arch | `k2` (full) | `k2-router` (no_tun) |
|------|-------------|---------------------|
| linux/amd64 | Y | Y |
| linux/arm64 | Y | Y |
| linux/armv7 | Y | Y |
| linux/mipsle | Y | Y |

---

## 6. Scope Exclusions

| Feature | Decision | Reason |
|---------|----------|--------|
| HTTP/SOCKS proxy inbound | Not needed | TProxy covers all LAN devices transparently |
| FakeIP | Not needed | SetTmpRule (DNS -> IP mapping) sufficient |
| DNS rules (separate from route) | Not needed | direct/proxy DNS split driven by route rules |
| Clash API compatibility | Not needed | Config-driven, not operation-driven |
| RouterOS support | Not needed | Closed platform, can't run third-party binaries |
| Multiple inbound types | Not needed | TProxy only for router |

---

## 7. Migration Path

### k2rule Removal

1. Implement `k2/rule/` package with sing-box compatibility
2. Update `core/router.go` to use new rule engine
3. Update `engine.go` assembly pipeline
4. Update `core/tunnel.go` sniff logic (rule-driven instead of hardcoded)
5. Remove k2rule dependency from go.mod
6. Update all configs (YAML `rule:` section -> `route:` + `outbounds:`)

### Config Migration

Old:
```yaml
server: "k2v5://..."
rule:
  global: false
  rule_url: ""
  geoip_url: ""
```

New:
```yaml
outbounds:
  - tag: "default"
    server: "k2v5://..."
  - tag: "direct"
    type: direct
  - tag: "block"
    type: block

route:
  rules:
    - geoip: ["cn", "private"]
      outbound: "direct"
    - geosite: ["cn"]
      outbound: "direct"
  final: "default"
```

Desktop/mobile apps: webapp assembles new config format from Cloud API (same data, different shape). Engine config struct updated accordingly.

---

## 8. Implementation Order

1. **Rule engine** (k2/rule/) — foundation, everything depends on this
2. **Config format** (outbounds + route) — wire up new config to engine
3. **DNS enhancement** — DoH/DoT + persistent cache + hosts + independent cache
4. **k2subs:// stub** — daemon subscription manager with mock
5. **Build trimming** — no_tun tag + OpenRC + CI matrix
6. **k2subs:// real** — Center API integration (depends on API spec)
