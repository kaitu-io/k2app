---
title: k2s Server Deployment
date: 2026-02-21
summary: Deploy k2s on a Linux VPS with zero configuration. Covers system service setup, Docker deployment, advanced options, and troubleshooting.
section: getting-started
order: 3
draft: false
---

# k2s Server Deployment

k2s is the server-side component of the k2 protocol. It is designed for **zero-configuration startup**: a single command handles all key generation, certificate creation, and service registration.

## System Requirements

- **OS**: Linux (x86_64 or arm64)
- **Port**: Port **443** must be open for inbound traffic (both UDP and TCP)
- **Privileges**: root (or `NET_BIND_SERVICE` capability)

## Installation

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
```

Verify the installation:

```bash
k2s --version
```

## Starting the Server

```bash
sudo k2s run
```

On **first run**, k2s creates the following files in `/etc/k2s/`:

- `server.crt` / `server.key` — EC certificate
- `server-rsa.crt` / `server-rsa.key` — RSA certificate
- `echkey.pem` — ECH HPKE private key
- `config.yml` — Server configuration file

After startup, the terminal displays the connection URL:

```
k2s running on 0.0.0.0:443
Connect URL:
k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:abc...
```

## Retrieving the Connection URL

If you closed your terminal, you can retrieve the URL at any time:

```bash
sudo k2s run
```

Or inspect the config file directly:

```bash
cat /etc/k2s/config.yml
```

## System Service (Auto-Start)

k2s registers a systemd service on first run:

```bash
# Check service status
sudo systemctl status k2s

# Enable auto-start on boot (already enabled by default)
sudo systemctl enable k2s

# Stop / start manually
sudo systemctl stop k2s
sudo systemctl start k2s

# Follow live logs
sudo journalctl -u k2s -f
```

## Docker Deployment

The repository includes a Docker Compose configuration for containerized environments:

```bash
git clone https://github.com/kaitu-io/k2.git
cd k2/docker/
docker compose up --build
```

Default port mappings:
- **443**: k2s server (UDP + TCP)
- **1080**: k2 client SOCKS5 proxy
- **1777**: k2 daemon API (local management)

## Advanced Configuration

Generate a commented example configuration file:

```bash
k2s demo-config > server.yml
```

Common configuration options:

```yaml
listen: 0.0.0.0:443               # Bind address
public_name: cloudflare-ech.com   # ECH public name (used for camouflage)
reverse_proxy: auto               # Non-ECH connection target (auto resolves from DNS)
cert_refresh_interval: 24h        # TLS record padding template refresh period
```

## Firewall Configuration

Make sure port 443 is open for both UDP and TCP:

```bash
# iptables
sudo iptables -A INPUT -p udp --dport 443 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 443 -j ACCEPT

# ufw
sudo ufw allow 443/udp
sudo ufw allow 443/tcp
```

## Troubleshooting

**Port 443 already in use**

Check what is using it:

```bash
sudo ss -tlunp | grep :443
```

You can change the listen port in the config file, though running on a non-standard port reduces stealth effectiveness.

**Upgrading k2s**

Re-run the install script. The new binary will replace the old one while preserving configuration files:

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
sudo systemctl restart k2s
```

---

Next: [k2 Client Usage](/k2/client) to connect to your deployed server.
