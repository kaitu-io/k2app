# k2tunnel

A minimalist VPN tunnel tool using HTTP/3 (WebTransport over QUIC). Inspired by qTunnel's "less is more" philosophy.

## Features

- **Simple**: ~900 lines of code total
- **Modern**: HTTP/3 WebTransport protocol (hard to detect/block)
- **Cross-platform**: macOS and Linux support
- **Modular**: Clean architecture with Provider/Core/Wire layers

## Architecture

```
Provider (TUN device) → Core (routing logic) → Wire (HTTP/3 transport)
```

## Quick Start

### Prerequisites

- Go 1.24 or later
- Root/sudo privileges (required for TUN device)
- TLS certificate (for server)

### Build

```bash
go build -o k2client ./cmd/k2client
go build -o k2server ./cmd/k2server
```

### Server Setup

1. Generate TLS certificate:

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
```

2. Start server:

```bash
sudo ./k2server \
  -listen :443 \
  -cert cert.pem \
  -key key.pem \
  -tun tun1 \
  -ip 10.0.0.1
```

3. Configure routing and NAT:

```bash
# Enable IP forwarding
sudo sysctl -w net.ipv4.ip_forward=1

# Configure NAT (replace eth0 with your network interface)
sudo iptables -t nat -A POSTROUTING -s 10.0.0.0/24 -o eth0 -j MASQUERADE
```

### Client Setup

1. Start client:

```bash
sudo ./k2client \
  -server your-server.com:443 \
  -tun tun0 \
  -ip 10.0.0.2 \
  -insecure  # Only for self-signed certs
```

2. Configure routing (route all traffic through tunnel):

```bash
# macOS
sudo route add default -interface tun0

# Linux
sudo ip route add default dev tun0
```

### Testing

```bash
# Test connectivity
ping 8.8.8.8

# Test DNS
nslookup google.com

# Test HTTP
curl http://httpbin.org/ip
```

## Configuration File

Instead of command-line flags, you can use a YAML configuration file:

```bash
# Client
sudo ./k2client -config config.yml

# Server
sudo ./k2server -config config.yml
```

See `config.example.yml` for a template.

## Command-Line Options

### k2client

```
  -server string
        Server address (e.g., server.com:443)
  -tun string
        TUN device name (default "tun0")
  -ip string
        TUN device IP address (default "10.0.0.2")
  -netmask string
        TUN device netmask (default "255.255.255.0")
  -mtu int
        TUN device MTU (default 1400)
  -protocol string
        Wire protocol (default "http3")
  -insecure
        Allow insecure TLS connections (development only)
  -config string
        Path to config file
```

### k2server

```
  -listen string
        Listen address (default ":443")
  -cert string
        TLS certificate file
  -key string
        TLS key file
  -tun string
        TUN device name (default "tun1")
  -ip string
        TUN device IP address (default "10.0.0.1")
  -netmask string
        TUN device netmask (default "255.255.255.0")
  -mtu int
        TUN device MTU (default 1400)
  -protocol string
        Wire protocol (default "http3")
  -config string
        Path to config file
```

## Troubleshooting

### "Failed to create TUN device: operation not permitted"

You need root privileges:
```bash
sudo ./k2client ...
```

### "dial webtransport: ..."

Check that:
- Server is running and accessible
- Firewall allows UDP port 443
- DNS resolves correctly
- Using `-insecure` flag with self-signed certs

### No internet after connecting

Make sure routing is configured:
```bash
# Check routes
route -n       # Linux
netstat -rn    # macOS

# Verify IP forwarding (server)
sysctl net.ipv4.ip_forward

# Check NAT rules (server)
sudo iptables -t nat -L -n -v
```

### High latency or packet loss

Try adjusting MTU:
```bash
-mtu 1300  # Or lower values
```

## Project Structure

```
k2tunnel/
├── cmd/
│   ├── k2client/     # Client entry point
│   └── k2server/     # Server entry point
├── config/           # Configuration file parsing
├── core/             # Core tunnel logic
│   ├── packet.go     # IP packet framing
│   ├── router.go     # Routing decisions
│   ├── direct.go     # Direct bypass
│   └── tunnel.go     # Tunnel implementation
├── provider/         # Platform-specific providers
│   ├── provider.go   # Provider interface
│   └── tun_unix.go   # TUN device (macOS/Linux)
└── wire/             # Wire layer (transport)
    └── wire.go       # HTTP/3 WebTransport
```

## Future Enhancements

- [ ] Smart routing (GeoIP-based)
- [ ] iOS PacketTunnelProvider support
- [ ] Android VPN service support
- [ ] Windows TAP adapter support
- [ ] OpenWRT integration
- [ ] Automatic reconnection
- [ ] Traffic statistics
- [ ] Web UI management

## License

MIT

## Credits

Inspired by:
- [qTunnel](https://github.com/getqujing/qtunnel) - Minimalist design philosophy
- [go-gost](https://github.com/go-gost/gost) - Network transport protocols
- [WireGuard](https://www.wireguard.com/) - TUN device implementation
