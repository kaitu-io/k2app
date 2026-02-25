#!/usr/bin/env bash
# Windows live test environment variables
# Source this file before running test scripts
# All scripts run FROM Mac, executing commands ON Windows via SSH

export WIN_HOST="10.211.55.6"
export WIN_USER="david"
export WIN_SSH="ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no $WIN_USER@$WIN_HOST"

# k2 server (HK)
export K2_SERVER_IP="54.46.4.4"
export K2_SERVER_USER="ubuntu"
export K2_CONNECT_URL='k2v5://506bbdb6:852dfd3805f2c6c2bad991d851aa9e1b@54.46.4.4.sslip.io:443?ech=AEX-DQBBlwAgACD8i25wh7k5i2WThKxTJ1JdTcJ30tBeocqErMNcTXI8FQAEAAEAAQASY2xvdWRmbGFyZS1lY2guY29tAAA=&pin=sha256:NycTWNigrUyPy9ST5kaNNt2MsmkimLh8GOY3NImhA-Q=,sha256:vXrNHKxajsN5pgMKqZRspZOUY-u4cO5WJjaCJafjQOk=&insecure=1&ip=54.46.4.4'

# Paths (Mac side)
export K2_PROJECT_ROOT="/Users/david/projects/kaitu-io/k2"
export K2_TEST_DIR="$K2_PROJECT_ROOT/test/live-windows"
export DIAG_DIR="$K2_TEST_DIR/diag"

# Paths (Windows side)
export WIN_K2_DIR="C:\\Users\\david\\k2-test"
export WIN_K2_BIN="C:\\Users\\david\\k2-test\\k2.exe"
export WIN_K2_CONFIG="C:\\Users\\david\\k2-test\\client-test.yaml"
export WIN_K2_LOG="C:\\Users\\david\\k2-test\\k2.log"

# Parallels network safety
export WIN_GATEWAY="10.211.55.1"
export WIN_SUBNET="10.211.55.0"
export WIN_MASK="255.255.255.0"

# Test targets
export TEST_CHINA_DOMAIN="www.baidu.com"
export TEST_FOREIGN_DOMAIN="www.google.com"
export TEST_IP_CHECK_URL="https://ipinfo.io/json"

# k2 daemon API (on Windows, accessed via SSH)
export K2_API="http://127.0.0.1:1777"

# Helper: run command on Windows via SSH (strip \r from Windows output)
win() {
    $WIN_SSH "$@" | LC_ALL=C tr -d '\r'
}

# Helper: run a multi-line CMD script on Windows in one SSH call
# Usage: win_batch <<'BAT' ... BAT
win_batch() {
    $WIN_SSH "cmd /C $(cat)"
}

# Collect full diagnostics in ONE SSH call, save to Mac-side diag/
# Usage: collect_diag "label"
collect_diag() {
    local label="${1:-$(date +%H%M%S)}"
    local ts=$(date +%Y%m%d-%H%M%S)
    local out="$DIAG_DIR/${ts}-${label}"
    mkdir -p "$out"

    echo "  Collecting diagnostics → $out/"

    # Single SSH call, all commands batched
    win "cmd /C \"echo ==ROUTES== & route print -4 & echo ==DNS== & netsh interface ip show dns & echo ==IPCONFIG== & ipconfig /all & echo ==PROCS== & tasklist /FI \"IMAGENAME eq k2.exe\" & echo ==STATUS== & curl -s http://127.0.0.1:1777/api/core -d \"{\\\"action\\\":\\\"status\\\"}\" 2>nul & echo ==DAEMON_STDOUT== & type C:\\Users\\david\\k2-test\\daemon-stdout.txt 2>nul & echo ==PING_GW== & ping -n 1 -w 2000 $WIN_GATEWAY & echo ==PING_EXT== & ping -n 1 -w 3000 8.8.8.8\"" \
        > "$out/windows-state.txt" 2>&1 || true

    # Grab k2 log separately (can be large)
    win "type $WIN_K2_LOG 2>nul" > "$out/k2.log" 2>&1 || true

    # Server log
    ssh -o ConnectTimeout=5 "$K2_SERVER_USER@$K2_SERVER_IP" \
        "sudo tail -100 /var/log/k2s.log 2>/dev/null" \
        > "$out/k2s-server.log" 2>&1 || true

    echo "  Diagnostics saved: $(ls "$out/" | tr '\n' ' ')"
}

# Test result counters
PASS=0
FAIL=0
pass() { echo "  ✓ $1"; PASS=$((PASS + 1)); }
fail() {
    echo "  ✗ $1"
    FAIL=$((FAIL + 1))
    # Auto-collect on first failure of each phase
    if [ "$FAIL" -eq 1 ] && [ -n "${PHASE_NAME:-}" ]; then
        collect_diag "$PHASE_NAME-fail"
    fi
}
reset_counters() { PASS=0; FAIL=0; }
