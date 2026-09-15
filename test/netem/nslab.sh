#!/usr/bin/env bash
# Lossy-QUIC escape lab on native Linux network namespaces (no Docker, no Docker Desktop VM
# stealing the host's memory). Needs root on a Linux box with iproute2 + iptables (u32/length/
# statistic/hashlimit matches) and the cross-compiled binaries in .build/ (see run.sh build_binaries):
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o test/netem/.build/{k2,k2s,gen-config} ./cmd/k2 ./cmd/k2s ./docker/gen-config
# Usage (on the Linux host):  sudo MOBILE=1 BIG=400 SHORT_ONLY=1 PAD=800 TAG=s7 ./nslab.sh ; ./nslab-analyze.sh .lab/results/s7
#
#   root ns  : router + NAT (UPLINK, default-route iface → internet), local HTTP target on 10.200.1.1:8080
#   ns srv   : k2s  (10.200.1.2, veth s-in/s-out)
#   ns cli   : k2   (10.200.0.2, veth c-in/c-out), socks 1080 + daemon API 1777
#
# Shaping happens on the client's egress (c-in inside ns cli):
#   band 1:1 default (TCP)   band 1:2 all UDP (LOSS/DELAY netem)   band 1:3 fwmark 7 → netem loss 100% (silent drop)
#   fwmark 7 sources: BIG (UDP:443 packets ≥ BIG bytes, with prob BIG_PROB) and/or HASHLIMIT (per-srcport budget)
# SERVER_BLACKHOLE=1 instead drops everything the server sends beyond the lab subnets (egress-dead node).
#
# Knobs (env): BIN (k2 client binary, e.g. an older build as the known-bad control), MOBILE (K2_MOBILE_POWER_SAVING), BIG, BIG_PROB, LOSS, DELAY, JITTER,
#   HASHLIMIT_RATE, HASHLIMIT_BURST, PAD (bytes of X-Pad header), TARGET (http url), TRAFFIC_INTERVAL,
#   BASELINE, SOAK, RECOVERY, PAUSE_AT, RESUME_AT, REMOVE_AT, REINJECT_AT (seconds after injection), TAG,
#   SHORT_ONLY=1 (BIG applies only to QUIC short-header packets: 1-RTT data dies, handshake Initials pass —
#   the "handshake + echo fine, requests time out" fingerprint of #3757)
set -uo pipefail
[[ $EUID -eq 0 ]] || { echo "run with sudo"; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAB="${LAB:-$SCRIPT_DIR/.lab}"; BUILD="${BUILD:-$SCRIPT_DIR/.build}"
BIN="${BIN:-$BUILD/k2}"; K2S="${K2S:-$BUILD/k2s}"; GEN="${GEN:-$BUILD/gen-config}"
UPLINK="${UPLINK:-$(ip route show default | awk '/default/ {print $5; exit}')}"
: "${MOBILE:=1}" "${BIG:=0}" "${BIG_PROB:=1}" "${LOSS:=0}" "${DELAY:=0}" "${JITTER:=0}" "${HASHLIMIT_RATE:=}" "${HASHLIMIT_BURST:=6kb}"
: "${SHORT_ONLY:=0}" "${PAD:=0}" "${TARGET:=http://44.44.44.44:8080/}" "${TRAFFIC_INTERVAL:=0.5}" "${SERVER_BLACKHOLE:=0}"
: "${BASELINE:=30}" "${SOAK:=240}" "${RECOVERY:=90}" "${PAUSE_AT:=0}" "${RESUME_AT:=0}" "${REMOVE_AT:=0}" "${REINJECT_AT:=0}" "${TAG:=run}"
OUT="$LAB/results/$TAG"; mkdir -p "$OUT"; : > "$OUT/driver.log"
[[ -x "$BIN" && -x "$K2S" && -x "$GEN" ]] || { echo "missing binaries: $BIN $K2S $GEN"; exit 1; }
PAD_HDR=""; (( PAD > 0 )) && PAD_HDR=$(head -c "$PAD" /dev/zero | tr '\0' a)
export TZ=UTC
ts() { date -u +%H:%M:%S; }
log() { echo "[$(ts)] $*" | tee -a "$OUT/driver.log"; }
CLI="ip netns exec cli"; SRV="ip netns exec srv"
API="http://127.0.0.1:1777"; SOCKS="127.0.0.1:1080"

teardown() {
  [[ -n "${TRAFFIC_PID:-}" ]] && kill "$TRAFFIC_PID" 2>/dev/null; [[ -n "${POLL_PID:-}" ]] && kill "$POLL_PID" 2>/dev/null
  pkill -f "curl -s -o /dev/null -w" 2>/dev/null
  pkill -f "$BIN run -c" 2>/dev/null; pkill -f "$K2S run -c" 2>/dev/null; pkill -f "http.server 8080" 2>/dev/null
  sleep 1; pkill -9 -f "$BIN run -c" 2>/dev/null; pkill -9 -f "$K2S run -c" 2>/dev/null
  iptables -D FORWARD -s 10.200.1.2 ! -d 10.200.0.0/16 -j DROP 2>/dev/null
  iptables -D INPUT -s 10.200.1.2 -d 44.44.44.44 -j DROP 2>/dev/null
  iptables -t nat -D POSTROUTING -s 10.200.0.0/16 -o "$UPLINK" -j MASQUERADE 2>/dev/null
  ip netns del cli 2>/dev/null; ip netns del srv 2>/dev/null
  cp /var/log/kaitu/k2.log "$OUT/k2.log" 2>/dev/null
  [[ -n "${SUDO_USER:-}" ]] && chown -R "$SUDO_USER" "$LAB"
  log "done; results in $OUT"
}
trap teardown EXIT

setup_topology() {
  ip netns del cli 2>/dev/null; ip netns del srv 2>/dev/null
  for i in 1 2 3 4 5; do ip link del s-out 2>/dev/null; ip link del c-out 2>/dev/null; ip link show s-out >/dev/null 2>&1 || ip link show c-out >/dev/null 2>&1 || break; sleep 1; done
  ip netns add srv; ip netns add cli
  ip link add s-out type veth peer name s-in; ip link set s-in netns srv
  ip link add c-out type veth peer name c-in; ip link set c-in netns cli
  ip addr add 10.200.1.1/24 dev s-out; ip addr add 44.44.44.44/32 dev s-out; ip link set s-out up
  ip addr add 10.200.0.1/24 dev c-out; ip link set c-out up
  $SRV ip addr add 10.200.1.2/24 dev s-in; $SRV ip link set s-in up; $SRV ip link set lo up; $SRV ip route add default via 10.200.1.1
  $CLI ip addr add 10.200.0.2/24 dev c-in; $CLI ip link set c-in up; $CLI ip link set lo up; $CLI ip route add default via 10.200.0.1
  sysctl -qw net.ipv4.ip_forward=1
  iptables -t nat -C POSTROUTING -s 10.200.0.0/16 -o "$UPLINK" -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s 10.200.0.0/16 -o "$UPLINK" -j MASQUERADE
  mkdir -p /etc/netns/srv /etc/netns/cli; echo "nameserver 8.8.8.8" > /etc/netns/srv/resolv.conf; echo "nameserver 8.8.8.8" > /etc/netns/cli/resolv.conf
  $CLI ping -c1 -W2 10.200.1.2 >/dev/null || { log "cli cannot reach srv"; exit 1; }
}

start_traffic() {
  ( n=0; while true; do
      s=$(date +%s.%N); code=$(curl -s -o /dev/null -w '%{http_code}' --socks5-hostname "$SOCKS" --max-time 8 ${PAD_HDR:+-H "X-Pad: $PAD_HDR"} "$TARGET" 2>/dev/null || echo ERR)
      e=$(date +%s.%N); printf '%s %s %.2f\n' "$(date -u +%H:%M:%S)" "$code" "$(echo "$e - $s" | bc)" >> "$OUT/traffic.log"
      sleep "$TRAFFIC_INTERVAL"
    done ) &
  TRAFFIC_PID=$!
}
stop_traffic() { [[ -n "${TRAFFIC_PID:-}" ]] && kill "$TRAFFIC_PID" 2>/dev/null; wait "$TRAFFIC_PID" 2>/dev/null; TRAFFIC_PID=""; }
# run the traffic loop inside ns cli
export -f start_traffic 2>/dev/null

inject() {
  if [[ "$SERVER_BLACKHOLE" == "1" ]]; then
    log "inject: SERVER BLACKHOLE (drop srv egress beyond lab)"
    iptables -I FORWARD -s 10.200.1.2 ! -d 10.200.0.0/16 -j DROP
    iptables -I INPUT -s 10.200.1.2 -d 44.44.44.44 -j DROP
    return
  fi
  log "inject: BIG>=${BIG}B shortOnly=${SHORT_ONLY} p=${BIG_PROB} loss=${LOSS}% delay=${DELAY}ms±${JITTER} hashlimit='${HASHLIMIT_RATE}'"
  $CLI bash -c "
    tc qdisc add dev c-in root handle 1: prio bands 3 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 &&
    tc qdisc add dev c-in parent 1:2 handle 20: netem loss ${LOSS}% delay ${DELAY}ms ${JITTER}ms &&
    tc qdisc add dev c-in parent 1:3 handle 30: netem loss 100% &&
    tc filter add dev c-in protocol ip parent 1:0 prio 1 handle 7 fw flowid 1:3 &&
    tc filter add dev c-in protocol ip parent 1:0 prio 3 u32 match ip protocol 17 0xff flowid 1:2" || { log "tc failed"; exit 1; }
  if (( BIG > 0 )); then
    U32=""; [[ "$SHORT_ONLY" == "1" ]] && U32='-m u32 --u32 0>>22&0x3C@8>>24&0x80=0'
    $CLI iptables -t mangle -A OUTPUT -o c-in -p udp --dport 443 -m length --length "${BIG}:" $U32 -m statistic --mode random --probability "$BIG_PROB" -j MARK --set-mark 7 || { log "iptables BIG failed"; exit 1; }
  fi
  if [[ -n "$HASHLIMIT_RATE" ]]; then
    $CLI iptables -t mangle -A OUTPUT -o c-in -p udp --dport 443 -m hashlimit --hashlimit-name k2pf --hashlimit-mode srcport \
      --hashlimit-above "$HASHLIMIT_RATE" --hashlimit-burst "$HASHLIMIT_BURST" -j MARK --set-mark 7 || { log "iptables hashlimit failed"; exit 1; }
  fi
}
remove() {
  $CLI tc qdisc del dev c-in root 2>/dev/null; $CLI iptables -t mangle -F OUTPUT 2>/dev/null
  iptables -D FORWARD -s 10.200.1.2 ! -d 10.200.0.0/16 -j DROP 2>/dev/null
  iptables -D INPUT -s 10.200.1.2 -d 44.44.44.44 -j DROP 2>/dev/null
  log "shaping removed"
}

log "start: BIN=$BIN MOBILE=$MOBILE BIG=$BIG shortOnly=$SHORT_ONLY p=$BIG_PROB LOSS=$LOSS DELAY=$DELAY HASHLIMIT=$HASHLIMIT_RATE BLACKHOLE=$SERVER_BLACKHOLE PAD=$PAD TARGET=$TARGET INTERVAL=$TRAFFIC_INTERVAL BASELINE=$BASELINE SOAK=$SOAK PAUSE=$PAUSE_AT RESUME=$RESUME_AT REMOVE_AT=$REMOVE_AT REINJECT_AT=$REINJECT_AT"
"$BIN" version 2>/dev/null | head -1 | tee -a "$OUT/driver.log"
setup_topology
mkdir -p "$LAB/etc" "$LAB/www" /var/log/kaitu; : > /var/log/kaitu/k2.log
echo "hello" > "$LAB/www/index.html"
(cd "$LAB/www" && exec python3 -m http.server 8080 --bind 44.44.44.44 >/dev/null 2>&1) & HTTP_PID=$!
"$GEN" -out "$LAB/etc" -host 10.200.1.2 -port 443 -listen 0.0.0.0:1777 >"$OUT/gen.log" 2>&1 || { log "gen-config failed"; exit 1; }
$SRV "$K2S" run -c "$LAB/etc/server.yaml" >"$OUT/k2s.log" 2>&1 </dev/null & K2S_PID=$!
for i in $(seq 1 20); do $SRV ss -ulnp 2>/dev/null | grep -q ':443 ' && break; sleep 1; done
$SRV ss -ulnp 2>/dev/null | grep -q ':443 ' || { log "k2s not listening"; cat "$OUT/k2s.log" | tail -20; exit 1; }
log "k2s listening"
$CLI env K2_MOBILE_POWER_SAVING="$MOBILE" TZ=UTC "$BIN" run -c "$LAB/etc/client.yaml" >"$OUT/k2-stderr.log" 2>&1 </dev/null & K2_PID=$!
for i in $(seq 1 30); do $CLI curl -sf "$API/ping" >/dev/null 2>&1 && break; sleep 1; done
$CLI curl -sf "$API/ping" >/dev/null || { log "daemon unreachable"; tail -20 "$OUT/k2-stderr.log"; exit 1; }
for attempt in 1 2 3; do
  $CLI "$BIN" up "$LAB/etc/client.yaml" >>"$OUT/ctl-up.log" 2>&1 &
  for i in $(seq 1 40); do
    st=$($CLI curl -sf -X POST "$API/api/core" -H 'Content-Type: application/json' -d '{"action":"status"}' 2>/dev/null | jq -r '.data.state // empty')
    [[ "$st" == "connected" ]] && break 2; sleep 1
  done
  log "up attempt $attempt ended in state=$st; retrying"
done
[[ "$st" == "connected" ]] || { log "not connected: $st"; tail -30 "$OUT/k2-stderr.log"; exit 1; }
log "connected"
( while true; do
    $CLI curl -sf "$API/api/stats" 2>/dev/null | jq -c '.data[-1] | [.ts, .health, .transport, .wire_fallback, .active_channel, .score_quic, .score_tcpws, .packet_loss_rate]' >> "$OUT/stats.jsonl" 2>/dev/null
    sleep 1
  done ) & POLL_PID=$!

# traffic loop must run inside ns cli: wrap start_traffic
start_traffic_ns() { OUT="$OUT" SOCKS="$SOCKS" TARGET="$TARGET" PAD_HDR="$PAD_HDR" TRAFFIC_INTERVAL="$TRAFFIC_INTERVAL" $CLI bash -c "$(declare -f start_traffic); start_traffic; wait" >/dev/null 2>&1 </dev/null & TRAFFIC_PID=$!; }
stop_traffic_ns() { [[ -n "${TRAFFIC_PID:-}" ]] && pkill -P "$TRAFFIC_PID" 2>/dev/null; kill "$TRAFFIC_PID" 2>/dev/null; wait "$TRAFFIC_PID" 2>/dev/null; TRAFFIC_PID=""; pkill -f "curl -s -o /dev/null -w" 2>/dev/null; }
start_traffic_ns
log "traffic started; baseline ${BASELINE}s"; sleep "$BASELINE"
inject; T_INJECT=$(ts); log "injected at $T_INJECT; soak ${SOAK}s"
elapsed=0
while (( elapsed < SOAK )); do
  sleep 1; elapsed=$((elapsed+1))
  (( PAUSE_AT > 0 && elapsed == PAUSE_AT )) && { stop_traffic_ns; log "traffic PAUSED (t=+${elapsed}s)"; }
  (( RESUME_AT > 0 && elapsed == RESUME_AT )) && { start_traffic_ns; log "traffic RESUMED (t=+${elapsed}s)"; }
  (( REMOVE_AT > 0 && elapsed == REMOVE_AT )) && { remove; log "shaping REMOVED mid-soak (t=+${elapsed}s)"; }
  (( REINJECT_AT > 0 && elapsed == REINJECT_AT )) && { inject; log "shaping RE-INJECTED (t=+${elapsed}s)"; }
done
remove; T_REMOVE=$(ts); log "removed at $T_REMOVE; recovery ${RECOVERY}s"
[[ -z "${TRAFFIC_PID:-}" ]] && start_traffic_ns
sleep "$RECOVERY"
log "finished (inject=$T_INJECT remove=$T_REMOVE)"
