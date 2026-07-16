#!/usr/bin/env bash
set -euo pipefail
# Desktop artifact brand purity guard.
#   check-desktop-brand-purity.sh <kaitu|overleap> <path>
# <path>: a .app bundle, or a directory of unpacked resources (Windows).
# Two checks:
#   1) loose resources — reuse webapp/scripts/check-brand-purity.sh. CAVEAT:
#      Tauri v2 embeds frontendDist INTO the binary (brotli-compressed), so
#      the webapp payload is NOT visible here or to `strings`. The webapp's
#      purity is therefore gated on webapp/dist BEFORE `tauri build` (see the
#      pre-package gate below) — this artifact-level check only covers loose
#      files and the Rust-side URLs.
#   2) binary strings — the other brand's updater/CDN URLs must not appear
#      (guaranteed by cfg(brand_overleap) compile-time fork; this catches regressions)
# Bare "kaitu" tokens (kaitu-icon:// scheme, HKDF salt, S3 bucket, service name)
# are protocol/internal identifiers and intentionally allowed.

BRAND="${1:?usage: $0 <kaitu|overleap> <path>}"
TARGET="${2:?usage: $0 <kaitu|overleap> <path>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

case "$BRAND" in
  kaitu)    FORBIDDEN='overleap\.io|/overleap/desktop/' ;;
  overleap) FORBIDDEN='kaitu\.io|开途|開途|/kaitu/desktop/' ;;
  *) echo "ERROR: brand must be kaitu|overleap" >&2; exit 1 ;;
esac

FAIL=0

# 1) webapp resources inside the bundle (macOS: Contents/Resources; else: as-is)
RES_DIR="$TARGET"
[ -d "$TARGET/Contents/Resources" ] && RES_DIR="$TARGET/Contents/Resources"
if ! bash "$ROOT_DIR/webapp/scripts/check-brand-purity.sh" "$BRAND" "$RES_DIR"; then
  FAIL=1
fi

# 2) binary strings — check every Mach-O/PE in the bundle
while IFS= read -r BIN; do
  if strings "$BIN" 2>/dev/null | grep -E -m1 "$FORBIDDEN" >/dev/null; then
    echo "PURITY FAIL ($BRAND): $(basename "$BIN") contains forbidden pattern: $FORBIDDEN" >&2
    strings "$BIN" | grep -E "$FORBIDDEN" | head -5 >&2
    FAIL=1
  fi
done < <( (find "$TARGET" -type f ! -name 'k2*' \( -perm +111 -o -name '*.exe' -o -name '*.dll' \) 2>/dev/null || \
           find "$TARGET" -type f ! -name 'k2*' \( -perm /111 -o -name '*.exe' -o -name '*.dll' \) 2>/dev/null) )

if [ "$FAIL" = 0 ]; then
  echo "PURITY OK ($BRAND): $TARGET"
fi
exit "$FAIL"
