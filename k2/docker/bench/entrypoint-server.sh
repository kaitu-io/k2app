#!/bin/bash
set -e

echo "=== Bench Server ==="

# bench.example.com → localhost so k2s/sing-box proxy exit resolves to local file server
echo "127.0.0.1 bench.example.com" >> /etc/hosts

# Generate test files
mkdir -p /tmp/bench
echo "Generating test files..."
dd if=/dev/urandom of=/tmp/bench/1M.bin    bs=1M count=1    status=none
dd if=/dev/urandom of=/tmp/bench/10M.bin   bs=1M count=10   status=none
dd if=/dev/urandom of=/tmp/bench/100M.bin  bs=1M count=100  status=none
echo "  Generating 2000M.bin (this takes ~30s)..."
dd if=/dev/urandom of=/tmp/bench/2000M.bin bs=1M count=2000 status=progress
echo "Test files ready."
ls -lh /tmp/bench/*.bin

# HTTP file server on :10010
# Bound to 0.0.0.0 so client container can health-check readiness.
# Benchmark traffic still goes through SOCKS5 tunnel (speedtest → k2/sing-box → server proxy exit → localhost:10010).
python3 -m http.server 10010 --bind 0.0.0.0 -d /tmp/bench &
echo "HTTP file server on :10010"

# sing-box hysteria2 server
sing-box run -c /etc/bench/hysteria2-server.json &
echo "sing-box hysteria2 server on :10013"

# k2s (foreground) — auto-provisions certs/ECH/auth into /etc/bench/k2s/
echo "Starting k2s on :443"
exec k2s run -s /etc/bench/server.yaml
