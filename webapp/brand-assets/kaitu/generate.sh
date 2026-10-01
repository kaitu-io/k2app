#!/usr/bin/env bash
# Regenerates every kaitu bitmap from logo.svg (+ logo-small.svg). Run from anywhere:
#   bash webapp/brand-assets/kaitu/generate.sh
# Requires: ImageMagick 7 (`magick`), macOS `iconutil` and `qlmanage`.
# SVG rasterization goes through QuickLook (WebKit) — see the overleap sibling
# script for why ImageMagick's own SVG renderer is not used.
#
# The mark is a K whose two arms run off the tile. logo.svg draws them far past
# the 512 box and clips to the rounded tile; this script strips that clip to get
# a full-bleed field, then cuts each platform's shape out of it:
#   tile    rounded square (web, webapp, Windows, legacy Android square, splash)
#   square  full-bleed, opaque (iOS — the OS applies its own mask)
#   round   circle (legacy Android round icon), mark shrunk to stay inside it
#   adaptive  Android foreground = ink only, background = the tile colour
#   small   logo-small.svg (heavier strokes) for 16/32 px
# Not generated here: web/public/images/og-default.png — after a logo change also run
#   (cd web && node scripts/generate-og-image.mjs)   # reads public/kaitu-icon.png
# Set OUT_ROOT to render into another tree.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
SVG="$HERE/logo.svg"
SVG_SMALL="$HERE/logo-small.svg"

for tool in magick iconutil qlmanage; do
  command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: $tool not found" >&2; exit 1; }
done

OUT_ROOT="${OUT_ROOT:-$ROOT}"
WEBAPP_ASSETS="$OUT_ROOT/webapp/src/brands/kaitu/assets"
DESKTOP_ICONS="$OUT_ROOT/desktop/src-tauri/icons"
WEB_PUBLIC="$OUT_ROOT/web/public"
IOS_BRAND="$OUT_ROOT/mobile/ios/App/App/brand/kaitu"
IOS_ACTIVE="$OUT_ROOT/mobile/ios/App/App/Assets.xcassets"   # committed copy = kaitu
AND_RES="$OUT_ROOT/mobile/android/app/src/main/res"
# Colours are read from the SVG, never duplicated here.
BG=$(grep -o 'fill="#[0-9A-Fa-f]\{6\}" stroke="none"' "$SVG" | grep -o '#[0-9A-Fa-f]\{6\}')
INK=$(grep -o '<g [^>]*stroke="#[0-9A-Fa-f]\{6\}"' "$SVG" | grep -o '#[0-9A-Fa-f]\{6\}')
[ -n "$BG" ] && [ -n "$INK" ] || { echo "ERROR: cannot read colours from logo.svg" >&2; exit 1; }
SPLASH_BG="#0F0F13"   # app dark surface; the tile sits on it
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$WEBAPP_ASSETS" "$DESKTOP_ICONS" "$WEB_PUBLIC"

render() { # render <src.svg> <viewBox> <out.png>  — unclipped field, 1024 px
  local name; name="$(basename "$3" .png)"
  sed -e 's/ clip-path="url(#tile)"//' -e "s/viewBox=\"0 0 512 512\"/viewBox=\"$2\"/" "$1" > "$TMP/$name.svg"
  qlmanage -t -s 1024 -o "$TMP" "$TMP/$name.svg" >/dev/null 2>&1
  [ -f "$TMP/$name.svg.png" ] || { echo "ERROR: qlmanage produced no thumbnail for $name" >&2; exit 1; }
  mv "$TMP/$name.svg.png" "$3"
}
render "$SVG"       "0 0 512 512"        "$TMP/square.png"
render "$SVG_SMALL" "0 0 512 512"        "$TMP/square-small.png"
render "$SVG"       "-40 -40 592 592"    "$TMP/round-field.png"      # mark at 86% of a circle
render "$SVG"       "-190 -190 892 892"  "$TMP/adaptive-field.png"   # 512 box = 86% of the 72dp viewport

