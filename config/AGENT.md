# Config Layer (config/)

Single file: `config.go` — separate `ClientConfig` and `ServerConfig` types.

## Client (client.yaml)

```yaml
server: "k2v5://udid:token@host:443?ech=...&pin=sha256:..."
mode: tun          # "tun" (default, needs root) or "proxy"
proxy:
  listen: "127.0.0.1:1080"
dns:
  direct: ["114.114.114.114:53", "223.5.5.5:53"]
  proxy: ["8.8.8.8:53", "1.1.1.1:53"]
rule:
  global: false    # true = global proxy (ignore rules)
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
  cache_ttl: 5m
log:
  level: info
```

## Types

- `ClientConfig` — Server URL, Mode, Proxy, DNS, Rule, Log
- `ServerConfig` — Listen, Host, TLS, ECH, CertDir, PCC, Rate, Auth, Log
- `LoadClient(path)` / `LoadServer(path)` — YAML loaders
- `ClientFromURL(wireURL)` — creates ClientConfig from k2v5:// URL
- `DefaultServerConfig()` — fully-defaulted config for zero-config mode
- Uses `gopkg.in/yaml.v3`
