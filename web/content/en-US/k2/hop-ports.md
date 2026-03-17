---
title: Port Hopping Configuration Guide
date: 2026-03-06
summary: Improve QUIC connection stability through UDP port hopping, preventing single-port throttling
section: technical
order: 7.5
draft: false
---

# Port Hopping Configuration Guide

QUIC communicates over a single UDP port by default. Some networks apply QoS throttling or blocking on fixed ports. Port hopping lets the client randomly select a UDP port from a configured range, bypassing single-port restrictions.

## How It Works

```
Client --[UDP:50042]--> Server Firewall --[REDIRECT to :443]--> k2s
```

The client picks a random UDP port from the configured range. The server's firewall NAT rules redirect traffic from these ports to the k2s listening port (443).

## Prerequisites

- k2s server installed and running (`sudo k2s setup`)
- Ports 443/tcp and 443/udp are open

## Step 1: Configure Port Redirect

Set up firewall rules on the server to redirect UDP traffic from the hop port range to port 443.

### Ubuntu / Debian (nftables)

```bash
# Add NAT redirect rule
sudo nft add table ip nat
sudo nft add chain ip nat prerouting { type nat hook prerouting priority 0 \; }
sudo nft add rule ip nat prerouting udp dport 50000-50100 redirect to :443

# Persist
sudo nft list ruleset > /etc/nftables.conf
sudo systemctl enable nftables
```

### Ubuntu / Debian (iptables, legacy)

```bash
sudo iptables -t nat -A PREROUTING -p udp --dport 50000:50100 -j REDIRECT --to-port 443

# Persist
sudo apt install -y iptables-persistent
sudo netfilter-persistent save
```

### CentOS / RHEL / Rocky / AlmaLinux (firewalld)

```bash
sudo firewall-cmd --permanent --add-forward-port=port=50000-50100:proto=udp:toport=443
sudo firewall-cmd --reload
```

### Alpine Linux (iptables)

```bash
sudo iptables -t nat -A PREROUTING -p udp --dport 50000:50100 -j REDIRECT --to-port 443

# Persist
sudo rc-update add iptables
sudo /etc/init.d/iptables save
```

### Arch Linux (nftables)

```bash
sudo nft add table ip nat
sudo nft add chain ip nat prerouting { type nat hook prerouting priority 0 \; }
sudo nft add rule ip nat prerouting udp dport 50000-50100 redirect to :443

sudo nft list ruleset > /etc/nftables.conf
sudo systemctl enable nftables
```

## Step 2: Open Firewall Ports

Ensure inbound UDP traffic on the hop port range is allowed.

### ufw

```bash
sudo ufw allow 50000:50100/udp
```

### firewalld

```bash
sudo firewall-cmd --permanent --add-port=50000-50100/udp
sudo firewall-cmd --reload
```

### iptables

```bash
sudo iptables -A INPUT -p udp --dport 50000:50100 -j ACCEPT
```

### Cloud Security Groups

Add an inbound rule in your cloud platform's security group / firewall settings:

| Protocol | Port Range | Source |
|----------|-----------|--------|
| UDP | 50000-50100 | 0.0.0.0/0 |

Applies to AWS, Alibaba Cloud, Tencent Cloud, GCP, Azure, etc.

## Step 3: Update Client URI

Add the `&hop=50000-50100` parameter to the connection URI:

```
k2v5://alice:token@1.2.3.4:443?ech=...&pin=...&hop=50000-50100&country=JP#tokyo
```

Paste the updated URI into the client's node management page.

## Verification

### Server Side

```bash
# nftables
sudo nft list ruleset | grep 50000

# iptables
sudo iptables -t nat -L -n | grep 50000
```

### Client Side

After connecting, check the logs to confirm hop ports are in use.

## Customizing Port Range

- Default range 50000-50100 (101 ports), recommend at least 50 ports
- Port range must not conflict with other services on the server
- Starting port should be >= 49152 (dynamic/private port range)

## FAQ

**Does port hopping expose the real connection?**

No. Port hopping only changes the local UDP source port. ECH encryption, TLS fingerprint mimicry, and all other stealth mechanisms remain unaffected. Port hopping is an additional defense layer on top of stealth — countering port-based QoS throttling.

**How many ports are needed at minimum?**

At least 20 ports are recommended (e.g., `hop=40000-40019`). k2s defaults to 20 ports. The client randomly selects and periodically rotates ports, sufficient to evade most port-statistics-based throttling.

**Can port hopping and ECH be used together?**

Absolutely, and it's recommended. ECH encrypts SNI for stealth, port hopping evades UDP QoS throttling — two independent defense layers that stack. This multi-layered defense architecture is unique to k2.

**Does Hysteria2 support port hopping?**

Hysteria2 also supports port hopping, but k2's implementation is deeply integrated with ECH + k2cc — port rotation does not interfere with k2cc's rate probing or QUIC connection persistence. This seamless integration is a k2 architectural advantage.
