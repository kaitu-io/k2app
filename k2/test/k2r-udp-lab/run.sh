#!/usr/bin/env bash
# k2r UDP data-plane lab — native Linux, root, real TPROXY.
#
# The Docker UAT cannot test this: LinuxKit has no UDP TPROXY, so UDP never
# reaches the gateway's flow code there. This script builds the whole path in
# network namespaces on one Linux host:
#
#   netns lan (10.30.0.2) ──veth── root ns = router, runs k2r ──veth── netns srv (10.40.0.2)
#                                                                      k2s :443
#                                                                      44.44.44.44 / .45 on a dummy
#                                                                      interface: UDP echo, HTTP
#
# 44.44.44.44 is reachable from the LAN client only through the tunnel. The
# echo server reports the source address it saw, which is how the egress port
# of each packet is observed.
#
# Usage:   BIN=/path/to/k2r K2S=/path/to/k2s GENCONFIG=/path/to/gen-config ./run.sh
# Needs:   root, nft_tproxy, python3, curl, tc (netem). Optional: iperf3.
#          A throwaway host: k2r takes over the host's DNS while it runs, and
#          /etc/k2r/state.json is set aside for the duration.
# Exit:    non-zero when an assertion fails.
#
# Link delay matters: on a zero-latency veth the channel scoreboard sees RTT
# inflation under load and flips QUIC→TCP-WS, which looks like a regression and
# is not. RTT_MS (default 30) puts a realistic delay on the server link.
set -uo pipefail

: "${BIN:?path to the k2r binary under test}"
: "${K2S:?path to k2s}"
: "${GENCONFIG:?path to gen-config}"
: "${RTT_MS:=30}"
: "${FLOWS:=20}" "${PPS:=100}" "${SECS:=10}"
HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="${WORK:-/tmp/k2r-udp-lab}"
LOG="$WORK/k2r.log"
FAIL=0

lan() { ip netns exec lan "$@"; }
ok()   { echo "  PASS  $*"; }
bad()  { echo "  FAIL  $*"; FAIL=$((FAIL + 1)); }
check() { local name=$1 got=$2 want=$3; if [[ "$got" == "$want" ]]; then ok "$name ($got)"; else bad "$name: got '$got', want '$want'"; fi; }
field() { sed -n "s/.*$1=\([^ ]*\).*/\1/p" | head -1; }

cleanup() {
    pkill -f "^$BIN run" 2>/dev/null
    pkill -f "$HERE/echo.py" 2>/dev/null
    [[ -n "${K2S_PID:-}" ]] && kill "$K2S_PID" 2>/dev/null
    [[ -n "${HTTP_PID:-}" ]] && kill "$HTTP_PID" 2>/dev/null
    sleep 1
    nft delete table inet k2r 2>/dev/null
    while ip rule del fwmark 1 table 100 2>/dev/null; do :; done
    ip netns del lan 2>/dev/null; ip netns del srv 2>/dev/null
    ip link del vlan0 2>/dev/null; ip link del vsrv0 2>/dev/null
    if [[ -f /etc/k2r/state.json.udp-lab ]]; then mv -f /etc/k2r/state.json.udp-lab /etc/k2r/state.json; fi
}
trap cleanup EXIT
cleanup; mkdir -p "$WORK"

# ── topology ─────────────────────────────────────────────────────
ip netns add lan; ip netns add srv
ip link add vlan0 type veth peer name eth0 netns lan
ip link add vsrv0 type veth peer name eth0 netns srv
ip addr add 10.30.0.1/24 dev vlan0; ip link set vlan0 up
ip addr add 10.40.0.1/24 dev vsrv0; ip link set vsrv0 up
ip -n lan addr add 10.30.0.2/24 dev eth0; ip -n lan link set eth0 up; ip -n lan link set lo up
ip -n lan route add default via 10.30.0.1
ip -n srv addr add 10.40.0.2/24 dev eth0; ip -n srv link set eth0 up; ip -n srv link set lo up
ip -n srv link add dum0 type dummy; ip -n srv link set dum0 up
ip -n srv addr add 44.44.44.44/32 dev dum0; ip -n srv addr add 44.44.44.45/32 dev dum0
sysctl -qw net.ipv4.ip_forward=1
modprobe nft_tproxy 2>/dev/null || true
half=$((RTT_MS / 2))
tc qdisc replace dev vsrv0 root netem delay "${half}ms"
ip netns exec srv tc qdisc replace dev eth0 root netem delay "${half}ms"

