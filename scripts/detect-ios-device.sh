#!/usr/bin/env bash
# Print the hardware UDID of the best connected physical iPhone for `cap run ios`.
#
# Why this exists: `xcrun xctrace list devices | head -1` happily returns a
# paired-but-offline iPhone (e.g. a second device on another iOS version), and
# Capacitor then rejects it with "Invalid target ID". devicectl knows which
# devices are actually reachable via connectionProperties.tunnelState, so we
# filter on that and prefer connected > disconnected, excluding "unavailable".
#
# Output: a single UDID on stdout (the value cap run ios --target wants), or
# empty if no usable device is found (callers should fall back to interactive
# target selection). Never errors out — dev-ios must still run on a clean Mac.
set -euo pipefail

tmp="$(mktemp -t k2-devicectl).json"
trap 'rm -f "$tmp"' EXIT

if xcrun devicectl list devices --json-output "$tmp" >/dev/null 2>&1; then
  udid="$(python3 - "$tmp" <<'PY'
import json, sys
try:
    devices = json.load(open(sys.argv[1]))["result"]["devices"]
except Exception:
    sys.exit(0)
# Lower rank = preferred. Anything "unavailable" is dropped entirely.
rank = {"connected": 0, "connecting": 1, "disconnected": 2}
best = None
for d in devices:
    hw = d.get("hardwareProperties", {})
    if "iPhone" not in (hw.get("marketingName") or "") and \
       "iPhone" not in (hw.get("deviceType") or ""):
        continue
    state = d.get("connectionProperties", {}).get("tunnelState", "")
    if state == "unavailable" or state not in rank:
        continue
    udid = hw.get("udid")
    if not udid:
        continue
    if best is None or rank[state] < best[0]:
        best = (rank[state], udid)
if best:
    print(best[1])
PY
)"
  if [ -n "$udid" ]; then
    echo "$udid"
    exit 0
  fi
fi

# Fallback: devicectl unavailable (older Xcode) — old xctrace heuristic.
xcrun xctrace list devices 2>/dev/null \
  | grep 'iPhone' | grep -v 'Simulator' \
  | sed 's/.*(\([0-9A-Fa-f-]\{25,\}\)).*/\1/' | head -1
