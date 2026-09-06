#!/usr/bin/env bash
# Regenerates every Overleap bitmap from logo.svg. Run from anywhere:
#   bash webapp/brand-assets/overleap/generate.sh
# Requires: ImageMagick 7 (`magick`), macOS `iconutil` and `qlmanage`.
# SVG rasterization goes through QuickLook (WebKit): ImageMagick's built-in MSVG
# renderer silently drops stroked paths (verified 2026-09-04 — it drew the
# background rect and nothing else), and the rsvg delegate is not installed.
#
# Outputs: webapp icons, web (Next.js) icons + OG image, desktop (Tauri) icons,
# iOS AppIcon + Splash (staged by scripts/apply-ios-brand.sh) and the Android
# overleap-flavour launcher icons + splash. Set OUT_ROOT to render into another
# tree (scripts/check-mobile-brand-assets.sh style guards / experiments).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
SVG="$HERE/logo.svg"

for tool in magick iconutil qlmanage; do
  command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: $tool not found" >&2; exit 1; }
done

OUT_ROOT="${OUT_ROOT:-$ROOT}"          # guard renders into a temp tree
WEBAPP_ASSETS="$OUT_ROOT/webapp/src/brands/overleap/assets"
DESKTOP_ICONS="$OUT_ROOT/desktop/src-tauri/icons-overleap"
WEB_PUBLIC="$OUT_ROOT/web/public"
WEB_BRAND="$WEB_PUBLIC/brand/overleap"
IOS_BRAND="$OUT_ROOT/mobile/ios/App/App/brand/overleap"
AND_RES="$OUT_ROOT/mobile/android/app/src/overleap/res"
# Background colour is read from the SVG, never duplicated here.
BG=$(grep -o 'rx="112" fill="#[0-9A-Fa-f]\{6\}"' "$SVG" | grep -o '#[0-9A-Fa-f]\{6\}')
[ -n "$BG" ] || { echo "ERROR: cannot read background fill from logo.svg" >&2; exit 1; }
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$WEBAPP_ASSETS" "$DESKTOP_ICONS" "$WEB_BRAND"

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
# Slogan = webapp/src/brands/overleap/index.ts slogans.default.
magick -size 1200x630 xc:'#0B0E14' \
  "$TMP/og-logo.png" -geometry +500+90 -composite \
  -font "$FONT" -gravity North \
  -pointsize 72 -fill '#E6E8F0' -annotate +0+320 'Overleap' \
  -pointsize 34 -fill '#9AA0B4' -annotate +0+420 'Your browsing is your business.' \
  -pointsize 26 -fill "$BG" -annotate +0+540 'overleap.io' \
  -strip "$WEB_PUBLIC/overleap-og.png"

# --- iOS (staged by scripts/apply-ios-brand.sh) ---
# `xc:` canvases are Q16 → force 8-bit output (half the bytes, what the stores expect).
mkdir -p "$IOS_BRAND/AppIcon.appiconset" "$IOS_BRAND/Splash.imageset"
# App Store icons must be opaque: fill the rounded corners with the brand background.
magick "$TMP/master-1024.png" -background "$BG" -alpha remove -alpha off -depth 8 -strip "$IOS_BRAND/AppIcon.appiconset/AppIcon-512@2x.png"
# Splash master: 2732 square, brand background, logomark centred at 30% width.
png 820 "$TMP/splash-logo.png"
magick -size 2732x2732 xc:"$BG" "$TMP/splash-logo.png" -gravity center -composite -depth 8 -strip "$IOS_BRAND/Splash.imageset/splash-2732x2732.png"
cp "$IOS_BRAND/Splash.imageset/splash-2732x2732.png" "$IOS_BRAND/Splash.imageset/splash-2732x2732-1.png"
cp "$IOS_BRAND/Splash.imageset/splash-2732x2732.png" "$IOS_BRAND/Splash.imageset/splash-2732x2732-2.png"
cat > "$IOS_BRAND/Splash.imageset/Contents.json" <<'JSON'
{
  "images" : [
    { "idiom" : "universal", "filename" : "splash-2732x2732-2.png", "scale" : "1x" },
    { "idiom" : "universal", "filename" : "splash-2732x2732-1.png", "scale" : "2x" },
    { "idiom" : "universal", "filename" : "splash-2732x2732.png", "scale" : "3x" }
  ],
  "info" : { "version" : 1, "author" : "xcode" }
}
JSON

# --- Android (overleap flavour resources) ---
# legacy 48dp launcher (API 24-25) / adaptive 108dp foreground (API 26+) / splash
for spec in mdpi:48:108 hdpi:72:162 xhdpi:96:216 xxhdpi:144:324 xxxhdpi:192:432; do
  d=${spec%%:*}; rest=${spec#*:}; L=${rest%%:*}; A=${rest##*:}
  mkdir -p "$AND_RES/mipmap-$d"
  png "$L" "$AND_RES/mipmap-$d/ic_launcher.png"
  c=$(( L / 2 ))
  magick "$TMP/master-1024.png" -resize "${L}x${L}" \
    \( -size "${L}x${L}" xc:none -fill white -draw "circle $c,$c $c,0" \) \
    -compose CopyOpacity -composite -depth 8 -strip "$AND_RES/mipmap-$d/ic_launcher_round.png"
  # Adaptive icon: the 66dp safe-zone circle inside the 108dp canvas.
  fg=$(( A * 66 / 100 ))
  magick -size "${A}x${A}" xc:none \( "$TMP/master-1024.png" -resize "${fg}x${fg}" \) \
    -gravity center -composite -depth 8 -strip "$AND_RES/mipmap-$d/ic_launcher_foreground.png"
done
mkdir -p "$AND_RES/values"
cat > "$AND_RES/values/ic_launcher_background.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">$BG</color>
</resources>
XML
# Splash per density (Capacitor size table); same file names as main/ so the
# flavour overrides them.
for spec in mdpi:320x480 hdpi:480x800 xhdpi:720x1280 xxhdpi:960x1600 xxxhdpi:1280x1920; do
  d=${spec%%:*}; wh=${spec#*:}; w=${wh%%x*}; h=${wh##*x}
  logo=$(( (w < h ? w : h) * 30 / 100 ))
  png "$logo" "$TMP/splash-$d.png"
  mkdir -p "$AND_RES/drawable-port-$d" "$AND_RES/drawable-land-$d"
  magick -size "${w}x${h}" xc:"$BG" "$TMP/splash-$d.png" -gravity center -composite -depth 8 -strip "$AND_RES/drawable-port-$d/splash.png"
  magick -size "${h}x${w}" xc:"$BG" "$TMP/splash-$d.png" -gravity center -composite -depth 8 -strip "$AND_RES/drawable-land-$d/splash.png"
done

echo "done: $(ls "$WEBAPP_ASSETS" | wc -l | tr -d ' ') webapp, $(ls "$WEB_BRAND" | wc -l | tr -d ' ') web-brand, $(ls "$DESKTOP_ICONS" | grep -c png) desktop png, $(find "$IOS_BRAND" -name '*.png' | wc -l | tr -d ' ') ios png, $(find "$AND_RES" -name '*.png' | wc -l | tr -d ' ') android png"
