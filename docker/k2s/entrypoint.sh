#!/bin/sh
set -e

# K2v5 Entrypoint Script
# 1. Wait for sidecar to generate config and certificates
# 2. Verifies k2v5-config.yaml exists
# 3. Sets up iptables DNAT rules for hop ports -> 443
# 4. Ensures UDP 443 is accessible
# 5. Starts k2s with shared config
#
# Environment Variables:
#   K2_HOP_PORT_MIN  - Start of hop port range (default: 10020)
#   K2_HOP_PORT_MAX  - End of hop port range (default: 10119)

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
# Step 1: Ensure UDP 443 is accessible
# =============================================================================
# Some hosts have restrictive firewalls that block UDP even with host network mode.
# This rule ensures QUIC (UDP 443) works alongside TCP 443.

echo "[INFO] Ensuring UDP 443 is accessible..."
iptables -C INPUT -p udp --dport 443 -j ACCEPT 2>/dev/null || \
    iptables -I INPUT -p udp --dport 443 -j ACCEPT 2>/dev/null || true

# =============================================================================
# Step 2: Setup iptables DNAT for hop ports (if configured)
# =============================================================================

if [ -n "$K2_HOP_PORT_MIN" ] && [ -n "$K2_HOP_PORT_MAX" ]; then
    echo "[INFO] Setting up hop port DNAT: $K2_HOP_PORT_MIN-$K2_HOP_PORT_MAX -> 443"

    # TCP hop ports -> 443
    iptables -t nat -A PREROUTING -p tcp \
        --dport "$K2_HOP_PORT_MIN:$K2_HOP_PORT_MAX" \
        -j REDIRECT --to-port 443 2>/dev/null || {
        echo "[WARN] Failed to set up TCP hop port DNAT (may require NET_ADMIN capability)"
    }

    # UDP hop ports -> 443
    iptables -t nat -A PREROUTING -p udp \
        --dport "$K2_HOP_PORT_MIN:$K2_HOP_PORT_MAX" \
        -j REDIRECT --to-port 443 2>/dev/null || {
        echo "[WARN] Failed to set up UDP hop port DNAT (may require NET_ADMIN capability)"
    }

    echo "[INFO] Hop port DNAT rules configured"
else
    echo "[INFO] Hop port DNAT not configured (K2_HOP_PORT_MIN or K2_HOP_PORT_MAX not set)"
fi

# =============================================================================
# Step 3: Start k2s with shared config file
# =============================================================================

echo "[INFO] Starting k2s with config: $CONFIG_FILE"
exec ./k2s -c "$CONFIG_FILE"
