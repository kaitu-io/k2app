#!/bin/sh
set -e

# Sidecar Entrypoint
# Generates config.yaml from environment variables, then starts k2-sidecar.

CONFIG_FILE="/tmp/sidecar-config.yaml"

cat > "$CONFIG_FILE" <<EOF
k2_center:
  enabled: true
  base_url: "${K2_CENTER_URL:-https://k2.52j.me}"
  timeout: "10s"
  secret: "${K2_NODE_SECRET}"
  report_interval: "${REPORT_INTERVAL:-120s}"
  billing_start_date: "${K2_NODE_BILLING_START_DATE:-}"
  traffic_limit_gb: ${K2_NODE_TRAFFIC_LIMIT_GB:-0}

node:
  name: "${K2_NODE_NAME:-}"
  region: "${K2_NODE_REGION:-}"

tunnel:
  enabled: true
  domain: "${K2_DOMAIN:-}"
  port: ${K2_PORT:-443}

oc:
  enabled: ${K2OC_ENABLED:-false}
  domain: "${K2OC_DOMAIN:-}"
  port: 443
  listen_port: 443
  radius_server: "k2-sidecar"

ech:
  enabled: ${K2_ECH_ENABLED:-false}

relay:
  enabled: ${K2_HAS_RELAY:-false}

test_node: ${K2_TEST_NODE:-false}
config_dir: "${K2_CONFIG_DIR:-/etc/kaitu}"
k2v4_port: "${K2V4_PORT:-8443}"
EOF

echo "[entrypoint] Generated config: $CONFIG_FILE"
exec k2-sidecar -c "$CONFIG_FILE"
