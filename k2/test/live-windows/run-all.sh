#!/usr/bin/env bash
# Master test runner for Windows — runs all phases in sequence
# Usage: bash run-all.sh [--skip-deploy] [--skip-safety] [--skip-start]
#
# Diagnostics are collected automatically on any failure.
# All diag files go to test/live-windows/diag/
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

SKIP_DEPLOY=false
SKIP_SAFETY=false
SKIP_START=false
for arg in "$@"; do
    case "$arg" in
        --skip-deploy) SKIP_DEPLOY=true ;;
        --skip-safety) SKIP_SAFETY=true ;;
        --skip-start) SKIP_START=true ;;
    esac
done

# Clean old diag
rm -rf "$DIAG_DIR"
mkdir -p "$DIAG_DIR"

echo "========================================="
echo "  k2 Live Test Suite — TUN Mode (Windows)"
echo "  Diag dir: $DIAG_DIR"
echo "========================================="
echo ""

RESULTS=()
FAILED=false
run_phase() {
    local name="$1"
    local script="$2"
    echo ""
    echo "─────────────────────────────────────────"
    if bash "$SCRIPT_DIR/$script"; then
        RESULTS+=("✓ $name")
    else
        RESULTS+=("✗ $name")
        FAILED=true
    fi
    echo ""
}

# Phase 0: Build & Deploy
if [ "$SKIP_DEPLOY" = false ]; then
    run_phase "Build & Deploy" "00-build-deploy.sh"
fi

# Phase 1: Safety net
if [ "$SKIP_SAFETY" = false ]; then
    run_phase "Safety Net" "01-safety-net.sh"
fi

# Phase 2: Generate config
run_phase "Generate Config" "02-gen-config.sh"

# Phase 3: Start client
if [ "$SKIP_START" = false ]; then
    run_phase "Start Client" "03-start-client.sh"
fi

# Phase 4: DNS tests
run_phase "DNS Tests" "04-test-dns.sh"

# Phase 5: Routing tests
run_phase "Routing Tests" "05-test-routing.sh"

# Phase 6: Crash recovery
run_phase "Crash Recovery" "06-test-crash-recovery.sh"

# Phase 7: Stop & cleanup
run_phase "Stop & Cleanup" "07-stop-cleanup.sh"

# Collect final diagnostics regardless
source "$SCRIPT_DIR/env.sh"
collect_diag "final"

# Summary
echo ""
echo "========================================="
echo "  Test Summary"
echo "========================================="
for r in "${RESULTS[@]}"; do
    echo "  $r"
done
echo ""
echo "  Diagnostics: $DIAG_DIR/"
echo ""

if [ "$FAILED" = true ]; then
    echo "  Some tests failed. Review diag/ for root cause analysis."
    exit 1
fi
