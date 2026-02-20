
sudo mkdir -p /apps/kaitu-slave/logs/
sudo chown -R ubuntu: /apps/kaitu-slave

cat > /apps/kaitu-slave/docker-compose.yml << 'EOF'

version: '3.8'

# Kaitu Slave Docker Compose
# Architecture:
# - k2-slave-sidecar: registration, config generation, RADIUS proxy (bridge network)
# - k2-slave: K2 protocol tunnel (SNI Router mode, host network for iptables)
# - k2-oc: OpenConnect tunnel (bridge network for sysctls)
#
# Network Design:
# - k2-internal (bridge): k2-slave-sidecar, k2-oc (inter-container communication)
# - host network: k2-slave (required for SNI routing + iptables DNAT)
#
# Communication:
# - k2-oc -> k2-slave-sidecar: RADIUS auth via Docker DNS (UDP 1812)
# - k2-slave -> k2-oc: via host port mapping (127.0.0.1:K2OC_PORT -> container:443)
# - External -> k2-slave: host port 443
# - External -> k2-oc: host port K2OC_PORT (mapped to container:443)
#
# Dependency Management:
# - k2-slave-sidecar creates /etc/kaitu/.ready after config generation
# - k2-slave waits for /etc/kaitu/.ready (certificates)
# - k2-oc waits for /etc/ocserv/ocserv.conf (config file)
# - Healthcheck on k2-slave-sidecar monitors /etc/kaitu/.ready
#
# Logging Configuration:
# Set K2_LOG_TO_FILE=true to enable file logging for development/debugging
# Logs will be saved to /etc/kaitu-slave/logs/
#
# Example usage:
#   K2_LOG_TO_FILE=true docker-compose up -d
#   tail -f logs/k2-slave.log
#
# Environment Variables:
#   K2_LOG_TO_FILE    - Enable file logging (default: false)
#   K2_LOG_LEVEL      - Log level: debug, info, warn, error (default: info)
#   K2_LOG_DIR        - Log directory (default: ./logs)

networks:
  k2-internal:
    driver: bridge

volumes:
  config:  # Shared config volume for certificates and configs

services:
  # K2 Sidecar - Central management service (bridge network)
  # Provides RADIUS proxy for k2-oc authentication
  k2-slave-sidecar:
    image: public.ecr.aws/d6n9t2r2/k2-slave-sidecar:latest
    container_name: k2-slave-sidecar
    restart: unless-stopped
    networks:
      - k2-internal
    volumes:
      - config:/etc/kaitu
      - config:/etc/ocserv
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
    environment:
      - K2_NODE_SECRET=${K2_NODE_SECRET}
      - K2_CENTER_URL=${K2_CENTER_URL:-https://k2.52j.me}
      - K2_NODE_NAME=${K2_NODE_NAME:-}
      - K2_NODE_REGION=${K2_NODE_REGION:-}
      - K2_DOMAIN=${K2_DOMAIN:-}
      - K2_PORT=${K2_PORT:-443}
      - K2OC_ENABLED=${K2OC_ENABLED:-false}
      - K2OC_DOMAIN=${K2OC_DOMAIN:-}
      - K2OC_PORT=${K2OC_PORT:-10001}
      - K2_CONFIG_DIR=/etc/kaitu
      - REPORT_INTERVAL=${REPORT_INTERVAL:-120s}
      - K2_NODE_BILLING_START_DATE=${K2_NODE_BILLING_START_DATE:-}
      - K2_NODE_TRAFFIC_LIMIT_GB=${K2_NODE_TRAFFIC_LIMIT_GB:-0}
      - K2_TEST_NODE=${K2_TEST_NODE:-false}
      - K2_HAS_RELAY=${K2_HAS_RELAY:-false}
      - K2_JUMP_PORT_MIN=${K2_JUMP_PORT_MIN:-10020}
      - K2_JUMP_PORT_MAX=${K2_JUMP_PORT_MAX:-10119}
    healthcheck:
      test: ["CMD", "test", "-f", "/etc/kaitu/.ready"]
      interval: 2s
      timeout: 1s
      retries: 60
      start_period: 5s

  # K2 Slave - K2 protocol tunnel (with SNI Router)
  # Using host network mode with iptables DNAT for jump ports
  # Ports:
  # - TCP 443: WebSocket + HTTP (SNI Router)
  # - UDP 443: QUIC (K2v4 with ALPN: k2v4-bbr, k2v4-brutal, k2v4-pcc)
  # - Jump ports 10020-10119: Redirected to 443 via iptables DNAT (limited to 100 ports)
  # SNI routes to k2-oc via 127.0.0.1:K2OC_PORT (host port mapping)
  k2-slave:
    image: public.ecr.aws/d6n9t2r2/k2-slave:latest
    container_name: k2-slave
    restart: unless-stopped
    network_mode: host
    depends_on:
      k2-slave-sidecar:
        condition: service_healthy
    cap_add:
      - NET_ADMIN
    volumes:
      - config:/etc/kaitu:ro
      - ${K2_LOG_DIR:-./logs}:/logs
    environment:
      # Config is shared from sidecar via /etc/kaitu/config.yaml
      - K2_CONFIG_DIR=/etc/kaitu
      # Logging configuration
      - K2_LOG_LEVEL=${K2_LOG_LEVEL:-info}
      - K2_LOG_TO_FILE=${K2_LOG_TO_FILE:-false}
      - K2_LOG_FILE=/logs/k2-slave.log
      # Jump port configuration for iptables DNAT (limited to 100 ports)
      - K2_JUMP_PORT_MIN=${K2_JUMP_PORT_MIN:-10020}
      - K2_JUMP_PORT_MAX=${K2_JUMP_PORT_MAX:-10119}
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  # K2 OC Tunnel - OpenConnect protocol (bridge network)
  # Bridge network allows sysctls (net.ipv4.ip_forward) to work
  # Port mapping: K2OC_PORT:443 (external port -> container port 443)
  # k2-slave routes SNI traffic to 127.0.0.1:K2OC_PORT on host
  # RADIUS auth via k2-slave-sidecar:1812 (Docker DNS on bridge network)
  k2-oc:
    image: public.ecr.aws/d6n9t2r2/k2-oc:latest
    container_name: k2-oc
    restart: unless-stopped
    networks:
      - k2-internal
    privileged: true
    sysctls:
      - net.ipv4.ip_forward=1
    cap_add:
      - SYS_NICE
      - NET_ADMIN
      - NET_RAW
    depends_on:
      k2-slave-sidecar:
        condition: service_healthy
    ports:
      - "${K2OC_PORT:-10001}:443"
    volumes:
      - config:/etc/ocserv:ro
    environment:
      - OCSERV_CONFIG=/etc/ocserv/ocserv.conf
      - DEBUG=${DEBUG:-false}

EOF


