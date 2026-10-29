#!/usr/bin/env bash
# Phase 2: Generate client config and upload to Windows
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "=== Phase 2: Generate Client Config ==="

# Generate config with RouteExcludeAddrs to protect Parallels subnet
cat > /tmp/k2-win-config.yaml <<EOF
# Windows live test client config
server: "$K2_CONNECT_URL"

mode: tun

tun:
  ipv4: "198.18.0.7/15"
  ipv6: "fdfe:dcba:9876::7/64"
  routeExclude:
    - "$WIN_SUBNET/24"
    - "$K2_SERVER_IP/32"

dns:
  direct:
    - "114.114.114.114:53"
    - "223.5.5.5:53"
  proxy:
    - "8.8.8.8:53"
    - "1.1.1.1:53"

rule:
  global: true

log:
  level: debug
  output: "C:\\\\Users\\\\david\\\\k2-test\\\\k2.log"
EOF

echo "Config generated:"
cat /tmp/k2-win-config.yaml
echo ""

# Upload config
echo "Uploading config to Windows..."
scp /tmp/k2-win-config.yaml "$WIN_USER@$WIN_HOST:k2-test/client-test.yaml"
echo "  Uploaded to $WIN_K2_CONFIG"

# Validate config on Windows
echo "Validating config..."
VALIDATE=$(win "$WIN_K2_BIN -t -c $WIN_K2_CONFIG 2>&1" || echo "VALIDATE_FAILED")
echo "  $VALIDATE"

if echo "$VALIDATE" | grep -qi "fail\|error\|invalid"; then
    echo "  Config validation FAILED"
    exit 1
fi

echo ""
echo "=== Phase 2 complete ==="
