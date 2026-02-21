---
title: 1-Minute Quickstart
date: 2026-02-21
summary: Start a k2s server and connect with the k2 client in under a minute. The server auto-generates all keys; the client connects with a single command.
section: getting-started
order: 2
draft: false
---

# 1-Minute Quickstart

This guide gets you up and running as quickly as possible. You need a Linux VPS with a public IP and a client machine to connect from.

## Step 1: Deploy the Server (30 seconds)

Run this on your server:

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
sudo k2s run
```

On **first run**, k2s automatically:

- Generates TLS self-signed certificates (RSA + EC dual certificates)
- Generates ECH keys
- Installs a systemd service
- Prints a ready-to-use connection URL:

```
k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

> To see the connection URL again later, just run `sudo k2s run` again.

## Step 2: Connect the Client (30 seconds)

Run this on your client machine (replace the URL with the one printed in step 1):

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

Once connected, all traffic is routed through the encrypted k2 tunnel.

## Common Commands

```bash
k2 status     # Show connection status
k2 down       # Disconnect
k2 up <url>   # Reconnect
```

## Proxy Mode (No Root Required)

If you prefer not to use root to create a TUN device, use proxy mode:

```bash
k2 up --mode proxy k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

Proxy mode starts a local SOCKS5 proxy at `socks5://127.0.0.1:1080`. No system route changes are made.

---

Next: [k2s Server Deployment](/k2/server) for advanced configuration and Docker deployment.
