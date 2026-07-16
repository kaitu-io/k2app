#!/usr/bin/env bash
set -euo pipefail
# Mobile artifact purity: unzip the APK (or use the .app inside an xcarchive)
# and grep for the other brand's tokens. Reuses the webapp purity patterns.
#   check-mobile-brand-purity.sh <kaitu|overleap> <apk-or-xcarchive-or-dir>
#
# All greps are case-insensitive (-Ei): the only real historical brand leak
# was a mixed-case variant (Overleap.io / KAITU.IO style), which a case-
# sensitive `-E` grep would have missed entirely. See
# scripts/check-desktop-brand-purity.sh (fixed the same way in 3236f30e) and
# webapp/scripts/check-brand-purity.sh (uses -EliI / -EnoiI) for precedent.
BRAND="${1:?}"; TARGET="${2:?}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"; ROOT_DIR="$(dirname "$SCRIPT_DIR")"
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT

case "$TARGET" in
  # -o (overwrite, non-interactive): some APKs contain duplicate zip entries
  # (observed with baseline-profile injected builds); without -o, unzip
  # prompts on stdin, gets EOF under CI/set -e, and returns a nonzero
  # "warnings occurred" exit that aborts the whole script before any grep runs.
  *.apk) unzip -qq -o "$TARGET" -d "$WORK/apk"; SCAN="$WORK/apk" ;;
  *.xcarchive) SCAN=$(find "$TARGET/Products/Applications" -maxdepth 1 -name '*.app' | head -1) ;;
  *) SCAN="$TARGET" ;;
esac

case "$BRAND" in
  kaitu)    FORBIDDEN='overleap\.io|/overleap/' ;;
  overleap) FORBIDDEN='kaitu\.io|开途|開途|/kaitu/(android|ios|web)/' ;;
  *) echo "brand must be kaitu|overleap" >&2; exit 1 ;;
esac

# assets/ (webapp dist) — text grep; native payloads — strings. K2Mobile/gomobile
# frameworks and libgojni are protocol layer (brand-neutral) and excluded.
# Bare `io.kaitu` / `io.overleap` package namespace tokens are NOT matched by
# the path patterns above (narrowed to CDN path segments, not bare brand
# words) — dex class/package names must not trip this guard.
FAIL=0
if grep -rEil --binary-files=without-match "$FORBIDDEN" "$SCAN" \
     --exclude-dir='K2Mobile.xcframework' --exclude-dir='Frameworks' --exclude='lib*.so' 2>/dev/null | head -5 | grep .; then
  echo "PURITY FAIL ($BRAND): text matches above" >&2
  FAIL=1
fi
while IFS= read -r BIN; do
  if strings "$BIN" 2>/dev/null | grep -Ei -m1 "$FORBIDDEN" >/dev/null; then
    echo "PURITY FAIL ($BRAND): $(basename "$BIN")" >&2
    FAIL=1
  fi
done < <(find "$SCAN" \( -name 'classes*.dex' -o -path '*/MacOS/*' -o -name "$(basename "${SCAN%.app}")" \) -type f 2>/dev/null)
[ "$FAIL" = 0 ] && echo "PURITY OK ($BRAND): $TARGET"
exit "$FAIL"
