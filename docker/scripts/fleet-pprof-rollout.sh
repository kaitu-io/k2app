#!/usr/bin/env bash
# fleet-pprof-rollout.sh — staged, gated rollout of a pprof-enabled k2s image to the fleet.
#
#   fleet-pprof-rollout.sh --tag v0.4.16-07a59c52 --nodes nodes.txt --compose docker-compose.yml \
#       --upgrade node-upgrade.sh --postcheck node-postcheck.sh [--dry-run] [--soak 75] \
#       [--window 0-6] [--state FILE] [--max-preflight-fail 3]
#
# nodes.txt: one `ip|name` per line, in rollout order (idle nodes first).
#
# Runs from the operator's Mac over SSH (port 1022, key auth) — no Center access key needed.
#
# Gates, in order:
#   0. time window (Asia/Bangkok hour in [start,end)); --dry-run ignores it
#   1. read-only PREFLIGHT on every node: ssh ok, /apps/k2s/{.env,docker-compose.yml} present,
#      compose md5 is a KNOWN version (old or new), both containers running, sidecar healthy.
#      A node failing preflight is skipped; more than --max-preflight-fail aborts everything
#      (something systemic is wrong — do not start).
#   2. per node, sequential: back up compose+.env → push new compose → upsert K2_PPROF=1 →
#      node-upgrade.sh <tag> → soak → node-postcheck.sh <tag> → verify pprof answers.
#      ANY failure in step 2 rolls THAT node back and STOPS the whole sweep (exit 1).
#
# Rollback of one node = restore docker-compose.yml.bak-pprof + .env.bak-pprof, `compose up -d`.
# Never prints secrets: .env is only edited with a fixed KEY=VALUE, never read back.
set -uo pipefail

TAG=""; NODES=""; COMPOSE=""; UPGRADE=""; POSTCHECK=""
DRY=0; SOAK=75; WINDOW="0-6"; STATE=""; MAXPF=3
OLD_COMPOSE_MD5="${OLD_COMPOSE_MD5:-8281bb4593182ea578b4cdeff9c49975}"   # docker/docker-compose.yml @ e295d3d8 (what the fleet runs)
SSH_USER="${KAITU_SSH_USER:-ubuntu}"; SSH_PORT="${KAITU_SSH_PORT:-1022}"

while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="$2"; shift 2 ;;
    --nodes) NODES="$2"; shift 2 ;;
    --compose) COMPOSE="$2"; shift 2 ;;
    --upgrade) UPGRADE="$2"; shift 2 ;;
    --postcheck) POSTCHECK="$2"; shift 2 ;;
    --soak) SOAK="$2"; shift 2 ;;
    --window) WINDOW="$2"; shift 2 ;;
    --state) STATE="$2"; shift 2 ;;
    --max-preflight-fail) MAXPF="$2"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 64 ;;
  esac
done
for v in TAG NODES COMPOSE UPGRADE POSTCHECK; do
  [ -n "${!v}" ] || { echo "missing --$(echo "$v" | tr 'A-Z' 'a-z')" >&2; exit 64; }
done
for f in "$NODES" "$COMPOSE" "$UPGRADE" "$POSTCHECK"; do [ -f "$f" ] || { echo "no such file: $f" >&2; exit 64; }; done
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-[0-9a-f]{8}$ ]] || { echo "tag must look like v0.4.16-07a59c52 (pinned, never :latest): $TAG" >&2; exit 64; }
STATE="${STATE:-$(dirname "$NODES")/rollout.state}"
NEW_COMPOSE_MD5=$(md5 -q "$COMPOSE" 2>/dev/null || md5sum "$COMPOSE" | cut -d' ' -f1)

ts() { date '+%F %T'; }
log() { printf '[%s] %s\n' "$(ts)" "$*"; }
SSHO=(-o ConnectTimeout=10 -o StrictHostKeyChecking=no -o BatchMode=yes -p "$SSH_PORT")
rssh()  { ssh -n "${SSHO[@]}" "$SSH_USER@$1" "${@:2}"; }      # no stdin
rsshi() { ssh    "${SSHO[@]}" "$SSH_USER@$1" "${@:2}"; }      # with stdin (script piped in)

