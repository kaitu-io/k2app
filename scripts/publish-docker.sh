#!/bin/bash
#
# Build and push kaitu slave Docker images to ECR
#
# Usage: make publish-docker
#
# Images:
#   - public.ecr.aws/d6n9t2r2/k2v5:latest
#   - public.ecr.aws/d6n9t2r2/k2-sidecar:latest
#
set -e

ECR_REGISTRY="public.ecr.aws/d6n9t2r2"
K2V5_IMAGE="${ECR_REGISTRY}/k2v5:latest"
SIDECAR_IMAGE="${ECR_REGISTRY}/k2-sidecar:latest"

echo "================================================"
echo "  Build & Push Slave Docker Images"
echo "================================================"
echo ""

# 1. Cross-compile binaries
echo "[1/3] Building binaries (linux/amd64)..."
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

# 2. Docker build locally
echo ""
echo "[2/3] Building Docker images..."
docker build --platform linux/amd64 -t "${K2V5_IMAGE}" docker/k2s/
docker build --platform linux/amd64 -t "${SIDECAR_IMAGE}" docker/sidecar/

# 3. ECR login + push
echo ""
echo "[3/3] Pushing to ECR..."
aws ecr-public get-login-password --region us-east-1 | \
    docker login --username AWS --password-stdin public.ecr.aws
docker push "${K2V5_IMAGE}"
docker push "${SIDECAR_IMAGE}"

echo ""
echo "================================================"
echo "  Done! Images pushed to ECR."
echo "================================================"
echo "  ${K2V5_IMAGE}"
echo "  ${SIDECAR_IMAGE}"
echo ""
