# Config Layer (config/)

Single file: `config.go` — separate `ClientConfig` and `ServerConfig` types.

## Client (client.yaml)

```yaml
server: "k2v5://udid:token@host:443?ech=...&pin=sha256:..."
mode: tun          # "tun" (default, needs root) or "proxy"
tun:
  ipv4: "198.18.0.7/15"    # default; appext overrides via init()
  ipv6: "fdfe:dcba:9876::7/64"
proxy:
  listen: "127.0.0.1:1080"
dns:
  direct: ["114.114.114.114:53", "223.5.5.5:53"]
  proxy: ["8.8.8.8:53", "1.1.1.1:53"]
rule:
  global: false    # true = global proxy (ignore rules)
  rule_url: ""     # remote rule file URL ("" = CDN default)
  geoip_url: ""    # remote GeoIP .mmdb URL ("" = CDN default)
  porn_url: ""     # remote porn list URL ("" = CDN default)
  cache_dir: ""    # set by platform shell (daemon/appext)
block_quic: false  # true = ICMP-reject browser QUIC (default: false = allow through DATAGRAM)
log:
  level: info      # debug, info, warn, error
```

## Server (server.yaml)

All fields optional. `k2s run` with no config uses `DefaultServerConfig()`.

```yaml
listen: ":443"                    # default :443
host: ""                          # external hostname/IP for connect URL (auto-detected if empty)
cert_dir: "~/.k2s"               # auto-generated dual keypair (RSA+EC), ECH keys, auth
tls:                              # explicit certs (overrides cert_dir auto-generation)
  cert: /path/to/cert.pem
  key: /path/to/key.pem
ech:
  provider: local                 # "local" = auto-generate from Cloudflare DNS
pcc: gfw
rate: 100000000
auth:
  remote_url: "https://auth.example.com/check"
  cache_ttl: 15m
log:
  level: info
control_plane_routes:            # optional; antiblock Phase 1 relay.
  k2.52j.me: "k2.52j.me:443"   # inner SNI → Center origin (host:port).
                                 # Omit entirely → built-in default = this exact
                                 # single route (Center address is permanent).
```

## Types

- `ClientConfig` — Server, Mode, Tun, Proxy, DNS, Rule, Log, Listen, PIDFile, BlockQUIC
  - Sub-types: `TunConfig`, `ProxyConfig`, `DNSConfig`, `RuleConfig`, `LogConfig`
- `ServerConfig` — Listen, Host, TLS, ECH, CertDir, PCC, Rate, Auth, Log, ControlPlaneRoutes
- `LoadClient(path)` / `LoadServer(path)` — YAML loaders
- `ClientFromURL(wireURL)` — creates ClientConfig from k2v5:// URL
- `DefaultServerConfig()` — fully-defaulted config for zero-config mode
- Uses `gopkg.in/yaml.v3`
