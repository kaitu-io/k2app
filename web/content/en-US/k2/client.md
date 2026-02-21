---
title: k2 Client Usage
date: 2026-02-21
summary: Install and use the k2 command-line client on Linux or macOS. Connect to a k2v5 server, manage the VPN tunnel, and choose between TUN and proxy modes.
section: getting-started
order: 4
draft: false
---

# k2 Client Usage

The k2 command-line client runs on **Linux** (x86_64 / arm64) and **macOS** (Intel / Apple Silicon).

If you prefer a graphical interface, the [Kaitu desktop client](/install) has k2 built in — no separate installation needed.

## Installation

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
```

Verify:

```bash
k2 --version
```

## Connecting

Pass the k2v5 URL printed by your server to `k2 up`:

```bash
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

After connecting, system routing is updated automatically and all traffic flows through the tunnel.

## Common Commands

```bash
# Connect (TUN mode — requires root)
sudo k2 up k2v5://...

# Connect (proxy mode — no root required)
k2 up --mode proxy k2v5://...

# Check connection status
k2 status

# Disconnect
sudo k2 down

# Show version
k2 --version

# Show help
k2 --help
```

## Connection Modes

### TUN Mode (Default)

TUN mode creates a virtual network interface that captures all system traffic, acting as a global proxy. Root privileges are required.

```bash
sudo k2 up k2v5://...
```

### Proxy Mode

Proxy mode starts a SOCKS5 proxy at `127.0.0.1:1080` without modifying system routes. Suitable when root access is unavailable or only specific applications need proxying.

```bash
k2 up --mode proxy k2v5://...
```

Configure your system or application to use `socks5://127.0.0.1:1080`.

## Configuration File

For environments where passing a URL is inconvenient, generate a config file:

```bash
k2 demo-config > client.yml
```

Connect using the config file:

```bash
sudo k2 up --config client.yml
```

## TLS Fingerprint Selection

k2 supports multiple TLS fingerprints to blend with different browser traffic:

```bash
# Chrome (default)
sudo k2 up k2v5://...?fp=chrome

# Firefox
sudo k2 up k2v5://...?fp=firefox

# Safari
sudo k2 up k2v5://...?fp=safari

# Random (rotates fingerprint periodically)
sudo k2 up k2v5://...?fp=random
```

## Status Output

Example `k2 status` output:

```
State:     connected
Server:    203.0.113.5:443
Protocol:  k2v5 (QUIC/H3)
Latency:   28ms
Uploaded:  1.2 GB
Downloaded: 8.4 GB
Uptime:    2h 15m
```

When QUIC is unavailable, the protocol automatically switches to `k2v5 (TCP-WS)` with no manual intervention needed.

## Uninstall

```bash
sudo k2 down
sudo rm /usr/local/bin/k2
```

---

Next: [Protocol Technical Details](protocol) to understand how k2v5 works under the hood.
