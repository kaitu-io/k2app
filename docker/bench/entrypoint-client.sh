#!/bin/bash
set -e

echo "=== Bench Client ==="
echo "Scenario: ${SCENARIO}"
echo "Protocols: ${PROTOCOLS}"
echo "Rounds: ${ROUNDS}, Size: ${SIZE}, Timeout: ${TIMEOUT}s"

# Apply netem
source /scenarios.sh
apply_scenario "${SCENARIO}"

# Wait for server HTTP file server
echo "Waiting for server..."
for i in $(seq 1 60); do
    if curl -s --max-time 2 "http://server:10010/1M.bin" -o /dev/null 2>/dev/null; then
        break
    fi
    [ "$i" -eq 60 ] && { echo "Server not ready after 60s, aborting."; exit 1; }
    sleep 1
done
echo "Server ready."

# Wait for k2s connect URL (auto-provisioned by k2s on startup)
echo "Waiting for k2s connect URL..."
for i in $(seq 1 60); do
    [ -f /etc/bench/k2s/connect-url.txt ] && break
    [ "$i" -eq 60 ] && { echo "k2s connect URL not found after 60s, aborting."; exit 1; }
    sleep 1
done
K2_URL=$(tr -d '\n' < /etc/bench/k2s/connect-url.txt)
# Strip ip6 parameter — external IPv6 is unreachable inside Docker
K2_URL=$(echo "$K2_URL" | sed 's/&ip6=[^&]*//')
echo "k2s connect URL ready."

# Generate k2 client config dynamically from connect URL
cat > /tmp/k2-client.yaml << CLIENTEOF
server: "${K2_URL}&tp=quic"
mode: proxy
proxy:
  listen: "0.0.0.0:10005"
dns:
  direct:
    - "8.8.8.8:53"
  proxy:
    - "8.8.8.8:53"
rule:
  global: true
log:
  level: info
  output: stderr
CLIENTEOF

# Start proxies
k2 run -c /tmp/k2-client.yaml &
K2_PID=$!
echo "K2ARC started (PID $K2_PID, SOCKS5 :10005)"

sing-box run -c /etc/bench/hy2-brutal-client.json &
SB_BRUTAL_PID=$!
echo "Hy2 Brutal started (PID $SB_BRUTAL_PID, SOCKS5 :10003)"

sing-box run -c /etc/bench/hy2-bbr-client.json &
SB_BBR_PID=$!
echo "Hy2 BBR started (PID $SB_BBR_PID, SOCKS5 :10006)"

# Wait for all SOCKS5 ports to be ready
echo "Waiting for proxies..."
for port in 10003 10006 10005; do
    for i in $(seq 1 30); do
        if curl -s --max-time 1 --socks5 127.0.0.1:$port http://127.0.0.1/ -o /dev/null 2>/dev/null; then
            break
        fi
        # Simple TCP connect check as fallback
        if (echo >/dev/tcp/127.0.0.1/$port) 2>/dev/null; then
            break
        fi
        [ "$i" -eq 30 ] && echo "WARNING: port $port not ready after 30s"
        sleep 1
    done
    echo "  :$port ready"
done

# Run benchmark
mkdir -p /results
python3 /speedtest.py \
    --server bench.example.com:10010 \
    --size "${SIZE}" \
    --protocols "${PROTOCOLS}" \
    --rounds "${ROUNDS}" \
    --timeout "${TIMEOUT}" \
    --scenario "${SCENARIO}" \
    --output "/results/${SCENARIO}.json"

echo ""
echo "Results saved to /results/${SCENARIO}.json"

kill $K2_PID $SB_BRUTAL_PID $SB_BBR_PID 2>/dev/null || true