log "rollout tag=$TAG nodes=$NODES new_compose_md5=$NEW_COMPOSE_MD5 old_compose_md5=$OLD_COMPOSE_MD5 soak=${SOAK}s window=$WINDOW dry=$DRY"

# ---- gate 0: time window -------------------------------------------------------------
if [ "$DRY" = 0 ]; then
  H=$(TZ=Asia/Bangkok date +%-H); WS=${WINDOW%-*}; WE=${WINDOW#*-}
  if [ "$H" -lt "$WS" ] || [ "$H" -ge "$WE" ]; then
    log "ABORT: Bangkok hour $H is outside the allowed window [$WS,$WE) — not touching the fleet."; exit 3
  fi
fi

touch "$STATE"
is_done() { grep -qx "$1" "$STATE"; }

# ---- gate 1: read-only preflight on every node ---------------------------------------
PF_OK=(); PF_BAD=()
log "== PREFLIGHT (read-only) =="
while IFS='|' read -r IP NAME; do
  [ -n "${IP:-}" ] || continue
  if is_done "$IP"; then log "  $NAME $IP: already done, skip"; continue; fi
  OUT=$(rssh "$IP" 'cd /apps/k2s 2>/dev/null && test -f .env && test -f docker-compose.yml && echo "md5=$(md5sum docker-compose.yml | cut -c1-32)" && echo "run=$(sudo docker ps --format "{{.Names}}" | LC_ALL=C sort | tr "\n" ",")" && echo "health=$(sudo docker inspect --format "{{.State.Health.Status}}" k2-sidecar 2>/dev/null)" && echo "img=$(sudo docker inspect --format "{{.Config.Image}}" k2s 2>/dev/null)"' 2>&1) || OUT="SSH_OR_DIR_FAIL: $OUT"
  MD5=$(printf '%s\n' "$OUT" | sed -n 's/^md5=//p'); RUN=$(printf '%s\n' "$OUT" | sed -n 's/^run=//p')
  HEALTH=$(printf '%s\n' "$OUT" | sed -n 's/^health=//p'); IMG=$(printf '%s\n' "$OUT" | sed -n 's/^img=//p')
  WHY=""
  [ -n "$MD5" ] || WHY="ssh/dir: $(printf '%s' "$OUT" | head -c 120 | tr '\n' ' ')"
  [ -z "$WHY" ] && [ "$MD5" != "$OLD_COMPOSE_MD5" ] && [ "$MD5" != "$NEW_COMPOSE_MD5" ] && WHY="compose drift md5=$MD5"
  [ -z "$WHY" ] && [ "$RUN" != "k2-sidecar,k2s," ] && WHY="containers=[$RUN]"
  [ -z "$WHY" ] && [ "$HEALTH" != "healthy" ] && WHY="sidecar=$HEALTH"
  if [ -z "$WHY" ]; then PF_OK+=("$IP|$NAME"); log "  OK   $NAME $IP  img=${IMG##*:}"
  else PF_BAD+=("$IP|$NAME"); log "  SKIP $NAME $IP  ($WHY)"; fi
done < "$NODES"
log "preflight: ok=${#PF_OK[@]} skipped=${#PF_BAD[@]}"
if [ "${#PF_BAD[@]}" -gt "$MAXPF" ]; then log "ABORT: more than $MAXPF nodes failed preflight — systemic, not starting."; exit 4; fi
if [ "$DRY" = 1 ]; then log "dry-run: stopping after preflight."; exit 0; fi
[ "${#PF_OK[@]}" -gt 0 ] || { log "nothing to do."; exit 0; }

# ---- gate 2: per-node rollout ---------------------------------------------------------
rollback() {  # $1 ip
  log "  ROLLBACK $1"
  rssh "$1" 'cd /apps/k2s && test -f docker-compose.yml.bak-pprof && test -f .env.bak-pprof && sudo cp -p docker-compose.yml.bak-pprof docker-compose.yml && sudo cp -p .env.bak-pprof .env && sudo docker compose up -d -t 2 2>&1 | tail -2' \
    || log "  ROLLBACK FAILED on $1 — manual intervention needed"
}
fail_node() {  # $1 ip $2 name $3 reason
  log "FAIL $2 $1: $3"; rollback "$1"
  log "ABORT sweep after failure on $2. Completed so far: $(wc -l < "$STATE" | tr -d ' ') node(s); state=$STATE"; exit 1
}

N=0; TOTAL=${#PF_OK[@]}
log "== ROLLOUT ($TOTAL nodes, sequential) =="
for ent in "${PF_OK[@]}"; do
  IP="${ent%%|*}"; NAME="${ent##*|}"; N=$((N+1))
  log "[$N/$TOTAL] $NAME $IP"

  # backups are taken once and never overwritten (a re-run must not clobber the pre-rollout state)
  rssh "$IP" 'cd /apps/k2s && { test -f docker-compose.yml.bak-pprof || sudo cp -p docker-compose.yml docker-compose.yml.bak-pprof; } && { test -f .env.bak-pprof || sudo cp -p .env .env.bak-pprof; } && test -f docker-compose.yml.bak-pprof && test -f .env.bak-pprof' \
    || fail_node "$IP" "$NAME" "could not back up compose/.env"

  rsshi "$IP" 'sudo tee /apps/k2s/docker-compose.yml >/dev/null' < "$COMPOSE" || fail_node "$IP" "$NAME" "push compose"
  rssh "$IP" 'cd /apps/k2s && if sudo grep -q "^K2_PPROF=" .env; then sudo sed -i "s/^K2_PPROF=.*/K2_PPROF=1/" .env; else echo "K2_PPROF=1" | sudo tee -a .env >/dev/null; fi; sudo grep -qx "K2_PPROF=1" .env' \
    || fail_node "$IP" "$NAME" "set K2_PPROF=1"

  UP=$(rsshi "$IP" "sudo bash -s $TAG" < "$UPGRADE" 2>&1); RC=$?
  printf '%s\n' "$UP" | grep -E 'prev_version|k2s_gap|UPGRADE_OK|FAIL' | sed 's/^/    /'
  { [ $RC -eq 0 ] && printf '%s\n' "$UP" | grep -q "UPGRADE_OK tag=$TAG"; } || fail_node "$IP" "$NAME" "node-upgrade.sh rc=$RC"

  log "  soak ${SOAK}s"; sleep "$SOAK"
  PC=$(rsshi "$IP" "sudo bash -s $TAG" < "$POSTCHECK" 2>&1)
  printf '%s\n' "$PC" | grep -E '^(containers|sidecar_health|since_k2s_start)' | sed 's/^/    /'
  printf '%s\n' "$PC" | grep -q "sidecar_health=healthy k2s_restarts=0" || fail_node "$IP" "$NAME" "postcheck: sidecar not healthy or k2s restarted"
  printf '%s\n' "$PC" | grep -q "panics=0"                              || fail_node "$IP" "$NAME" "postcheck: panics reported"
  [ "$(printf '%s\n' "$PC" | grep -o "$TAG" | wc -l | tr -d ' ')" -ge 2 ] || fail_node "$IP" "$NAME" "postcheck: both containers not on $TAG"

  PP=$(rssh "$IP" 'sudo docker exec k2s wget -qO- http://127.0.0.1:6060/debug/pprof/cmdline 2>&1 | tr "\0" " " | head -c 80')
  printf '%s' "$PP" | grep -q "k2s" || fail_node "$IP" "$NAME" "pprof not answering on 127.0.0.1:6060 inside k2s (got: $PP)"

  echo "$IP" >> "$STATE"; log "  DONE $NAME (pprof answering)"
done

log "== ALL DONE: $TOTAL node(s) on $TAG with K2_PPROF=1 =="
[ "${#PF_BAD[@]}" -gt 0 ] && log "NOT rolled out (failed preflight): ${PF_BAD[*]}"
exit 0
