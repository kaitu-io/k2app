#!/usr/bin/env bash
# scripts/check-mobile-brand-assets.sh <kaitu|overleap>
# Structural checks on the mobile brand artwork (machine-independent — no byte hashes,
# QuickLook rasterisation differs across macOS builds). Verifies the files exist, have
# the dimensions each platform requires, are not flat placeholders, carry the brand's
# background colour, and are not identical to the peer brand's files.
set -euo pipefail
BRAND="${1:?usage: $0 <kaitu|overleap>}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
command -v magick >/dev/null || { echo "ERROR: ImageMagick 7 (magick) required" >&2; exit 1; }
fails=0
pass() { printf '  PASS  %s\n' "$1"; }
fail() { printf '  FAIL  %s\n' "$1"; fails=$((fails+1)); }

dims()   { [ -f "$1" ] && magick identify -format '%wx%h' "$1" 2>/dev/null || echo missing; }
alpha()  { [ -f "$1" ] && magick identify -format '%A' "$1" 2>/dev/null || echo missing; }   # True/False/Blend/Undefined
colors() { [ -f "$1" ] && magick "$1" -format '%k' info: 2>/dev/null || echo 0; }             # unique colours
# Top-left pixel: pure background on every splash (the logomark is centred at 30%).
# An averaged colour is not used — Q16-HDRI `-scale 1x1` averages in a colorspace
# that does not round-trip to the sRGB hex we compare against.
corner() { magick "$1" -depth 8 -format '#%[hex:p{0,0}]' info: | cut -c1-7; }   # -depth 8: 16-bit PNGs print 12 hex digits

IOS="$ROOT/mobile/ios/App/App/brand/$BRAND"
AND="$ROOT/mobile/android/app/src/$BRAND/res"
# kaitu's Android artwork is the flavour-neutral main/ set (the kaitu flavour
# overrides nothing); overleap overrides every file under src/overleap/res.
[ "$BRAND" = kaitu ] && AND="$ROOT/mobile/android/app/src/main/res"

# 1. iOS AppIcon: 1024x1024, opaque, real image
f="$IOS/AppIcon.appiconset/AppIcon-512@2x.png"
[ "$(dims "$f")" = "1024x1024" ] && pass "AppIcon 1024x1024" || fail "AppIcon missing or wrong size ($(dims "$f"))"
case "$(alpha "$f")" in False|Undefined) pass "AppIcon has no alpha channel" ;; *) fail "AppIcon has alpha ($(alpha "$f")) — App Store rejects" ;; esac
[ "$(colors "$f")" -gt 50 ] && pass "AppIcon is not a flat placeholder" || fail "AppIcon has $(colors "$f") colours"

# 2. iOS Splash: three 2732x2732 files + Contents.json
for s in splash-2732x2732.png splash-2732x2732-1.png splash-2732x2732-2.png; do
  f="$IOS/Splash.imageset/$s"
  [ "$(dims "$f")" = "2732x2732" ] && pass "Splash $s" || fail "Splash $s missing or wrong size ($(dims "$f"))"
done
[ -f "$IOS/Splash.imageset/Contents.json" ] && pass "Splash Contents.json" || fail "Splash Contents.json missing"

# 3. Android launcher icons per density
declare -A LEGACY=([mdpi]=48 [hdpi]=72 [xhdpi]=96 [xxhdpi]=144 [xxxhdpi]=192)
declare -A ADAPT=([mdpi]=108 [hdpi]=162 [xhdpi]=216 [xxhdpi]=324 [xxxhdpi]=432)
for d in mdpi hdpi xhdpi xxhdpi xxxhdpi; do
  for n in ic_launcher ic_launcher_round; do
    f="$AND/mipmap-$d/$n.png"; want="${LEGACY[$d]}x${LEGACY[$d]}"
    [ "$(dims "$f")" = "$want" ] && pass "$d/$n $want" || fail "$d/$n missing or not $want ($(dims "$f"))"
    [ "$(colors "$f")" -gt 50 ] && pass "$d/$n not flat" || fail "$d/$n is a flat placeholder ($(colors "$f") colours)"
  done
  f="$AND/mipmap-$d/ic_launcher_foreground.png"; want="${ADAPT[$d]}x${ADAPT[$d]}"
  [ "$(dims "$f")" = "$want" ] && pass "$d/foreground $want" || fail "$d/foreground missing or not $want ($(dims "$f"))"
