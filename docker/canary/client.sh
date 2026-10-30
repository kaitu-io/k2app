#!/bin/bash
# client.sh — a private k2 client daemon on this Mac, pointed at the canary, in
# proxy mode (SOCKS5 127.0.0.1:10805). Never touches the installed Kaitu/开途
# app: that daemon is root on 127.0.0.1:1777 (invisible to a non-root lsof), and
# `k2 up` / any default-port call would reconfigure it. Everything here talks to
# 127.0.0.1:$API_PORT only, and `stop` checks the app daemon's state is unchanged.
#
#   docker/canary/client.sh start <k2 binary>   # any k2 build, old or new
#   docker/canary/client.sh stop
set -euo pipefail
API_PORT="${API_PORT:-17777}"; SOCKS="${SOCKS:-127.0.0.1:10805}"
WORK="${CANARY_WORK:-$HOME/.cache/k2s-canary}"
URL_FILE="${CANARY_URL_FILE:-$HOME/.config/k2s-canary/connect-url}"
API="http://127.0.0.1:$API_PORT/api/core"
app_state() { curl -s -m 2 -X POST http://127.0.0.1:1777/api/core -d '{"action":"status"}' | python3 -c 'import sys,json
try: print(json.load(sys.stdin).get("data",{}).get("state","?"))
except Exception: print("absent")'; }
mkdir -p "$WORK"; chmod 700 "$WORK"
case "${1:?start|stop}" in
start)
  BIN="${2:?k2 binary}"
  curl -s -m 1 "http://127.0.0.1:$API_PORT/" >/dev/null 2>&1 && { echo "FAIL: 127.0.0.1:$API_PORT already in use"; exit 2; }
  app_state > "$WORK/app-state-before"
  printf 'listen: "127.0.0.1:%s"\nlog:\n  level: info\n  output: "%s/k2-daemon.log"\n' "$API_PORT" "$WORK" > "$WORK/daemon.yml"
  "$BIN" run -c "$WORK/daemon.yml" > "$WORK/daemon.out" 2>&1 & echo $! > "$WORK/daemon.pid"
  for i in $(seq 1 50); do curl -s -m 1 -X POST "$API" -d '{"action":"status"}' >/dev/null 2>&1 && break; sleep 0.2; done
  kill -0 "$(cat "$WORK/daemon.pid")" 2>/dev/null || { echo "FAIL: daemon exited"; cat "$WORK/daemon.out"; exit 3; }
  ( umask 077; python3 - "$URL_FILE" "$SOCKS" > "$WORK/up.json" <<'PY'
import json, sys
url = open(sys.argv[1]).read().strip()
print(json.dumps({"action": "up", "params": {"config": {
    "routes": [{"via": url, "match": {"all": True}}],
    "mode": "proxy", "proxy": {"listen": sys.argv[2]}}}}))
PY
  )
  curl -s -m 30 -X POST "$API" --data-binary @"$WORK/up.json" | head -c 200; echo
  for i in $(seq 1 50); do
    [ "$(curl -s -m 5 -o /dev/null -w '%{http_code}' --socks5-hostname "$SOCKS" http://www.gstatic.com/generate_204)" = 204 ] && { echo "client up ($("$BIN" version 2>&1 | head -1)), app daemon: $(cat "$WORK/app-state-before")"; exit 0; }
    sleep 0.5
  done
  echo "FAIL: no traffic through $SOCKS"; exit 4 ;;
stop)
  curl -s -m 10 -X POST "$API" -d '{"action":"down"}' >/dev/null 2>&1 || true
  [ -f "$WORK/daemon.pid" ] && kill "$(cat "$WORK/daemon.pid")" 2>/dev/null || true
  rm -f "$WORK/daemon.pid"
  NOW=$(app_state); BEFORE=$(cat "$WORK/app-state-before" 2>/dev/null || echo "?")
  echo "test client stopped; app daemon before=$BEFORE now=$NOW"
  [ "$NOW" = "$BEFORE" ] || { echo "WARN: app daemon state changed"; exit 5; } ;;
esac
