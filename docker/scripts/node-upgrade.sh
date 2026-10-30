#!/bin/bash
# node-upgrade.sh — upgrade ONE k2s node to an image tag with the shortest k2s gap.
#
#   backup .env → set K2_VERSION → docker compose pull (service unaffected)
#   → phase A: recreate ONLY k2-sidecar (old k2s keeps serving) → wait healthy
#   → phase B: recreate k2s → wait for "k2s server ready" in the file log.
#
# Single-phase `docker compose up -d` puts the sidecar healthcheck wait INSIDE
# the k2s gap; splitting it keeps the gap at stop-grace + k2s start.
# Until 2026-10-30 k2s never exited on SIGTERM by itself (tcpListenLoop sat in
# Accept), so every stop ran the full grace and ended in SIGKILL with nothing
# said to clients: 13–18 s with compose's 10 s default (2026-09-07), 5.0–7.0 s
# with -t 2 (2026-10-03). Since then k2s stops accepting, sends every client
# QUIC CONNECTION_CLOSE 0x04 / TCP-WS close, and exits in milliseconds, so the
# gap is ~ container recreate + k2s start; clients redial at once. A QUIC
# stateless-reset key in cert_dir covers SIGKILL/crash from the second restart on.
#
# Usage (from the kaitu-center MCP, runs ON the node; exit != 0 ⇒ stop the sweep):
#   exec_on_node(ip, "sudo bash -s v0.4.10-<commit8>", scriptPath="docker/scripts/node-upgrade.sh", timeout=300)
# Then, ~75 s later, run node-postcheck.sh with the same tag before the next node.
# Rollback = restore the .env.bak-* it wrote, then `docker compose pull && up -d`.
# Never prints secrets.
set -euo pipefail
TAG="${1:?tag}"
cd /apps/k2s
[ -f .env ] || { echo "FAIL: no .env"; exit 2; }
BK=".env.bak-ccobs-$(date -u +%Y%m%d)"
[ -f "$BK" ] || cp -p .env "$BK"
PREV=$(grep -E '^K2_VERSION=' .env | cut -d= -f2- || true)
echo "prev_version=${PREV:-<unset>} backup=$BK"
if grep -qE '^K2_VERSION=' .env; then sed -i "s/^K2_VERSION=.*/K2_VERSION=$TAG/" .env; else echo "K2_VERSION=$TAG" >> .env; fi
grep -qx "K2_VERSION=$TAG" .env || { echo "FAIL: .env not updated"; exit 3; }
T0=$(date -u +%s)
docker compose pull -q 2>&1 | tail -3 || { echo "FAIL: pull"; exit 4; }
T1=$(date -u +%s); echo "pull_s=$((T1-T0))"
# Phase A: sidecar only. `up -d <service>` recreates that service (+deps), never its dependents.
docker compose up -d k2-sidecar 2>&1 | tail -3
H=none
for i in $(seq 1 45); do H=$(docker inspect --format '{{.State.Health.Status}}' k2-sidecar 2>/dev/null || echo none); [ "$H" = healthy ] && break; sleep 2; done
TA=$(date -u +%s); echo "sidecar_recreate_to_healthy_s=$((TA-T1)) sidecar_health=$H"
[ "$H" = healthy ] || { echo "FAIL: sidecar not healthy (old k2s still serving)"; docker ps --format '{{.Names}} {{.Image}} {{.Status}}'; exit 6; }
OLD_K2S_IMG=$(docker inspect --format '{{.Config.Image}}' k2s 2>/dev/null || echo none)
echo "k2s_still_old=$OLD_K2S_IMG"
# Phase B: k2s. Gap starts now.
CUTSTR=$(date -u -d @"$TA" +%Y-%m-%dT%H:%M:%S)
TB=$(date -u +%s)
# Stop grace 2 s instead of compose's 10 s default: k2s exits on SIGTERM in milliseconds; the grace
# only matters for an older image that still hangs until SIGKILL.
docker compose up -d --remove-orphans -t "${STOP_T:-2}" 2>&1 | tail -3
READY=""
for i in $(seq 1 300); do
  if awk -v c="$CUTSTR" 'substr($0,6,19)>=c' logs/k2s.log 2>/dev/null | grep -q 'server ready'; then READY=$(date -u +%s); break; fi
  sleep 0.2
done
if [ -z "$READY" ]; then echo "FAIL: k2s not ready within 60s"; docker ps --format '{{.Names}} {{.Image}} {{.Status}}'; exit 5; fi
echo "k2s_gap_upper_bound_s=$((READY-TB)) ready_at=$(date -u -d @"$READY" +%FT%TZ)"
# measurement only — must never fail the upgrade (set -e / pipefail)
SD=$(awk -v c="$CUTSTR" 'substr($0,6,19)>=c' logs/k2s.log | grep -m1 "Shutting down" | cut -c6-28 || true)
RD=$(awk -v c="$CUTSTR" 'substr($0,6,19)>=c' logs/k2s.log | grep -m1 "k2s server ready" | cut -c6-28 || true)
if [ -n "$SD" ] && [ -n "$RD" ]; then echo "no_accept_window sigterm=$SD ready=$RD ms=$(( ($(date -u -d "${RD}Z" +%s%N) - $(date -u -d "${SD}Z" +%s%N)) / 1000000 ))"; else echo "no_accept_window unmeasured sigterm=${SD:-?} ready=${RD:-?}"; fi
docker ps --format '{{.Names}} {{.Image}} {{.Status}}'
docker logs --since "$(date -u -d @"$T1" +%FT%TZ)" k2-sidecar 2>&1 | grep -E 'Registration completed|Tunnel registered|Traffic monitor initialized|usage-reporter-start|Metering disabled' | sed -E 's/(SECRET|CLAIM|TOKEN)=[^ ]*/\1=<redacted>/g' | head -6
N=$(docker ps --format '{{.Image}}' | grep -c ":$TAG$" || true)
[ "$N" = 2 ] || { echo "FAIL: expected 2 containers on $TAG, got $N"; exit 7; }
echo "UPGRADE_OK tag=$TAG"