done
# round must differ from square (a real circular mask was applied). Overleap-only:
# kaitu's historical main/ set ships the same bytes for both (the adaptive icon
# path on API 26+ never reads them) and is left as-is on purpose.
if [ "$BRAND" = overleap ]; then
  cmp -s "$AND/mipmap-xxxhdpi/ic_launcher.png" "$AND/mipmap-xxxhdpi/ic_launcher_round.png" && fail "round icon identical to square" || pass "round icon differs from square"
fi

# 4. Android splash per density (Capacitor size table)
declare -A PORT=([mdpi]=320x480 [hdpi]=480x800 [xhdpi]=720x1280 [xxhdpi]=960x1600 [xxxhdpi]=1280x1920)
declare -A LAND=([mdpi]=480x320 [hdpi]=800x480 [xhdpi]=1280x720 [xxhdpi]=1600x960 [xxxhdpi]=1920x1280)
for d in mdpi hdpi xhdpi xxhdpi xxxhdpi; do
  f="$AND/drawable-port-$d/splash.png"; [ "$(dims "$f")" = "${PORT[$d]}" ] && pass "splash port $d" || fail "splash port $d missing or not ${PORT[$d]} ($(dims "$f"))"
  f="$AND/drawable-land-$d/splash.png"; [ "$(dims "$f")" = "${LAND[$d]}" ] && pass "splash land $d" || fail "splash land $d missing or not ${LAND[$d]} ($(dims "$f"))"
done

# 5. Brand colour: overleap artwork must be dominated by the logo.svg background
if [ "$BRAND" = overleap ]; then
  BG=$(grep -o 'rx="112" fill="#[0-9A-Fa-f]\{6\}"' "$ROOT/webapp/brand-assets/overleap/logo.svg" | grep -o '#[0-9A-Fa-f]\{6\}' | tr 'a-f' 'A-F')
  [ -n "$BG" ] || fail "cannot read background colour from logo.svg"
  grep -qi "$BG" "$AND/values/ic_launcher_background.xml" 2>/dev/null && pass "ic_launcher_background = $BG" || fail "ic_launcher_background.xml missing $BG"
  for f in "$AND/drawable-port-xxxhdpi/splash.png" "$IOS/Splash.imageset/splash-2732x2732.png"; do
    if [ -f "$f" ]; then
      m=$(corner "$f" | tr 'a-f' 'A-F')
      [ "$m" = "$BG" ] && pass "$(basename "$(dirname "$f")")/$(basename "$f") background = $BG" || fail "$(basename "$(dirname "$f")")/$(basename "$f") background $m != $BG"
    else
      fail "splash background: $f missing"
    fi
  done
fi

# 6. Not the peer brand's bytes
PEER=$([ "$BRAND" = overleap ] && echo kaitu || echo overleap)
PEER_AND="mobile/android/app/src/$PEER/res"; [ "$PEER" = kaitu ] && PEER_AND="mobile/android/app/src/main/res"
for pair in "$IOS/AppIcon.appiconset/AppIcon-512@2x.png:$ROOT/mobile/ios/App/App/brand/$PEER/AppIcon.appiconset/AppIcon-512@2x.png" \
            "$AND/mipmap-xxxhdpi/ic_launcher.png:$ROOT/$PEER_AND/mipmap-xxxhdpi/ic_launcher.png"; do
  a="${pair%%:*}"; b="${pair##*:}"
  if [ -f "$a" ] && [ -f "$b" ]; then cmp -s "$a" "$b" && fail "$(basename "$a") identical to $PEER's" || pass "$(basename "$a") differs from $PEER's"; fi
done

echo "=== $BRAND mobile assets: $fails failure(s) ==="
exit $(( fails > 0 ))
