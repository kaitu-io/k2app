#!/usr/bin/env bash
set -euo pipefail
# Desktop artifact brand purity guard.
#   check-desktop-brand-purity.sh <kaitu|overleap> <path>
# <path>: a .app bundle, a directory of unpacked resources (Windows), or a
#         .app.tar.gz updater archive (checked by content, not filename —
#         both brands share the Tauri bundle dir, so a stale other-brand
#         tar.gz can get collected under the right name; see check 0).
# Checks:
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
#
# F7: the forbidden pattern anchors on the *compile-time literal* domain+brand
# prefix (channel.rs STABLE_ENDPOINTS/BETA_ENDPOINTS/WEB_OTA_BASES), e.g.
# "cloudfront.net/kaitu" or "all7.cc/kaitu" — every one of those consts is a
# single &str literal that starts "https://{domain}/{brand}", so this
# substring is guaranteed contiguous in the compiled binary's rodata. The
# previous pattern `/{brand}/(desktop|web)/` matched "/kaitu/web/" too, but
# that segment only exists at runtime — WEB_OTA_BASES holds just
# "https://d0.all7.cc/kaitu" and the "web/latest.json" suffix is appended via
# format!() in channel.rs's source_for(), so "/kaitu/web/" being contiguous in
# the binary was never guaranteed by the compiler, only by incidental rodata
# layout.

BRAND="${1:?usage: $0 <kaitu|overleap> <path>}"
TARGET="${2:?usage: $0 <kaitu|overleap> <path>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

case "$BRAND" in
  kaitu)    FORBIDDEN='overleap\.io|cloudfront\.net/overleap|all7\.cc/overleap'; EXPECTED_APP='Kaitu.app' ;;
  overleap) FORBIDDEN='kaitu\.io|开途|開途|cloudfront\.net/kaitu|all7\.cc/kaitu'; EXPECTED_APP='Overleap.app' ;;
  *) echo "ERROR: brand must be kaitu|overleap" >&2; exit 1 ;;
esac

FAIL=0

# 0) updater .app.tar.gz — the top-level .app dir inside the archive must be
#    the expected brand's productName. Guards against collecting a stale
#    other-brand tar.gz under a brand-correct release filename.
case "$TARGET" in
  *.app.tar.gz)
    if [ ! -f "$TARGET" ]; then
      echo "ERROR: $TARGET not found" >&2
      exit 1
    fi
    TOP_APP=$(tar tzf "$TARGET" 2>/dev/null | cut -d/ -f1 | grep '\.app$' | sort -u || true)
    if [ "$TOP_APP" != "$EXPECTED_APP" ]; then
      echo "PURITY FAIL ($BRAND): $TARGET top-level app is '${TOP_APP:-<none>}', expected '$EXPECTED_APP'" >&2
      FAIL=1
    fi
    if [ "$FAIL" = 0 ]; then
      echo "PURITY OK ($BRAND): $TARGET"
    fi
    exit "$FAIL"
    ;;
esac

# 1) webapp resources inside the bundle (macOS: Contents/Resources; else: as-is)
RES_DIR="$TARGET"
[ -d "$TARGET/Contents/Resources" ] && RES_DIR="$TARGET/Contents/Resources"
if ! bash "$ROOT_DIR/webapp/scripts/check-brand-purity.sh" "$BRAND" "$RES_DIR"; then
  FAIL=1
fi

# 2) binary strings — check every Mach-O/PE in the bundle
while IFS= read -r BIN; do
  # Capture first, don't test the pipeline directly: under `set -o pipefail`,
  # `grep -m1` exiting as soon as it matches can SIGPIPE the still-writing
  # `strings` producer, and that producer's non-zero (141) exit status can be
  # the pipeline's pipefail-visible status — making `if` see failure even
  # though a match was found (mass leaks would report "PURITY OK"). See
  # scripts/check-mobile-brand-purity.sh for the same fix and full rationale.
  FOUND=$(strings "$BIN" 2>/dev/null | grep -Ei -m1 "$FORBIDDEN" || true)
  if [ -n "$FOUND" ]; then
    echo "PURITY FAIL ($BRAND): $(basename "$BIN") contains forbidden pattern: $FORBIDDEN" >&2
    strings "$BIN" | grep -Ei "$FORBIDDEN" | head -5 >&2
    FAIL=1
  fi
done < <( (find "$TARGET" -type f ! -name 'k2' ! -name 'k2.exe' ! -name 'k2-*' \( -perm +111 -o -name '*.exe' -o -name '*.dll' \) 2>/dev/null || \
           find "$TARGET" -type f ! -name 'k2' ! -name 'k2.exe' ! -name 'k2-*' \( -perm /111 -o -name '*.exe' -o -name '*.dll' \) 2>/dev/null) )
# ^ Exclusions name the k2 SIDECAR exactly (k2, k2.exe, k2-<triple>). The old
#   `k2*` glob also swallowed k2app.exe — the Tauri app binary itself on
#   Windows (Cargo crate name `k2app`) — so a Windows target dir was never
#   actually string-checked. `make build-windows` stages that binary here.

if [ "$FAIL" = 0 ]; then
  echo "PURITY OK ($BRAND): $TARGET"
fi
exit "$FAIL"
