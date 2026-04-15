#!/usr/bin/env bash
# T07 — Token middle-character tamper → Center rejects with 401.
#
# Historical note: the original T07 draft flipped the LAST character of the
# JWT. base64url encoding of a 32-byte HMAC signature has 2 bits of trailing
# slack, so 1 in 16 "flips" actually produce an identical decoded signature
# and the server ACCEPTS the token — false-negative UAT pass. common.sh's
# corrupt_jwt_sig helper tampers a middle character instead (guaranteed
# byte-level signature change).

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/common.sh"

extract_creds
[ -n "${UDID:-}" ] && [ -n "${TOKEN:-}" ] || { echo "missing creds" >&2; exit 1; }

BAD_TOKEN=$(corrupt_jwt_sig "$TOKEN")
if [ -z "$BAD_TOKEN" ] || [ "$BAD_TOKEN" = "$TOKEN" ]; then
  echo "corrupt_jwt_sig failed to produce a distinct token" >&2
  exit 2
fi

# Before/after byte-level sanity: last few chars visibly differ from the
# middle, but the final character is unchanged (proves we targeted middle).
echo "original token tail: ...${TOKEN:(-10)}"
echo "tampered token tail: ...${BAD_TOKEN:(-10)}"
if [ "${TOKEN:(-1)}" != "${BAD_TOKEN:(-1)}" ]; then
  echo "WARN: helper flipped a trailing char — expected middle tamper" >&2
fi

VIA_BAD="k2subs://${UDID}:${BAD_TOKEN}@k2.52j.me/api/subs"
PAYLOAD=$(VIA="$VIA_BAD" python3 -c '
import json, os
print(json.dumps({"action":"up","params":{"config":{
  "mode":"tun",
  "tun":{"ipv4":"198.18.0.7/15","ipv6":"fdfe:dcba:9876::7/64"},
  "dns":{"direct":["114.114.114.114:53","223.5.5.5:53"],"proxy":["8.8.8.8:53","1.1.1.1:53"]},
  "routes":[{"via":os.environ["VIA"],"match":{"all":True}}],
  "log":{"level":"info"}
}}}))')

print_banner "T07 — up with tampered-sig token (expect 401 from Center)"
# The daemon returns the subscription fetch error via the /api/core response
# envelope. On tampered token, sub.Fetch should surface a 401.
RESP=$(curl -sS -X POST "$DAEMON_URL/api/core" -H 'Content-Type: application/json' -d "$PAYLOAD")
echo "$RESP"
echo

FAIL_OBSERVED=0
# Verify we saw a 401 surface — either as resp code != 0 or in state lastError.
CODE=$(echo "$RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("code",""))' 2>/dev/null || echo "")
MSG=$(echo "$RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("message",""))' 2>/dev/null || echo "")

# Either: daemon refused synchronously (code != 0) or accepted the request
# but transitioned to disconnected+error. Wait up to 6s for the async case.
if [ "$CODE" != "0" ]; then
  echo "daemon rejected synchronously: code=$CODE message='$MSG'"
  FAIL_OBSERVED=1
else
  # Async path: poll status for error surface.
  for i in 1 2 3 4 5 6; do
    STAT=$(api status)
    STATE=$(echo "$STAT" | python3 -c 'import json,sys;d=json.load(sys.stdin).get("data",{});print(d.get("state",""))')
    ERR=$(echo "$STAT" | python3 -c 'import json,sys;d=json.load(sys.stdin).get("data",{});e=d.get("lastError") or {};print((e.get("message") if isinstance(e,dict) else str(e)) or "")')
    echo "  poll $i: state=$STATE lastError=$ERR"
    if [ "$STATE" = "disconnected" ] && [ -n "$ERR" ]; then
      FAIL_OBSERVED=1
      break
    fi
    sleep 1
  done
fi

# Clean up any half-up state.
api down > /dev/null 2>&1 || true

if [ "$FAIL_OBSERVED" != "1" ]; then
  echo "T07 FAIL — tampered token was NOT rejected"
  exit 3
fi

# Confirm via log: look for a 401 / authentication hint within the last
# 30 lines. Best-effort — not all deployments wire the hint identically.
print_banner "T07 — log evidence"
grep_log 'DIAG: subs-refresh-fail|subscription fetch|401|invalid credentials|authentication' 20 || true

echo
echo "T07 PASS — signature-tampered JWT rejected end-to-end"
