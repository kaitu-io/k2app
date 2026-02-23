#!/bin/bash
#
# Build and push kaitu slave Docker images to ECR
#
# Usage: make publish-docker
#
# Images (each with :latest + :vX.Y.Z-<commit>):
#   - public.ecr.aws/d6n9t2r2/k2v5
#   - public.ecr.aws/d6n9t2r2/k2-slave-sidecar
#
set -e

ECR_REGISTRY="public.ecr.aws/d6n9t2r2"

# Version tag: vX.Y.Z-<8-char commit>
VERSION=$(node -p "require('./package.json').version")
COMMIT=$(git rev-parse --short=8 HEAD)
TAG="v${VERSION}-${COMMIT}"

K2V5_IMAGE="${ECR_REGISTRY}/k2v5"
SIDECAR_IMAGE="${ECR_REGISTRY}/k2-slave-sidecar"

echo "================================================"
echo "  Build & Push Slave Docker Images"
echo "  Tag: ${TAG}"
echo "================================================"
echo ""

# 1. Cross-compile binaries
echo "[1/4] Building binaries (linux/amd64)..."
cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w" \
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
docker build --progress=plain --platform linux/amd64 \
    -t "${K2V5_IMAGE}:latest" \
    -t "${K2V5_IMAGE}:${TAG}" \
    docker/k2s/
docker build --progress=plain --platform linux/amd64 \
    -t "${SIDECAR_IMAGE}:latest" \
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

docker push "${K2V5_IMAGE}:latest"
docker push "${K2V5_IMAGE}:${TAG}"
docker push "${SIDECAR_IMAGE}:latest"
docker push "${SIDECAR_IMAGE}:${TAG}"

echo ""
echo "================================================"
echo "  Done! Images pushed to ECR."
echo "================================================"
echo "  ${K2V5_IMAGE}:latest"
echo "  ${K2V5_IMAGE}:${TAG}"
echo "  ${SIDECAR_IMAGE}:latest"
echo "  ${SIDECAR_IMAGE}:${TAG}"
echo ""
