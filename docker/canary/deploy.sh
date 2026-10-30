#!/bin/bash
# deploy.sh — put an image on the k2s canary (runs locally, drives the host over SSH).
#
#   docker/canary/deploy.sh public.ecr.aws/d6n9t2r2/k2s:v0.4.14-918b82f1   # a released image
#   docker/canary/deploy.sh --binary path/to/k2s-linux-amd64               # an unreleased build
#
# --binary builds the image on the host from docker/k2s/Dockerfile (tag
# k2s:local-<sha8 of the binary>); nothing is pushed to ECR. Recreates with
# `up -d -t 2` like node-upgrade.sh, waits for "k2s server ready", then saves the
# connect URL (it carries the canary's udid:token) to $URL_FILE, mode 600 —
# never print it.
set -euo pipefail
HOST="${CANARY_HOST:-47.131.131.14}"
KEY="${CANARY_SSH_KEY:-$HOME/.ssh/id_rsa}"
URL_FILE="${CANARY_URL_FILE:-$HOME/.config/k2s-canary/connect-url}"
DIR=/apps/k2s-canary
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=15 -i "$KEY" "ubuntu@$HOST")
SCP=(scp -q -o BatchMode=yes -i "$KEY")

if [ "${1:-}" = "--binary" ]; then
  BIN="${2:?binary path}"
  file "$BIN" | grep -q 'ELF 64-bit.*x86-64' || { echo "FAIL: $BIN is not a linux/amd64 binary"; exit 2; }
  IMAGE="k2s:local-$(shasum -a 256 "$BIN" | cut -c1-8)"
  "${SSH[@]}" "rm -rf /tmp/k2s-build && mkdir -p /tmp/k2s-build"
  "${SCP[@]}" "$BIN" "ubuntu@$HOST:/tmp/k2s-build/k2s"
  "${SCP[@]}" "$ROOT/docker/k2s/Dockerfile" "$ROOT/docker/k2s/entrypoint.sh" "ubuntu@$HOST:/tmp/k2s-build/"
  "${SSH[@]}" "cd /tmp/k2s-build && sudo docker build -q --platform linux/amd64 -t $IMAGE . >/dev/null && rm -rf /tmp/k2s-build"
else
  IMAGE="${1:?usage: deploy.sh <image> | --binary <path>}"
  "${SSH[@]}" "sudo docker pull -q $IMAGE >/dev/null"
fi

"${SSH[@]}" "sudo mkdir -p $DIR && sudo chown ubuntu $DIR"
"${SCP[@]}" "$HERE/docker-compose.yml" "$HERE/server.yml" "$HERE/restart-cycle.sh" "ubuntu@$HOST:$DIR/"
"${SSH[@]}" "bash -s" <<REMOTE
set -euo pipefail
cd $DIR
echo "K2S_IMAGE=$IMAGE" > .env
LOG=/var/lib/docker/volumes/canary_k2v5-data/_data/k2s.log
C=\$(sudo grep -c "k2s server ready" \$LOG 2>/dev/null || true)
sudo docker compose up -d -t 2 2>&1 | tail -2
for i in \$(seq 1 300); do
  [ "\$(sudo grep -c 'k2s server ready' \$LOG 2>/dev/null || echo 0)" -gt "\${C:-0}" ] && break; sleep 0.2
done
[ "\$(sudo grep -c 'k2s server ready' \$LOG)" -gt "\${C:-0}" ] || { echo "FAIL: k2s not ready in 60s"; sudo docker ps -a --format '{{.Names}} {{.Image}} {{.Status}}'; exit 5; }
sudo docker ps --format '{{.Names}} {{.Image}} {{.Status}}'
sudo grep 'k2s server ready' \$LOG | tail -1 | cut -c1-200
REMOTE
mkdir -p "$(dirname "$URL_FILE")"
( umask 077; "${SSH[@]}" "sudo cat /var/lib/docker/volumes/canary_k2v5-data/_data/connect-url.txt" > "$URL_FILE" )
grep -q '^k2v5://' "$URL_FILE" || { echo "FAIL: no connect URL"; exit 6; }
echo "deployed $IMAGE on $HOST; connect URL saved to $URL_FILE"
