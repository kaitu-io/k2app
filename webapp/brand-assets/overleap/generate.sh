#!/usr/bin/env bash
# Regenerates every Overleap bitmap from logo.svg. Run from anywhere:
#   bash webapp/brand-assets/overleap/generate.sh
# Requires: ImageMagick 7 (`magick`), macOS `iconutil` and `qlmanage`.
# SVG rasterization goes through QuickLook (WebKit): ImageMagick's built-in MSVG
# renderer silently drops stroked paths (verified 2026-09-04 — it drew the
# background rect and nothing else), and the rsvg delegate is not installed.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
SVG="$HERE/logo.svg"

for tool in magick iconutil qlmanage; do
  command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: $tool not found" >&2; exit 1; }
done

WEBAPP_ASSETS="$ROOT/webapp/src/brands/overleap/assets"
DESKTOP_ICONS="$ROOT/desktop/src-tauri/icons-overleap"
WEB_PUBLIC="$ROOT/web/public"
WEB_BRAND="$WEB_PUBLIC/brand/overleap"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# One high-res master, then downscale (keeps edges consistent across sizes).
# QuickLook fills the corners outside the rounded rect with white, so re-apply
# the SVG's rx=112 corner radius (224px at 1024) as an alpha mask.
qlmanage -t -s 1024 -o "$TMP" "$SVG" >/dev/null 2>&1
[ -f "$TMP/logo.svg.png" ] || { echo "ERROR: qlmanage produced no thumbnail" >&2; exit 1; }
magick -size 1024x1024 xc:none -fill white -draw "roundrectangle 0,0 1023,1023 224,224" "$TMP/mask.png"
magick "$TMP/logo.svg.png" "$TMP/mask.png" -compose CopyOpacity -composite "$TMP/master-1024.png"

png() { # png <size> <out>
  magick "$TMP/master-1024.png" -resize "${1}x${1}" -strip "$2"
}

# --- webapp (served as /favicon.png, /icon-192x192.png, /icon-512x512.png) ---
png 64  "$WEBAPP_ASSETS/favicon.png"
png 192 "$WEBAPP_ASSETS/icon-192x192.png"
png 512 "$WEBAPP_ASSETS/icon-512x512.png"

# --- web (Next.js public/) ---
png 512 "$WEB_PUBLIC/overleap-icon.png"
png 16  "$WEB_BRAND/favicon-16x16.png"
png 32  "$WEB_BRAND/favicon-32x32.png"
png 48  "$WEB_BRAND/icon-48x48.png"
png 96  "$WEB_BRAND/icon-96x96.png"
png 192 "$WEB_BRAND/icon-192x192.png"
png 512 "$WEB_BRAND/icon-512x512.png"

# --- desktop (Tauri icons-overleap/) ---
png 32  "$DESKTOP_ICONS/32x32.png"
png 64  "$DESKTOP_ICONS/64x64.png"
png 128 "$DESKTOP_ICONS/128x128.png"
png 256 "$DESKTOP_ICONS/128x128@2x.png"
png 256 "$DESKTOP_ICONS/256x256.png"
png 512 "$DESKTOP_ICONS/icon.png"
for s in 30 44 71 89 107 142 150 284 310; do
  png "$s" "$DESKTOP_ICONS/Square${s}x${s}Logo.png"
done
png 50 "$DESKTOP_ICONS/StoreLogo.png"

# .icns via iconutil (needs the exact Apple iconset file names)
ICONSET="$TMP/icon.iconset"; mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  png "$s"        "$ICONSET/icon_${s}x${s}.png"
  png "$((s*2))"  "$ICONSET/icon_${s}x${s}@2x.png"
done
iconutil -c icns "$ICONSET" -o "$DESKTOP_ICONS/icon.icns"

# .ico (multi-size)
magick "$TMP/master-1024.png" -resize 256x256 -define icon:auto-resize=256,128,64,48,32,16 "$DESKTOP_ICONS/icon.ico"

# --- OG image 1200x630 ---
FONT=""
for f in "/System/Library/Fonts/Supplemental/Arial Bold.ttf" "/System/Library/Fonts/Helvetica.ttc" "/Library/Fonts/Arial Bold.ttf"; do
  [ -f "$f" ] && { FONT="$f"; break; }
done
[ -n "$FONT" ] || { echo "ERROR: no usable font for OG image" >&2; exit 1; }
png 200 "$TMP/og-logo.png"
magick -size 1200x630 xc:'#0B0E14' \
  "$TMP/og-logo.png" -geometry +500+90 -composite \
  -font "$FONT" -gravity North \
  -pointsize 72 -fill '#E6E8F0' -annotate +0+320 'Overleap' \
  -pointsize 34 -fill '#9AA0B4' -annotate +0+420 'Stays connected where others drop.' \
  -pointsize 26 -fill '#7C5CFF' -annotate +0+540 'overleap.io' \
  -strip "$WEB_PUBLIC/overleap-og.png"

echo "done: $(ls "$WEBAPP_ASSETS" | wc -l | tr -d ' ') webapp, $(ls "$WEB_BRAND" | wc -l | tr -d ' ') web-brand, $(ls "$DESKTOP_ICONS" | grep -c png) desktop png"
