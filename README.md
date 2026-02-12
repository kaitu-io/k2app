# k2

A stealth network tunnel. QUIC+H3 primary, TCP-WebSocket fallback, Encrypted Client Hello (ECH), TLS fingerprint mimicry.

## Server (on your VPS)

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
sudo k2s run
```

First run auto-generates certs + ECH keys, installs as a system service, and prints a connect URL:

```
k2s is running. Share this URL with clients:

  k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

Run again anytime to see the URL. Service survives reboots.

## Client (on your laptop)

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
sudo k2 up k2v5://...   # paste URL from server
```

All traffic is now tunneled. Daily commands:

```bash
k2 up        # reconnect (URL saved)
k2 down      # disconnect
k2 status    # check connection
```

### Proxy mode (no root)

```bash
k2 up --mode proxy k2v5://...
```

Uses SOCKS5+HTTP proxy on `127.0.0.1:1080` instead of TUN device.

## How it works

```
Client (k2)                          Server (k2s)
─────────────                        ─────────────
App traffic                          :443 (QUIC + TCP shared)
  ↓                                    ↓
TUN / SOCKS5 proxy                   TLS peek
  ↓                                    ├─ ECH → k2v5 handler
gVisor L4 stack                        └─ No ECH → looks like normal HTTPS
  ↓                                    ↓
Router (direct/proxy/reject)         Auth → TCP proxy + UDP forward
  ↓                         k2v5://    ↓
Wire (QUIC primary,        ───────→  Internet
      TCP-WS fallback)
```

**Stealth features:**
- ECH hides the real SNI inside encryption — outer SNI looks like `cloudflare-ech.com`
- TLS fingerprint mimics Chrome via uTLS
- TLS record sizes match real Cloudflare servers (padding)
- Dual RSA+EC certificates for algorithm camouflage
- Fallback to TCP-WebSocket when QUIC is blocked

## Build from source

```bash
# Requires Go 1.25+
go build -o k2  ./cmd/k2
go build -o k2s ./cmd/k2s
```

## Docker

```bash
cd docker/
docker compose up --build
```

Starts k2s (server, port 443) and k2 (client, proxy on port 1080).

```bash
curl -x socks5h://127.0.0.1:1080 https://httpbin.org/ip
```

## Documentation

- [Usage Guide](docs/guide.md) — detailed setup, config options, troubleshooting
- [Protocol Spec](wire/k2v5-protocol-spec.md) — k2v5 wire protocol specification

## License

MIT
