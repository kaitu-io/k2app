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


# corrupt_jwt_sig <token> — return a JWT whose signature is byte-level
# different from the input.
#
# WHY this exists: the signature section of a JWT is base64url-encoded. For
# HS256 the raw signature is exactly 32 bytes = 256 bits, but base64url with
# no padding encodes it as 43 characters = 258 bits. The trailing 2 bits are
# slack — the low 2 bits of the last base64 character are discarded at
# decode time. Four different last-characters therefore decode to the same
# 32 signature bytes:
#   index 0..3   → "A","B","C","D"  (top 4 bits 0000, low 2 bits dropped)
#   index 4..7   → "E","F","G","H"  …
#   …
# A naïve test that just flips `A`→`B` on the final character leaves the
# signature UNCHANGED post-decode, and the server happily accepts the
# token. We observed this during F3 UAT design — that's why this helper
# exists and why T07 MUST use it.
#
# Strategy: flip a single character in the MIDDLE third of the signature
# section. That guarantees the flipped char is part of a fully-materialized
# base64 group (not the trailing slack bits), so the decoded signature
# changes deterministically.
corrupt_jwt_sig() {
  local tok="$1"
  TOK="$tok" python3 - <<'PY'
import os, sys, base64

tok = os.environ["TOK"]
parts = tok.split(".")
if len(parts) != 3:
    print("ERR: not a 3-part JWT", file=sys.stderr)
    sys.exit(1)

alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

def b64url_decode(s: str) -> bytes:
    pad = "=" * (-len(s) % 4)
    return base64.urlsafe_b64decode(s + pad)

sig = parts[2]
if len(sig) < 6:
    print("ERR: signature too short", file=sys.stderr)
    sys.exit(1)

# Pick a stable middle index. Middle of the signature is far from both ends
# and therefore far from the trailing-slack bits.
idx = len(sig) // 2
orig_char = sig[idx]
# Pick a replacement that is NOT in the same 4-char group (index // 4 differs).
orig_i = alphabet.index(orig_char)
for cand_i in range(64):
    if cand_i // 4 != orig_i // 4 and alphabet[cand_i] != orig_char:
        new_char = alphabet[cand_i]
        break
else:
    print("ERR: no replacement found", file=sys.stderr)
    sys.exit(1)

new_sig = sig[:idx] + new_char + sig[idx+1:]
new_tok = parts[0] + "." + parts[1] + "." + new_sig

# Self-check: decoded signature must actually differ.
before = b64url_decode(sig)
after  = b64url_decode(new_sig)
if before == after:
    print("ERR: corruption left signature bytes unchanged (should be impossible)", file=sys.stderr)
    sys.exit(2)

print(new_tok)
PY
}

print_banner() {
  echo
  echo "================================================================"
  echo "=== $1"
  echo "================================================================"
}
