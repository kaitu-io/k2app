# Config Layer (config/)

Single file: `config.go` — separate `ClientConfig` and `ServerConfig` types.

## Client (client.yaml)

```yaml
server: "k2v5://udid:token@host:443?ech=AEX05...&pin=sha256:..."
dns:
  direct: ["114.114.114.114:53", "223.5.5.5:53"]
  proxy: ["8.8.8.8:53", "1.1.1.1:53"]
router:
  default: proxy    # "direct", "proxy", or "reject"
log:
  level: info       # debug, info, warn, error
```

## Server (server.yaml)

```yaml
listen: ":443"
tls:
  cert: /path/to/cert.pem
  key: /path/to/key.pem
pcc: gfw
rate: 100000000
auth:
  remote_url: "https://auth.example.com/check"
  cache_ttl: 5m
log:
  level: info
```

## Types

- `ClientConfig` — Server URL, DNS, Router, Log
- `ServerConfig` — Listen, TLS, PCC, Rate, Auth, Log
- `LoadClient(path)` / `LoadServer(path)` — YAML loaders
- Uses `gopkg.in/yaml.v3`
