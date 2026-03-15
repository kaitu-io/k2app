#!/bin/sh
set -e

# K2v5 Entrypoint Script
# 1. Wait for sidecar to generate config and certificates
# 2. Verifies k2v5-config.yaml exists
# 3. Starts k2s with shared config
#
# Hop ports (40000-40019) are handled by Docker port mapping in docker-compose.yml.
# No container-level iptables needed.

# =============================================================================
# Step 0: Wait for sidecar ready flag
# =============================================================================

CONFIG_DIR="/etc/kaitu"
CONFIG_FILE="$CONFIG_DIR/k2v5-config.yaml"
READY_FLAG="$CONFIG_DIR/.ready"

echo "[INFO] Waiting for sidecar to generate config: $CONFIG_FILE"
for i in $(seq 1 120); do
    if [ -f "$READY_FLAG" ]; then
        echo "[INFO] Sidecar ready flag found"
        break
    fi
    if [ $i -eq 120 ]; then
        echo "[ERROR] Timeout waiting for sidecar ready flag after 120s"
        exit 1
    fi
    sleep 1
done

# Verify config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "[ERROR] Config file not found: $CONFIG_FILE"
    exit 1
fi
echo "[INFO] Config file found: $CONFIG_FILE"

# =============================================================================
# Start k2s with shared config file
# =============================================================================

echo "[INFO] Starting k2s with config: $CONFIG_FILE"
exec ./k2s run -c "$CONFIG_FILE"
