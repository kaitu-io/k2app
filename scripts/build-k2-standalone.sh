#!/usr/bin/env bash
set -euo pipefail

# Cross-compile k2 (client) and k2s (server) standalone binaries.
# Builds for linux/{amd64,arm64} and darwin/{amd64,arm64}.
#
# Output: build/k2-standalone/
#
# Usage:
#   bash scripts/build-k2-standalone.sh              # All platforms
#   bash scripts/build-k2-standalone.sh linux-amd64   # Single platform

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
OUT_DIR="${ROOT_DIR}/build/k2-standalone"

VERSION=$(node -p "require('${ROOT_DIR}/package.json').version")
COMMIT=$(cd "${ROOT_DIR}/k2" && git rev-parse --short HEAD)
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}"

# macOS uses Network Extension (no standalone daemon). Linux only.
PLATFORMS=(
    "linux:amd64"
    "linux:arm64"
)

build_binary() {
    local os="$1" arch="$2" name="$3" pkg="$4" tags="${5:-}"
    local outfile="${OUT_DIR}/${name}-${os}-${arch}"
    local tag_flag=""
    [ -n "$tags" ] && tag_flag="-tags ${tags}"

    echo "  ${name}-${os}-${arch}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -C "${ROOT_DIR}/k2" ${tag_flag} \
        -ldflags "${LDFLAGS}" \
        -o "${outfile}" "./${pkg}"
}

filter="${1:-}"

echo "Building k2 standalone v${VERSION} (${COMMIT})"
echo ""
mkdir -p "$OUT_DIR"

for platform in "${PLATFORMS[@]}"; do
    os="${platform%%:*}"
    arch="${platform##*:}"
    key="${os}-${arch}"

    if [ -n "$filter" ] && [ "$filter" != "$key" ]; then
        continue
    fi

    build_binary "$os" "$arch" "k2"  "cmd/k2"  "nowebapp"
    build_binary "$os" "$arch" "k2s" "cmd/k2s" ""
done

echo ""

# Generate CHECKSUMS.txt (sha256sum-compatible format)
echo "Generating checksums..."
(cd "$OUT_DIR" && shasum -a 256 k2-* k2s-* > CHECKSUMS.txt)
cat "$OUT_DIR/CHECKSUMS.txt"

echo ""
echo "Output: ${OUT_DIR}/"
ls -lh "$OUT_DIR/"
