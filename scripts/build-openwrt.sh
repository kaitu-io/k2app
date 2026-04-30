#!/bin/bash
set -euo pipefail

VERSION=${VERSION:-$(node -p "require('./package.json').version")}
COMMIT=$(cd k2 && git rev-parse --short HEAD)
OUTDIR="release/k2r/${VERSION}"

TARGETS=(
    "linux:arm64::arm64"
    "linux:amd64::amd64"
    "linux:arm:7:armv7"
)

# 1. Build webapp
echo "=== Building webapp ==="
cd webapp && yarn build && cd ..

# 2. Copy dist to gateway embed path
echo "=== Copying webapp to k2/gateway/dist/ ==="
rm -rf k2/gateway/dist
cp -r webapp/dist k2/gateway/dist

# 3. Cross-compile each target
mkdir -p "${OUTDIR}"
for target in "${TARGETS[@]}"; do
    IFS=':' read -r goos goarch goarm name <<< "$target"
    echo "=== Building k2r-linux-${name} ==="

    env CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" ${goarm:+GOARM="${goarm}"} \
        go build \
        -C k2 \
        -tags release \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o "../${OUTDIR}/k2r-linux-${name}" \
        ./cmd/k2r

    # Verify architecture
    file "${OUTDIR}/k2r-linux-${name}"
done

# 4. Generate checksums
echo "=== Generating checksums ==="
(cd "${OUTDIR}" && shasum -a 256 k2r-linux-* > checksums.txt)
cat "${OUTDIR}/checksums.txt"

# 5. Restore gateway/dist placeholder
git -C k2 checkout -- gateway/dist/

echo "=== Build complete ==="
ls -lh "${OUTDIR}"/k2r-linux-*