# Rounded tile: rx 114/512 → 228 px at 1024.
magick -size 1024x1024 xc:none -fill white -draw "roundrectangle 0,0 1023,1023 228,228" "$TMP/mask.png"
magick "$TMP/square.png"       "$TMP/mask.png" -compose CopyOpacity -composite "$TMP/tile.png"
magick "$TMP/square-small.png" "$TMP/mask.png" -compose CopyOpacity -composite "$TMP/tile-small.png"
magick "$TMP/round-field.png" \( -size 1024x1024 xc:none -fill white -draw "circle 512,512 512,0" \) \
  -compose CopyOpacity -composite "$TMP/round.png"
# Adaptive foreground: ink only. Alpha comes from the green channel (ink ≈ 0, tile = 255),
# so anti-aliased edges carry no green fringe.
magick "$TMP/adaptive-field.png" -channel G -separate +channel -negate -level 0%,96% "$TMP/fg-alpha.png"
magick -size 1024x1024 xc:"$INK" "$TMP/fg-alpha.png" -alpha off -compose CopyOpacity -composite "$TMP/foreground.png"
# macOS: tile at 824/1024 with the standard transparent margin.
magick -size 1024x1024 xc:none \( "$TMP/tile.png" -resize 824x824 \) -gravity center -composite "$TMP/mac.png"
magick -size 1024x1024 xc:none \( "$TMP/tile-small.png" -resize 824x824 \) -gravity center -composite "$TMP/mac-small.png"

png() { # png <size> <out> [master]  — ≤32 px uses the heavier small-size drawing
  local src="${3:-tile}"
  if [ "$1" -le 32 ] && [ -f "$TMP/$src-small.png" ]; then src="$src-small"; fi
  # PNG32: Tauri's generate_context! rejects any icon that is not 8-bit RGBA.
  magick "$TMP/$src.png" -resize "${1}x${1}" -strip "PNG32:$2"
}
ico() { # ico <out> <sizes...>
  local out="$1" files=(); shift
  for s in "$@"; do png "$s" "$TMP/ico-$s.png"; files+=("$TMP/ico-$s.png"); done
  magick "${files[@]}" "$out"
}

# --- webapp (served as /favicon.png, /icon-192x192.png, /icon-512x512.png) ---
png 64  "$WEBAPP_ASSETS/favicon.png"
png 192 "$WEBAPP_ASSETS/icon-192x192.png"
png 512 "$WEBAPP_ASSETS/icon-512x512.png"

# --- web (Next.js public/; kaitu's favicon set lives at the public root) ---
png 512 "$WEB_PUBLIC/kaitu-icon.png"
png 16  "$WEB_PUBLIC/favicon-16x16.png"
png 32  "$WEB_PUBLIC/favicon-32x32.png"
png 48  "$WEB_PUBLIC/icon-48x48.png"
png 96  "$WEB_PUBLIC/icon-96x96.png"
png 192 "$WEB_PUBLIC/icon-192x192.png"
png 512 "$WEB_PUBLIC/icon-512x512.png"
ico "$WEB_PUBLIC/favicon.ico" 16 32 48

