#!/usr/bin/env bash
# Numbering rules for the iOS CFBundleVersion, checked by driving the REAL
# derivation in build-mobile-ios.sh (--print-build-number). Nothing here
# re-implements the arithmetic: a test that mirrors the formula keeps passing
# after the formula changes, which is exactly how the old scheme's fatal gap
# (no room to re-upload a rejected release) went unnoticed until ASC refused
# the build.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD="$SCRIPT_DIR/build-mobile-ios.sh"

fails=0
pass() { printf '  PASS  %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; fails=$((fails + 1)); }

# Derive via the production script. Marketing version and build number only —
# no toolchain is touched.
num() { VERSION_OVERRIDE="$1" IOS_BUILD_REV="${2:-0}" bash "$BUILD" --print-build-number | awk '{print $2}'; }
mkt() { VERSION_OVERRIDE="$1" IOS_BUILD_REV="${2:-0}" bash "$BUILD" --print-build-number | awk '{print $1}'; }

echo "=== iOS build-number rules ==="

# 1. Every number ASC has already accepted is permanently unusable. These are
#    the real burned values (App Store Connect, app 6448744655): the 4xx era,
#    and the 44xxxx scheme up to the two that cost us the 0.4.8 submission.
BURNED_MAX=440900
n=$(num "0.4.8")
if (( n > BURNED_MAX )); then
  pass "0.4.8 release ($n) is above every burned number ($BURNED_MAX)"
else
  fail "0.4.8 release ($n) collides with or sits below burned $BURNED_MAX"
fi

# 2. The gap that broke 0.4.8: a rejected release must have a successor.
r0=$(num "0.4.8" 0); r1=$(num "0.4.8" 1)
if (( r1 > r0 )); then
  pass "re-upload revision advances ($r0 → $r1)"
else
  fail "IOS_BUILD_REV does not advance the number ($r0 → $r1)"
fi

# 3. Ordering within a version: beta.N ascending, all below the release.
b1=$(num "0.4.8-beta.1"); b2=$(num "0.4.8-beta.2"); b98=$(num "0.4.8-beta.98")
rel=$(num "0.4.8")
if (( b1 < b2 && b2 < b98 && b98 < rel )); then
  pass "beta.1 < beta.2 < beta.98 < release ($b1 < $b2 < $b98 < $rel)"
else
  fail "intra-version ordering broken ($b1, $b2, $b98, $rel)"
fi

# 4. Ordering across versions, including the tightest case: the LAST possible
#    number of one patch (release, highest revision) must still fall below the
#    FIRST number of the next patch. This is where a too-narrow digit band
#    would silently overflow into its neighbour.
prev_max=$(num "0.4.8" 9); next_min=$(num "0.4.9-beta.1" 0)
if (( prev_max < next_min )); then
  pass "0.4.8 rev 9 ($prev_max) < 0.4.9-beta.1 ($next_min) — bands do not overlap"
else
  fail "band overflow: 0.4.8 rev 9 ($prev_max) >= 0.4.9-beta.1 ($next_min)"
fi

minor_max=$(num "0.4.99" 9); minor_next=$(num "0.5.0-beta.1" 0)
if (( minor_max < minor_next )); then
  pass "0.4.99 rev 9 ($minor_max) < 0.5.0-beta.1 ($minor_next) — minor rollover holds"
else
  fail "minor rollover broken ($minor_max >= $minor_next)"
fi

# 5. Marketing remap stays 0.x.y → 4.x.y (Apple's downgrade check vs legacy 3.x).
m=$(mkt "0.4.8")
[[ "$m" == "4.4.8" ]] && pass "0.4.8 → marketing $m" || fail "marketing remap wrong: $m"

# 6. Out-of-range inputs abort rather than silently producing a colliding
#    number — the failure mode that matters is a *plausible* wrong number.
for bad in "10" "x" "-1"; do
  if VERSION_OVERRIDE="0.4.8" IOS_BUILD_REV="$bad" bash "$BUILD" --print-build-number >/dev/null 2>&1; then
    fail "IOS_BUILD_REV='$bad' was accepted"
  else
    pass "IOS_BUILD_REV='$bad' rejected"
  fi
done

if VERSION_OVERRIDE="0.4.8-beta.99" bash "$BUILD" --print-build-number >/dev/null 2>&1; then
  fail "beta.99 was accepted (99 is the release slot)"
else
  pass "beta.99 rejected — 99 belongs to the release"
fi

echo
if (( fails == 0 )); then
  echo "All iOS build-number rules hold."
else
  echo "$fails rule(s) violated."
  exit 1
fi
