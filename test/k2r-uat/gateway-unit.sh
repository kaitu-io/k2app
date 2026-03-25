#!/usr/bin/env bash
# Run gateway/ unit tests.
#
# On Linux: runs natively with race detector.
# On macOS: cross-compiles (build check only), skips execution.
#
# Usage:
#   ./gateway-unit.sh          # run all gateway tests
#   ./gateway-unit.sh -v       # verbose
#   ./gateway-unit.sh -run Nft # filter
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$PROJECT_ROOT"

EXTRA_ARGS="${*:--count=1}"

if [[ "$(uname)" == "Linux" ]]; then
    echo "→ Running gateway/ unit tests (native Linux)..."
    go test -race $EXTRA_ARGS ./gateway/...
    echo "✓ Gateway unit tests passed"
else
    echo "→ gateway/ unit tests require Linux (current: $(uname))"
    echo "→ Running cross-compile + vet checks..."
    GOOS=linux go vet ./gateway/...
    echo "✓ go vet ./gateway/ passed"
    GOOS=linux go build ./gateway/...
    echo "✓ go build ./gateway/ passed (cross-compile)"
    echo "⊘ Unit test execution skipped (not Linux). Use 'make gateway-uat' for Docker-based integration tests."
fi