# --- desktop (Tauri icons/) ---
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
for f in "$DESKTOP_ICONS"/*.png; do
  ct=$(magick identify -format '%[png:IHDR.color-type-orig]' "$f")
  [ "$ct" = "6" ] || { echo "ERROR: $f has PNG colour type $ct, Tauri needs 6 (RGBA)" >&2; exit 1; }
done
ICONSET="$TMP/icon.iconset"; mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  png "$s"        "$ICONSET/icon_${s}x${s}.png"    mac
  png "$((s*2))"  "$ICONSET/icon_${s}x${s}@2x.png" mac
done
iconutil -c icns "$ICONSET" -o "$DESKTOP_ICONS/icon.icns"
ico "$DESKTOP_ICONS/icon.ico" 16 32 48 64 128 256
ico "$DESKTOP_ICONS/favicon.ico" 16 32 48

# --- iOS (brand/kaitu is what scripts/apply-ios-brand.sh stages; Assets.xcassets is its committed copy) ---
mkdir -p "$IOS_BRAND/AppIcon.appiconset" "$IOS_BRAND/Splash.imageset"
# App Store icons must be opaque and unmasked.
magick "$TMP/square.png" -background "$BG" -alpha remove -alpha off -depth 8 -strip "$IOS_BRAND/AppIcon.appiconset/AppIcon-512@2x.png"
magick "$TMP/tile.png" -resize 600x600 "$TMP/splash-logo.png"
magick -size 2732x2732 xc:"$SPLASH_BG" "$TMP/splash-logo.png" -gravity center -composite -depth 8 -strip "$IOS_BRAND/Splash.imageset/splash-2732x2732.png"
cp "$IOS_BRAND/Splash.imageset/splash-2732x2732.png" "$IOS_BRAND/Splash.imageset/splash-2732x2732-1.png"
cp "$IOS_BRAND/Splash.imageset/splash-2732x2732.png" "$IOS_BRAND/Splash.imageset/splash-2732x2732-2.png"
if [ -d "$IOS_ACTIVE" ]; then
  cp "$IOS_BRAND/AppIcon.appiconset/AppIcon-512@2x.png" "$IOS_ACTIVE/AppIcon.appiconset/AppIcon-512@2x.png"
  cp "$IOS_BRAND"/Splash.imageset/splash-2732x2732*.png "$IOS_ACTIVE/Splash.imageset/"
fi

# --- Android (flavour-neutral main/ set = kaitu) ---
for spec in mdpi:48:108 hdpi:72:162 xhdpi:96:216 xxhdpi:144:324 xxxhdpi:192:432; do
  d=${spec%%:*}; rest=${spec#*:}; L=${rest%%:*}; A=${rest##*:}
  mkdir -p "$AND_RES/mipmap-$d"
  magick "$TMP/tile.png"       -resize "${L}x${L}" -depth 8 -strip "$AND_RES/mipmap-$d/ic_launcher.png"
  magick "$TMP/round.png"      -resize "${L}x${L}" -depth 8 -strip "$AND_RES/mipmap-$d/ic_launcher_round.png"
  magick "$TMP/foreground.png" -resize "${A}x${A}" -depth 8 -strip "PNG32:$AND_RES/mipmap-$d/ic_launcher_foreground.png"
done
BG_UP=$(echo "$BG" | tr 'a-f' 'A-F')
cat > "$AND_RES/values/ic_launcher_background.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">$BG_UP</color>
</resources>
XML
for spec in mdpi:320x480 hdpi:480x800 xhdpi:720x1280 xxhdpi:960x1600 xxxhdpi:1280x1920; do
  d=${spec%%:*}; wh=${spec#*:}; w=${wh%%x*}; h=${wh##*x}
  logo=$(( (w < h ? w : h) * 26 / 100 ))
  magick "$TMP/tile.png" -resize "${logo}x${logo}" "$TMP/splash-$d.png"
  mkdir -p "$AND_RES/drawable-port-$d" "$AND_RES/drawable-land-$d"
  magick -size "${w}x${h}" xc:"$SPLASH_BG" "$TMP/splash-$d.png" -gravity center -composite -depth 8 -strip "$AND_RES/drawable-port-$d/splash.png"
  magick -size "${h}x${w}" xc:"$SPLASH_BG" "$TMP/splash-$d.png" -gravity center -composite -depth 8 -strip "$AND_RES/drawable-land-$d/splash.png"
done
cp "$AND_RES/drawable-port-hdpi/splash.png" "$AND_RES/drawable/splash.png"   # density-less fallback, 480x800

echo "done: BG=$BG INK=$INK"
