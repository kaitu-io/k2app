#!/usr/bin/env bash
# Shared helpers for k2subs UAT scripts.
# All scripts source this. Daemon assumed on :11777.

set -euo pipefail

export DAEMON_URL="http://127.0.0.1:11777"
export K2LOG="/var/log/kaitu/k2.log"

api() {
  # api <action> [<params-json>]
  local action="$1"; shift
  local params="${1:-}"
  if [ -n "$params" ]; then
    curl -sS -X POST "$DAEMON_URL/api/core" -H 'Content-Type: application/json' \
      -d "{\"action\":\"$action\",\"params\":$params}"
  else
    curl -sS -X POST "$DAEMON_URL/api/core" -H 'Content-Type: application/json' \
      -d "{\"action\":\"$action\"}"
  fi
}

status_brief() {
  api status | python3 -c '
import json, sys
d = json.load(sys.stdin)["data"]
print("  state=", d.get("state"), "uptime=", d.get("uptimeSeconds"), "err=", d.get("lastError"))
'
}

CREDS_FILE="/tmp/k2subs-uat-creds.env"

extract_creds() {
  # Cache creds to file so tests continue to work after daemon enters disconnected state.
  if [ -s "$CREDS_FILE" ]; then
    . "$CREDS_FILE"
    return 0
  fi
  local parsed
  parsed=$(api status | python3 -c '
import json, sys, re
d = json.load(sys.stdin)["data"]
via = (d.get("config") or {}).get("routes", [{}])[0].get("via","")
m = re.match(r"k2subs://([^:]+):([^@]+)@(.+)", via) or re.match(r"k2v5://([^:]+):([^@]+)@(.+)", via)
if not m:
    print("")
else:
    print("export UDID="+m.group(1))
    print("export TOKEN="+m.group(2))
')
  if [ -z "$parsed" ]; then
    echo "extract_creds: daemon has no active config — connect once before running UAT." >&2
    return 1
  fi
  echo "$parsed" > "$CREDS_FILE"
  . "$CREDS_FILE"
}

make_up_payload() {
  # stdout = JSON params for up action
  local via="$1"
  VIA="$via" python3 -c "
import json, os
print(json.dumps({'config':{
  'mode':'tun',
  'tun':{'ipv4':'198.18.0.7/15','ipv6':'fdfe:dcba:9876::7/64'},
  'dns':{'direct':['114.114.114.114:53','223.5.5.5:53'],'proxy':['8.8.8.8:53','1.1.1.1:53']},
  'routes':[{'via':os.environ['VIA'],'match':{'all':True}}],
  'log':{'level':'debug'}
}}))"
}

do_up() {
  local via="$1"
  local p
  p=$(make_up_payload "$via")
  curl -sS -X POST "$DAEMON_URL/api/core" -H 'Content-Type: application/json' \
    -d "{\"action\":\"up\",\"params\":$p}"
}

do_down() {
  api down > /dev/null
}

wait_poll() {
  # wait_poll <iterations> — each iter ~= 1s (one status curl)
  local n="${1:-6}"
  local i
  for ((i=0; i<n; i++)); do
    curl -sS --max-time 1 -X POST "$DAEMON_URL/api/core" \
      -H 'Content-Type: application/json' -d '{"action":"status"}' -o /dev/null || true
  done
}

truncate_log() {
  sudo -n truncate -s 0 "$K2LOG"
}

grep_log() {
  # grep_log <pattern> [max]
  local pat="$1"; local max="${2:-10}"
  sudo -n grep -E "$pat" "$K2LOG" | head -n "$max" || true
}

goroutine_count() {
  curl -sS "$DAEMON_URL/metrics" 2>/dev/null | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
    data = d.get("data", d)
    print(data.get("goroutines", 0))
except Exception:
    print(0)
' 2>/dev/null || echo 0
}

print_banner() {
  echo
  echo "================================================================"
  echo "=== $1"
  echo "================================================================"
}
