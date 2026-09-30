#!/bin/sh
# k2r service lifecycle on a real OpenWrt router (procd, busybox, nft).
#
# Run ON the router, as root, with k2r installed as a service and connected
# (`k2r setup <URL>` done, `k2r status` says connected). It stops, kills,
# restarts and uninstalls the service and checks what each leaves behind.
# Exit 0 = all pass. The service is reinstalled and reconnected at the end.
#
#   ssh root@router 'cat > /tmp/router-check.sh' < test/k2r-lifecycle/router-check.sh
#   ssh root@router sh /tmp/router-check.sh
#
# (OpenWrt ships no sftp-server, so a plain scp fails.)
#
# NODE_IP=<address of the connected node> adds step 8, which makes the node go
# silent for 45 s. Without it the step is skipped.
#
# Not covered, because it cannot be timed from outside: a connect that reaches
# Install after shutdown has removed the rules (gateway's installGate). Step 5
# passes with or without the gate.
#
# The Docker UAT cannot see any of this: no procd, no SIGTERM from a service
# manager, no busybox `ip`.

LOG=/var/log/kaitu/k2r.log
STATE=/etc/k2r/state.json
DEAD='k2v5://u:t@192.0.2.1:443/?ip=192.0.2.1' # TEST-NET-1: never answers
RECOVERED='primary transport replaced|engine: connected'
fails=0

pass() { echo "  ok    $1"; }
fail() { echo "  FAIL  $1"; fails=$((fails + 1)); }
check() { if eval "$2"; then pass "$1"; else fail "$1"; fi; }

table() { nft list tables 2>/dev/null | grep -c 'inet k2r'; }
fwmarks() { ip rule | grep -c 'fwmark 0x1 '; }
routes100() { ip route show table 100 2>/dev/null | wc -l; }
state() { k2r status 2>/dev/null | sed -n 's/.*"state": *"\([a-z]*\)".*/\1/p'; }
resolves() { nslookup openwrt.org 127.0.0.1 >/dev/null 2>&1; }
wait_state() { # <state> <seconds>
	i=0
	while [ "$i" -lt "$2" ]; do
		[ "$(state)" = "$1" ] && return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}
wait_gone() { # <pid> <seconds>
	i=0
	while [ "$i" -lt "$2" ]; do
		[ -d "/proc/$1" ] || return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}
logged() { grep -qE "$1" "$LOG"; }
wait_logged() { # <pattern> <seconds>
	i=0
	while [ "$i" -lt "$2" ]; do
		logged "$1" && return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}
clean() { [ "$(table)" = 0 ] && [ "$(fwmarks)" = 0 ] && [ "$(routes100)" = 0 ]; }

[ "$(state)" = connected ] || { echo "k2r must be connected before this check"; exit 2; }
cp "$STATE" /tmp/k2r-state.good

echo "1. stop removes the rules, keeps the saved session"
pid=$(pidof k2r)
/etc/init.d/k2r stop
check "daemon exits within procd's 5 s" "wait_gone $pid 5"
check "no inet k2r table, no fwmark rule, no table-100 route" clean
check "the router resolves names" resolves
check "state.json still says connected" "grep -q '\"state\": *\"connected\"' $STATE"

echo "2. start reconnects from the saved session"
/etc/init.d/k2r start
check "connected again" "wait_state connected 25"
check "exactly one fwmark rule" '[ "$(fwmarks)" = 1 ]'

echo "3. a killed daemon hands its rules to the respawned one"
kill -9 "$(pidof k2r)"
sleep 1
check "rules still in place right after the kill" '[ "$(table)" = 1 ]'
sleep 8 # procd respawns after 5 s; the new daemon needs the API up before wait_state can see it
check "respawned and connected" "wait_state connected 25"
check "fwmark rules did not stack" '[ "$(fwmarks)" = 1 ]'

echo "4. restart"
/etc/init.d/k2r restart
sleep 2
check "connected after restart" "wait_state connected 25"
check "one daemon, one fwmark rule" '[ "$(pidof k2r | wc -w)" = 1 ] && [ "$(fwmarks)" = 1 ]'

echo "5. stop right after a connect was issued"
k2r up >/dev/null 2>&1 # returns at once; the daemon is tearing down and re-dialing
pid=$(pidof k2r)
/etc/init.d/k2r stop
check "daemon exits within procd's 5 s" "wait_gone $pid 5"
check "nothing left behind" clean

echo "6. auto-reconnect stands down once the user connects"
sed "s#\"via\": *\"[^\"]*\"#\"via\": \"$DEAD\"#" /tmp/k2r-state.good >"$STATE"
: >"$LOG"
/etc/init.d/k2r start
i=0
while [ "$i" -lt 30 ] && ! grep -q 'auto-reconnect failed' "$LOG"; do
	sleep 1
	i=$((i + 1))
done
check "the saved config fails to connect" "grep -q 'auto-reconnect failed' $LOG"
k2r up >/dev/null 2>&1 # k2r.yml still holds the working config
check "the user's connect succeeds" "wait_state connected 25"
sleep 40 # retries 2 and 3 would fire 15 s and 45 s after the first failure
check "still connected 40 s later" '[ "$(state)" = connected ]'
check "rules still in place" '[ "$(table)" = 1 ]'
check "the loop stood down" "grep -q 'auto-reconnect stands down' $LOG"

echo "7. a saved session older than a week still reconnects"
/etc/init.d/k2r stop
sed -i 's/"timestamp": *"[^"]*"/"timestamp": "2020-01-01T00:00:00Z"/' "$STATE"
/etc/init.d/k2r start
check "connected from a session saved in 2020" "wait_state connected 25"

echo "8. the node goes silent on an idle router, then answers again"
if [ -n "$NODE_IP" ]; then
	[ "$(state)" = connected ] || { k2r up >/dev/null 2>&1; wait_state connected 25; }
	: >"$LOG"
	nft add table inet k2rcheck
	nft add chain inet k2rcheck in '{ type filter hook input priority -350 ; }'
	nft add rule inet k2rcheck in ip saddr "$NODE_IP" drop
	sleep 45 # two failed re-races, ~25 s, used to end the engine for good
	nft delete table inet k2rcheck
	# `state` alone proves nothing here: an engine that keeps re-racing the
	# same server reports connected throughout. The log line is written when
	# a transport is actually established again.
	check "a transport is re-established within 90 s of the node answering" "wait_logged '$RECOVERED' 90"
	check "connected" "wait_state connected 30"
	check "rules in place" '[ "$(table)" = 1 ] && [ "$(fwmarks)" = 1 ]'
else
	echo "  skip  NODE_IP not set"
fi

echo "9. uninstall removes the service and the rules"
exe=$(readlink -f "$(command -v k2r)")
pid=$(pidof k2r)
k2r service uninstall >/dev/null 2>&1
check "no daemon" "wait_gone ${pid:-0} 8"
check "nothing left behind" clean
check "the router resolves names" resolves

"$exe" service install >/dev/null 2>&1
"$exe" up >/dev/null 2>&1
wait_state connected 25 || echo "  (note: not reconnected after reinstall)"
rm -f /tmp/k2r-state.good

echo
if [ "$fails" = 0 ]; then echo "ALL PASS"; else echo "$fails FAILED"; fi
exit "$fails"