# ── services ─────────────────────────────────────────────────────
"$GENCONFIG" -out="$WORK" -host=10.40.0.2 -port=443 -mode=gateway -listen=127.0.0.1:1779 >/dev/null
sed -i 's/level: debug/level: info/' "$WORK/k2r.yml" "$WORK/server.yaml"
printf 'lan_subnets:\n  - "10.30.0.0/24"\n' >> "$WORK/k2r.yml"
ip netns exec srv "$K2S" run -c "$WORK/server.yaml" >"$WORK/k2s.log" 2>&1 & K2S_PID=$!
ip netns exec srv python3 -u "$HERE/echo.py" >"$WORK/echo.log" 2>&1 &
ip netns exec srv python3 -m http.server 8080 --bind 44.44.44.44 -d "$WORK" >/dev/null 2>&1 & HTTP_PID=$!
sleep 1
# k2r auto-reconnects from /etc/k2r/state.json 3 s after start and keeps
# retrying. A state left by an earlier run carries that run's server keys:
# every retry tears down the session `up` just built. Set it aside for the run.
[[ -f /etc/k2r/state.json ]] && mv /etc/k2r/state.json /etc/k2r/state.json.udp-lab
"$BIN" run -c "$WORK/k2r.yml" >"$LOG" 2>&1 &
sleep 2
"$BIN" up "$WORK/k2r.yml" >/dev/null 2>&1
for _ in $(seq 1 30); do "$BIN" status 2>/dev/null | grep -q '"state": "connected"' && break; sleep 1; done
"$BIN" status 2>/dev/null | grep -q '"state": "connected"' || { bad "k2r did not connect"; tail -20 "$LOG"; exit 1; }
sleep 5
K2R_PID=$(pgrep -f "^$BIN run")
tcp() { lan curl -s -o /dev/null -m 5 -w '%{http_code}' http://44.44.44.44:8080/k2r.yml; }

# ── assertions ───────────────────────────────────────────────────
echo "1. one client socket, 5 packets → one egress port"
check "distinct egress ports" "$(lan python3 "$HERE/client.py" conn 5 0.2 44.44.44.44:9000 | grep SUMMARY | field distinct_server_seen_src)" 1

echo "2. one unconnected socket, two destinations → one stable egress port each, replies from the right peer"
out=$(lan python3 "$HERE/client.py" unconn 6 0.2 44.44.44.44:9000 44.44.44.45:9000)
check "replies received" "$(echo "$out" | grep SUMMARY | field ok)" 6
check "distinct egress endpoints" "$(echo "$out" | grep SUMMARY | field distinct_server_seen_src)" 2
check "replies from the wrong source" "$(echo "$out" | grep -c -v -E "^[0-9]+ -> \('([0-9.]+)', ([0-9]+)\) reply_from=\('\1', \2\)|SUMMARY|^\[")" 0

echo "3. exchanges where the client has the last word (QUIC close, final ACK)"
for _ in 1 2 3 4; do lan python3 "$HERE/client.py" conn 2 0.5 44.44.44.44:9002 >/dev/null; done
ok "4 exchanges sent"

echo "4. $FLOWS flows x $PPS pps x $SECS s"
pids=(); for i in $(seq 1 "$FLOWS"); do lan python3 "$HERE/flood.py" "$PPS" "$SECS" 44.44.44.44:9000 >"$WORK/flood-$i.out" & pids+=($!); done
wait "${pids[@]}"
loss=$(cat "$WORK"/flood-*.out | awk '{split($2,a,"=");split($3,b,"=");s+=a[2];r+=b[2]} END {printf "%d",(1-r/s)*1000}')
# Not a tight bound on purpose: with this many flows sending at once the wire
# layer's send budget (maxDatagramInflight) sheds a few percent, and how many
# depends on the host. A session per packet loses more than half.
if (( loss <= 100 )); then ok "loss ${loss}‰ (≤ 100‰)"; else bad "loss ${loss}‰ (> 100‰)"; fi
check "TCP right after the UDP load" "$(tcp)" 200
rss=$(awk '/VmHWM/ {print $2}' "/proc/$K2R_PID/status")
if (( rss < 80000 )); then ok "peak RSS ${rss} kB (< 80 MB)"; else bad "peak RSS ${rss} kB — a session per packet?"; fi

if command -v iperf3 >/dev/null; then
    echo "5. iperf3 UDP 5 Mbit/s, both directions"
    ip netns exec srv iperf3 -s -B 44.44.44.44 -p 5201 -D >/dev/null 2>&1
    sleep 1
    for dir in "" "-R"; do
        line=$(lan timeout 30 iperf3 -c 44.44.44.44 -p 5201 -u -b 5M -t 6 -l 1200 $dir 2>&1 | grep receiver)
        if [[ "$line" =~ ([0-9.]+)\ Mbits/sec ]] && (( ${BASH_REMATCH[1]%.*} >= 4 )); then ok "iperf3 ${dir:-up}: ${BASH_REMATCH[1]} Mbit/s"; else bad "iperf3 ${dir:-up}: ${line:-no result}"; fi
    done
    pkill iperf3
fi

echo "6. after the idle deadline (70 s)"
sleep 70
# Only the lab's own targets: the LAN namespace also emits mDNS to 224.0.0.251,
# which TPROXY picks up and which nothing ever answers — a genuine unanswered
# session, correctly counted.
check "answered sessions counted as relay timeouts" "$(grep -a 'quicUDPConn: ReadFrom timeout' "$LOG" | grep -c 'target=44.44.44.')" 0
check "transport switches" "$(grep -a -c 'DIAG: transport-switch' "$LOG")" 0
check "replies that fell back to the listener socket" "$(grep -a -c 'transparent reply socket unavailable' "$LOG")" 0
sessions=$(grep -a -c 'UDPConn: closed' "$LOG")
if (( sessions <= FLOWS + 20 )); then ok "tunnel sessions opened: $sessions"; else bad "tunnel sessions opened: $sessions for ~$((FLOWS + 9)) flows"; fi
check "UDP after idle" "$(lan python3 "$HERE/client.py" conn 3 0.2 44.44.44.44:9000 | grep SUMMARY | field ok)" 3
check "TCP after idle" "$(tcp)" 200

echo
if (( FAIL == 0 )); then echo "ALL PASS — log: $LOG"; else echo "$FAIL FAILED — log: $LOG"; fi
exit $(( FAIL > 0 ))
