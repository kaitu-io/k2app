#!/bin/bash
#
# Build and push kaitu slave Docker images to ECR
#
# Usage: make publish-docker
#        scripts/publish-docker.sh --no-latest   # push ONLY the pinned :vX.Y.Z-<commit> tag
#
# --no-latest matters: nodes without a pinned K2_VERSION run :latest, and
# docker/scripts/auto-update.sh pulls it nightly, so pushing :latest is itself a
# fleet rollout. Use --no-latest for any build that must wait for a deliberate,
# staged rollout (canary / diagnostic builds).
#
# Images (each with :latest + :vX.Y.Z-<commit>, unless --no-latest):
#   - public.ecr.aws/d6n9t2r2/k2s
#   - public.ecr.aws/d6n9t2r2/k2-sidecar
#
set -e

PUSH_LATEST=1
for arg in "$@"; do
    case "$arg" in
        --no-latest) PUSH_LATEST=0 ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

ECR_REGISTRY="public.ecr.aws/d6n9t2r2"

# Version tag: vX.Y.Z-<8-char commit>
VERSION=$(node -p "require('./package.json').version")
COMMIT=$(git rev-parse --short=8 HEAD)
TAG="v${VERSION}-${COMMIT}"

K2V5_IMAGE="${ECR_REGISTRY}/k2s"
SIDECAR_IMAGE="${ECR_REGISTRY}/k2-sidecar"

echo "================================================"
echo "  Build & Push Slave Docker Images"
echo "  Tag: ${TAG}"
echo "================================================"
echo ""

# 1. Cross-compile binaries
echo "[1/4] Building binaries (linux/amd64)..."
# Stamp version/commit so the deployed binary self-reports what it is:
# `k2s version` is the only post-deploy check that doesn't depend on trusting
# the image tag. Without this the binary reports "k2s dev (none)" and a
# rollout can only be verified by image digest.
cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -tags release \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o ../docker/k2s/k2s ./cmd/k2s
cd ..
echo "  -> docker/k2s/k2s"

cd docker/sidecar && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w" \
    -o k2-sidecar .
cd ../..
echo "  -> docker/sidecar/k2-sidecar"

# 2. Docker build (alpine)
echo ""
echo "[2/4] Building Docker images (alpine)..."
LATEST_K2S=(); LATEST_SIDECAR=()
if [ "$PUSH_LATEST" = 1 ]; then
    LATEST_K2S=(-t "${K2V5_IMAGE}:latest"); LATEST_SIDECAR=(-t "${SIDECAR_IMAGE}:latest")
fi
docker build --progress=plain --platform linux/amd64 \
    "${LATEST_K2S[@]}" \
    -t "${K2V5_IMAGE}:${TAG}" \
    docker/k2s/
docker build --progress=plain --platform linux/amd64 \
    "${LATEST_SIDECAR[@]}" \
    -t "${SIDECAR_IMAGE}:${TAG}" \
    docker/sidecar/

# 3. Verify alpine base
echo ""
echo "[3/4] Verifying images..."
for img in "${K2V5_IMAGE}:${TAG}" "${SIDECAR_IMAGE}:${TAG}"; do
    os=$(docker inspect --format='{{.Os}}/{{.Architecture}}' "$img")
    echo "  ${img##*/}  ${os}"
done

# 4. ECR login + push
echo ""
echo "[4/4] Pushing to ECR..."
aws ecr-public get-login-password --region us-east-1 | \
    docker login --username AWS --password-stdin public.ecr.aws

if [ "$PUSH_LATEST" = 1 ]; then
    docker push "${K2V5_IMAGE}:latest"
    docker push "${SIDECAR_IMAGE}:latest"
fi
docker push "${K2V5_IMAGE}:${TAG}"
docker push "${SIDECAR_IMAGE}:${TAG}"

echo ""
echo "================================================"
echo "  Done! Images pushed to ECR."
echo "================================================"
[ "$PUSH_LATEST" = 1 ] && echo "  ${K2V5_IMAGE}:latest"
echo "  ${K2V5_IMAGE}:${TAG}"
[ "$PUSH_LATEST" = 1 ] && echo "  ${SIDECAR_IMAGE}:latest"
echo "  ${SIDECAR_IMAGE}:${TAG}"
echo ""
