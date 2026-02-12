# k2 Usage Guide

k2 is a minimalist VPN tunnel with the k2v5 composite wire protocol. Two binaries: **k2s** (server) and **k2** (client).

## Architecture

```
┌──────── Client (k2) ────────┐           ┌──────── Server (k2s) ────────┐
│                              │           │                               │
│  App traffic                 │           │  :443 (QUIC + TCP shared)     │
│    ↓                         │           │    ↓                          │
│  TUN device / SOCKS5 proxy   │           │  TLS ClientHello peek        │
│    ↓                         │           │    ├─ ECH → k2v5 handler      │
│  gVisor L4 stack             │           │    └─ No ECH → reverse proxy  │
│    ↓                         │           │                               │
│  Router (direct/proxy/reject)│           │  k2v5 handler:               │
│    ↓                         │  ──────→  │    Auth → TicketStore         │
│  Wire (QUIC primary,        │  k2v5://  │    QUIC server                │
│         TCP-WS fallback)     │           │    TCP-WS server              │
│                              │           │    ↓                          │
└──────────────────────────────┘           │  ProxyHandler                 │
                                           │    ├─ TCP: net.Dial → pipe    │
                                           │    └─ UDP: net.Dial → forward │
                                           └───────────────────────────────┘
```

## 1. Build

```bash
# Requires Go 1.25+
go build -o k2  ./cmd/k2    # Client binary
go build -o k2s ./cmd/k2s   # Server binary
```

## 2. Server Setup (k2s)

### 2.1 Generate TLS Certificate

k2s requires a TLS certificate. For testing, use a self-signed cert:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout server-key.pem -out server-cert.pem \
  -days 3650 -nodes -subj "/CN=k2s"
```

### 2.2 Server Configuration

Create `server.yaml`:

```yaml
listen: ":443"

tls:
  cert: "/path/to/server-cert.pem"
  key: "/path/to/server-key.pem"

# ECH (Encrypted Client Hello) — required for k2v5 protocol
ech:
  key_path: "/path/to/ech.key"         # Auto-generated on first run if missing
  public_name: "cloudflare-ech.com"    # Outer SNI (must be ECH-enabled domain)

log:
  level: debug
  output: stderr
```

On first startup, if `ech.key` does not exist, k2s auto-generates the ECH key pair and saves it.

### 2.3 Start Server

```bash
k2s run -s server.yaml
```

Output:
```
k2s server starting on :443
k2s server ready (TCP+QUIC on :443)
```

### 2.4 Extract ECH Config for Client

The client needs the base64-encoded ECH config from the server's ECH key file. A helper script:

```bash
# Read the ECH key file and extract the config list, then base64-encode it.
# The file format: [privkey_len:4][privkey][config_len:4][config]
python3 -c "
import struct, base64, sys
data = open('ech.key', 'rb').read()
pk_len = struct.unpack('>I', data[0:4])[0]
cfg_off = 4 + pk_len
cfg_len = struct.unpack('>I', data[cfg_off:cfg_off+4])[0]
cfg = data[cfg_off+4:cfg_off+4+cfg_len]
print(base64.b64encode(cfg).decode())
"
```

This outputs a base64 string like `AEX05...` which goes into the client's wire URL as `&ech=AEX05...`.

## 3. Client Setup (k2)

### 3.1 Wire URL Format

The k2v5 wire URL encodes all connection parameters:

```
k2v5://UDID:TOKEN@HOST:PORT?ech=ECH_CONFIG&pin=CERT_PIN&fp=FINGERPRINT
```

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| UDID | Yes | — | Device identifier (any non-empty string) |
| TOKEN | Yes | — | Auth token (any non-empty string for dev server) |
| HOST | Yes | — | Server IP or hostname |
| PORT | Yes | — | Server port |
| ech | Yes | — | URL-safe base64-encoded ECH config from server (contains public_name for outer SNI) |
| pin | Yes | — | `sha256:<rsa_b64>,sha256:<ec_b64>` — server public key pins |
| fp | No | chrome | TLS fingerprint: chrome, firefox, safari, random |
| hop | No | disabled | UDP port hopping range: START-END |
| insecure | No | false | Skip TLS cert verification (for self-signed certs) |

### 3.2 Client Configuration

Create `client.yaml`:

```yaml
server: "k2v5://my-device:my-token@SERVER_IP:443?ech=BASE64_ECH_CONFIG"

# "proxy" mode for Docker/headless (no root needed)
# "tun" mode for desktop VPN (needs root/sudo)
mode: proxy

proxy:
  listen: "0.0.0.0:1080"     # SOCKS5 + HTTP proxy

dns:
  direct:
    - "8.8.8.8:53"
  proxy:
    - "8.8.8.8:53"

rule:
  global: true                # Send all traffic through tunnel

log:
  level: debug
  output: stderr
```

### 3.3 Start Client

**Proxy mode** (no root required):
```bash
k2 run -c client.yaml
```

**TUN mode** (requires root):
```bash
sudo k2 run -c client.yaml
```

### 3.4 Verify Connection

In proxy mode, test through the SOCKS5 proxy:

```bash
curl -x socks5h://127.0.0.1:1080 http://httpbin.org/ip
```

## 4. Docker Deployment

See `docker/` directory for ready-to-use Docker Compose setup.

### Quick Start

```bash
cd docker/
docker compose up --build
```

This starts:
- **k2s** container: server listening on port 443
- **k2** container: client in proxy mode, SOCKS5 on port 1080

### Test

```bash
# From host machine — traffic goes: curl → k2 proxy → k2s server → internet
curl -x socks5h://127.0.0.1:1080 http://httpbin.org/ip
```

## 5. Ports Reference

| Port | Service | Description |
|------|---------|-------------|
| 443 | k2v5 wire | Server (QUIC + TCP-WS shared) |
| 1777 | Daemon API | Client localhost HTTP API |
| 1080 | SOCKS5/HTTP | Client proxy mode |

## 6. Authentication

The dev server accepts any non-empty UDID + token combination. In production, configure `auth.remote_url` in server.yaml to point to your auth API.

Auth flow:
1. Client sends `Authorization: Bearer <token>` + `X-K2-Device: <udid>` → `POST /k2v5/auth`
2. Server validates credentials, issues auth ticket (1h TTL)
3. Subsequent connections use ticket for faster re-auth
4. Auto-renewal at 50% TTL remaining

## 7. Troubleshooting

**"wire: ECH config required"** — Client wire URL is missing `ech=` parameter. Extract ECH config from server and add to URL.

**"TLS config required"** — Server config is missing TLS cert/key paths.

**"TLS handshake error"** — ECH config mismatch between client and server. Regenerate ECH key and update client config.

**Connection timeout** — Check firewall rules. Server needs TCP+UDP port 443 open. QUIC uses UDP.
