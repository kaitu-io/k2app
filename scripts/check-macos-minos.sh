#!/usr/bin/env bash
# Gate: every Mach-O slice inside a .app must declare a minimum macOS (LC_BUILD_VERSION
# minos) no newer than the app's own Info.plist LSMinimumSystemVersion.
#
# Why: the k2 sidecar is a cgo binary linked by clang, which stamps the BUILD HOST's
# macOS as minos unless told otherwise — 0.4.7–0.4.13 shipped k2 with minos 26.0 inside
# an app that claims 12.0. Current dyld still launches it, but clang compiled the C
# parts assuming every macOS-26 API exists (strong-linked, no availability warnings),
# so the first such call would crash on older macOS with no build-time signal.
#
# Anchored on the shipped artifact (the bundle's own Info.plist), not on a config file,
# so it checks what users actually get.
#
# Usage: bash scripts/check-macos-minos.sh <path/to/App.app>
set -euo pipefail

APP="${1:?usage: check-macos-minos.sh <App.app>}"
PLIST="$APP/Contents/Info.plist"

FLOOR=$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$PLIST" 2>/dev/null || true)
if [ -z "$FLOOR" ]; then
  echo "ERROR: $PLIST has no LSMinimumSystemVersion — nothing to check against" >&2
  exit 1
fi

# "12.4" → 12004000 so versions compare numerically (12.10 > 12.4).
ver_num() { awk -F. '{ printf "%d\n", $1*1000000 + $2*1000 + $3 }' <<<"$1"; }
FLOOR_N=$(ver_num "$FLOOR")

checked=0
bad=0
while IFS= read -r -d '' f; do
  file -b "$f" | grep -q '^Mach-O' || continue
  for arch in $(lipo -archs "$f"); do
    minos=$(vtool -arch "$arch" -show-build "$f" 2>/dev/null | awk '$1=="minos"{print $2; exit}')
    if [ -z "$minos" ]; then
      echo "ERROR: ${f#"$APP"/} [$arch]: no LC_BUILD_VERSION minos" >&2
      bad=1
      continue
    fi
    checked=$((checked + 1))
    if [ "$(ver_num "$minos")" -gt "$FLOOR_N" ]; then
      echo "ERROR: ${f#"$APP"/} [$arch]: minos $minos > app floor $FLOOR" >&2
      bad=1
    else
      echo "ok: ${f#"$APP"/} [$arch]: minos $minos"
    fi
  done
done < <(find "$APP/Contents" -type f -print0)

if [ "$checked" -eq 0 ]; then
  echo "ERROR: no Mach-O binaries found under $APP/Contents — gate checked nothing" >&2
  exit 1
fi
if [ "$bad" -ne 0 ]; then
  echo "macOS minos gate FAILED (app floor LSMinimumSystemVersion=$FLOOR)" >&2
  exit 1
fi
echo "macOS minos gate passed: $checked slice(s) <= $FLOOR"
