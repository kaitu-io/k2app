# k2 Guide

k2 is a network tunnel. Two parts: **k2s** (server, runs on your VPS) and **k2** (client, runs on your laptop/phone).

## Server Setup (1 minute)

SSH into your VPS:

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
sudo k2s run
```

That's it. On first run it:
- Generates TLS certificates and ECH keys
- Installs itself as a system service (survives reboots)
- Prints a connect URL

```
Installing k2s service...
Service installed.
Waiting for server to start...

k2s is running. Share this URL with clients:

  k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

Copy that URL. You'll paste it on the client.

Run `sudo k2s run` again anytime to see the URL.

## Client Setup (30 seconds)

On your laptop:

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

Done. All traffic is now tunneled.

### Daily use

```bash
k2 up          # reconnect (URL saved from first use)
k2 down        # disconnect
k2 status      # check connection
```

### Proxy mode (no root needed)

If you don't want VPN/TUN mode, use proxy mode instead:

```bash
k2 up --mode proxy k2v5://...
```

Then configure your browser or apps to use `socks5://127.0.0.1:1080`.

Test:
```bash
curl -x socks5h://127.0.0.1:1080 https://httpbin.org/ip
```

## Docker

For running the server in Docker:

```bash
cd docker/
docker compose up --build
```

This starts both k2s (server, port 443) and k2 (client, proxy on port 1080).

## Ports

| Port | What |
|------|------|
| 443 | Server (QUIC + TCP, shared port) |
| 1080 | Client proxy (SOCKS5 + HTTP) |
| 1777 | Client daemon API (localhost only) |

## Advanced: Custom Config

Most users don't need config files. For custom deployments:

```bash
# Print example configs
k2s config show > server.yml
k2 config show > client.yml

# Edit and run
sudo k2s run --foreground -c server.yml
sudo k2 run -c client.yml
```

## Troubleshooting

**"permission denied"** — Run with `sudo`. Both k2s and k2 need root for port 443 and TUN device.

**"wire: not connected"** — Check that your VPS firewall allows TCP+UDP on port 443. QUIC needs UDP.

**"TLS handshake error"** — The connect URL may be stale. Run `sudo k2s run` on the server to get a fresh URL.

**Client can't reach anything** — Try proxy mode first (`k2 up --mode proxy <url>`) to rule out TUN issues. If proxy mode works, the tunnel is fine — check your TUN/routing setup.
