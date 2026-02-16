#!/bin/bash
set -euo pipefail

VERSION=${VERSION:-$(node -p "require('./package.json').version")}
COMMIT=$(cd k2 && git rev-parse --short HEAD)
OUTDIR="release/openwrt/${VERSION}"

TARGETS=(
    "linux:arm64::aarch64"
    "linux:amd64::x86_64"
    "linux:arm:7:armv7"
    "linux:mipsle::mipsle"
)

# 1. Build webapp
echo "=== Building webapp ==="
cd webapp && yarn build && cd ..

# 2. Copy dist to cloud embed path
echo "=== Copying webapp to k2/cloud/dist/ ==="
rm -rf k2/cloud/dist
cp -r webapp/dist k2/cloud/dist

# 3. Cross-compile each target
mkdir -p "${OUTDIR}"
for target in "${TARGETS[@]}"; do
    IFS=':' read -r goos goarch goarm name <<< "$target"
    echo "=== Building k2-openwrt-${name} ==="

    env CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" ${goarm:+GOARM="${goarm}"} \
        go build \
        -C k2 \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o "../${OUTDIR}/k2-openwrt-${name}" \
        ./cmd/k2

    # Verify architecture
    file "${OUTDIR}/k2-openwrt-${name}"
done

# 4. Package each architecture
for target in "${TARGETS[@]}"; do
    IFS=':' read -r _ _ _ name <<< "$target"
    echo "=== Packaging k2-openwrt-${name} ==="

    PKGDIR=$(mktemp -d)
    cp "${OUTDIR}/k2-openwrt-${name}" "${PKGDIR}/k2"
    cp scripts/openwrt/install.sh "${PKGDIR}/"
    cp scripts/openwrt/k2.init "${PKGDIR}/"
    cp -r scripts/openwrt/luci-app-k2 "${PKGDIR}/"

    tar -czf "${OUTDIR}/k2-openwrt-${name}-v${VERSION}.tar.gz" -C "${PKGDIR}" .
    rm -rf "${PKGDIR}"
done

# 5. Restore cloud/dist placeholder
git -C k2 checkout -- cloud/dist/

echo "=== Build complete ==="
ls -lh "${OUTDIR}"/*.tar.gz
