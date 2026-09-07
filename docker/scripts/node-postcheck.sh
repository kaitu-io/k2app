#!/bin/bash
# node-postcheck.sh — READ-ONLY gate after node-upgrade.sh (run ~75 s after it):
# both containers on the tag, sidecar healthy, k2s RestartCount 0, clients
# reconnecting (auth_ok/opened > 0 on a non-idle node), cc-summary lines
# appearing, 0 ccSummaryLoop panics, ERROR kinds (expect only the pre-existing
# "TCP proxy dial" / "TLS handshake" / "proxy blocked" classes), sidecar
# usage-reporter cycling, k2s memory.
#
# Usage: exec_on_node(ip, "sleep 75; sudo bash -s v0.4.10-<commit8>", scriptPath="docker/scripts/node-postcheck.sh", timeout=150)
# Note: `server_ready=0` with everything else fine means the 20 MB log rotated
# right after the upgrade — the line is in the newest k2s-*.log.gz.
TAG="${1:?tag}"
echo "containers: $(docker ps --format '{{.Names}}={{.Image}}|{{.Status}}' | tr '\n' ' ')"
echo "sidecar_health=$(docker inspect --format '{{.State.Health.Status}}' k2-sidecar 2>/dev/null) k2s_restarts=$(docker inspect --format '{{.RestartCount}}' k2s 2>/dev/null)"
START=$(docker inspect --format '{{.State.StartedAt}}' k2s | cut -c1-19)
L=$(awk -v c="$START" 'substr($0,6,19)>=c' /apps/k2s/logs/k2s.log)
echo "since_k2s_start=$START: server_ready=$(printf '%s\n' "$L" | grep -c 'server ready') auth_ok=$(printf '%s\n' "$L" | grep -c 'metadata auth OK') opened=$(printf '%s\n' "$L" | grep -c 'handleDatagrams started') cc_lines=$(printf '%s\n' "$L" | grep -c 'DIAG: cc-summary') panics=$(printf '%s\n' "$L" | grep -c 'panic in ccSummaryLoop') errors=$(printf '%s\n' "$L" | grep -c 'level=ERROR')"
printf '%s\n' "$L" | grep 'level=ERROR' | grep -o 'msg="[^"]*"' | sort | uniq -c | sort -rn | head -3
echo "sidecar_recent: $(docker logs --tail 40 k2-sidecar 2>&1 | grep -cE 'usage-reporter-cycle-ok') reporter-ok lines in last 40; $(docker logs --tail 200 k2-sidecar 2>&1 | grep -ciE 'error|fail') error/fail lines in last 200"
docker stats --no-stream --format '{{.Name}} mem={{.MemUsage}} cpu={{.CPUPerc}}' k2s
