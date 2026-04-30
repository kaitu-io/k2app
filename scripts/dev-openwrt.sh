#!/usr/bin/env bash
set -euo pipefail

# Local OpenWrt (k2r gateway) dev: cross-compile k2r for linux/$ARCH,
# run it in Docker on :1779, start Vite dev server with gateway proxy +
# window.__K2_GATEWAY__ injection.
#
# Bridge selection:
#   vite.config.openwrt.ts injects window.__K2_GATEWAY__ in dev →
#   webapp main.tsx routes to gateway-k2.ts (matches production)
#
# k2r `run` only opens the HTTP API; TPROXY/nftables only kick in at
# connect time, so the container does not need NET_ADMIN to serve UI.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Match Docker's native arch on the host to avoid emulation overhead.
HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
    arm64|aarch64) GOARCH=arm64; PLATFORM=linux/arm64 ;;
    x86_64)        GOARCH=amd64; PLATFORM=linux/amd64 ;;
    *) echo "Unsupported host arch: $HOST_ARCH" >&2; exit 1 ;;
esac

GATEWAY_PORT=${K2_GATEWAY_PORT:-1779}
VITE_PORT=${VITE_PORT:-1422}
CONTAINER=k2app-dev-openwrt
BIN_DIR="$ROOT_DIR/build"
K2R_BIN="$BIN_DIR/k2r-linux-${GOARCH}"

cleanup() {
    echo ""
    echo "[dev-openwrt] Stopping..."
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
    if [ -n "${VITE_PID:-}" ]; then
        kill "$VITE_PID" 2>/dev/null || true
        wait "$VITE_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT INT TERM

# 1. Cross-compile k2r if missing or stale.
mkdir -p "$BIN_DIR"
NEEDS_REBUILD=1
if [ -f "$K2R_BIN" ]; then
    NEEDS_REBUILD=0
    while IFS= read -r f; do
        if [ "$f" -nt "$K2R_BIN" ]; then
            NEEDS_REBUILD=1
            break
        fi
    done < <(find "$ROOT_DIR/k2/cmd/k2r" "$ROOT_DIR/k2/gateway" "$ROOT_DIR/k2/webui" -type f -name '*.go' 2>/dev/null)
fi
if [ "$NEEDS_REBUILD" = "1" ]; then
    echo "[dev-openwrt] Cross-compiling k2r for $PLATFORM..."
    VERSION=$(node -p "require('$ROOT_DIR/package.json').version")
    COMMIT=$(cd "$ROOT_DIR/k2" && git rev-parse --short HEAD)
    env CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
        go build \
        -C "$ROOT_DIR/k2" \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o "$K2R_BIN" \
        ./cmd/k2r
else
    echo "[dev-openwrt] k2r binary up-to-date, skipping rebuild."
fi

# 2. Start k2r in Docker. The container only needs to serve the HTTP API;
# TPROXY rules are only installed when the user clicks "connect".
#
# /etc/k2r/ holds storage.json (webapp _platform.storage), state.json
# (auto-reconnect), and subs/ (k2subs cache). It is mounted as a named
# Docker volume so state survives `docker rm -f` on the next rebuild.
# Wipe with: docker volume rm k2app-dev-openwrt-state
echo "[dev-openwrt] Starting k2r in Docker on :${GATEWAY_PORT}..."
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
# NET_ADMIN/NET_RAW + nftables/iptables/iproute2 + ip_forward sysctls are
# required at connect time: gateway/intercept.go probes netlink → nft → iptables
# in that order, and the alpine base image has none of them. Without these,
# doUp fails with "no firewall backend available" and the webapp sees a silent
# disconnected → idle bounce. Mirrors k2/test/k2r-uat/{Dockerfile.k2r,docker-compose.yml}.
docker run -d --name "$CONTAINER" \
    --platform "$PLATFORM" \
    -p "${GATEWAY_PORT}:${GATEWAY_PORT}" \
    --cap-add NET_ADMIN \
    --cap-add NET_RAW \
    --sysctl net.ipv4.ip_forward=1 \
    --sysctl net.ipv6.conf.all.forwarding=1 \
    --entrypoint "" \
    -v "$K2R_BIN:/usr/bin/k2r:ro" \
    -v k2app-dev-openwrt-state:/etc/k2r \
    alpine:latest \
    /bin/sh -c "apk add --no-cache nftables iptables iproute2 >/dev/null 2>&1 && mkdir -p /etc/k2r && exec /usr/bin/k2r run" >/dev/null

# 3. k2r run uses /etc/k2r/k2r.yml; we feed it via env-less default by
# generating a config inside the container.
# log.output=stderr lets `docker logs -f` show live k2r output (default is
# /var/log/kaitu/k2r.log, which is invisible to the host).
docker exec "$CONTAINER" sh -c "cat > /etc/k2r/k2r.yml <<EOF
listen: 0.0.0.0:${GATEWAY_PORT}
log:
  output: stderr
  level: debug
EOF
"
docker restart "$CONTAINER" >/dev/null

# 4. Wait for /ping.
echo "[dev-openwrt] Waiting for k2r to come up..."
for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:${GATEWAY_PORT}/ping" >/dev/null 2>&1; then
        echo "[dev-openwrt] k2r ready at http://127.0.0.1:${GATEWAY_PORT}"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "[dev-openwrt] k2r failed to start within 30s. Container logs:"
        docker logs "$CONTAINER" 2>&1 | tail -30
        exit 1
    fi
    sleep 1
done

# 5. Start Vite dev server with the OpenWrt config.
export K2_GATEWAY_PORT="$GATEWAY_PORT"
echo "[dev-openwrt] Starting Vite dev server on :${VITE_PORT}..."
cd "$ROOT_DIR/webapp"
yarn dev:openwrt --port "$VITE_PORT" --strictPort &
VITE_PID=$!

# 6. Open browser once Vite is up.
echo "[dev-openwrt] Waiting for Vite..."
for i in $(seq 1 60); do
    if curl -s -o /dev/null "http://localhost:${VITE_PORT}" 2>/dev/null; then
        echo "[dev-openwrt] Vite ready."
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "[dev-openwrt] Vite failed to start within 60s"
        exit 1
    fi
    sleep 1
done

open "http://localhost:${VITE_PORT}" 2>/dev/null || true

cat <<EOF

[dev-openwrt] Open: http://localhost:${VITE_PORT}
[dev-openwrt] (k2r :${GATEWAY_PORT} · docker logs -f ${CONTAINER}) — Ctrl+C to stop.
EOF

wait "$VITE_PID"
