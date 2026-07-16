#!/usr/bin/env bash
set -euo pipefail
# Stage iOS brand: xcconfig + localized display names + app icon.
# StoreKit configs (Kaitu.storekit / Overleap.storekit) are static per-brand
# files, not staged by this script — see mobile/CLAUDE.md "iOS" section.
# Usage: apply-ios-brand.sh <kaitu|overleap>
BRAND="${1:?usage: $0 <kaitu|overleap>}"
case "$BRAND" in kaitu|overleap) ;; *) echo "brand must be kaitu|overleap" >&2; exit 1 ;; esac
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT_DIR/mobile/ios/App/App"

cp "$APP/Config/brand-$BRAND.xcconfig" "$APP/Config/brand-active.xcconfig"

# Localized display names: clear brand-managed lproj sets, then copy active set
for l in en ja zh-Hans zh-Hant; do rm -f "$APP/$l.lproj/InfoPlist.strings"; done
if [ -d "$APP/brand/$BRAND" ]; then
  (cd "$APP/brand/$BRAND" && find . -name 'InfoPlist.strings' | while read -r f; do
    mkdir -p "$APP/$(dirname "$f")"
    cp "$f" "$APP/$f"
  done)
fi

# App icon: overleap swaps the asset catalog iconset content
if [ "$BRAND" = "overleap" ] && [ -d "$APP/brand/overleap/AppIcon.appiconset" ]; then
  rsync -a --delete "$APP/brand/overleap/AppIcon.appiconset/" "$APP/Assets.xcassets/AppIcon.appiconset/"
elif [ "$BRAND" = "kaitu" ] && [ -d "$APP/brand/kaitu/AppIcon.appiconset" ]; then
  rsync -a --delete "$APP/brand/kaitu/AppIcon.appiconset/" "$APP/Assets.xcassets/AppIcon.appiconset/"
fi

echo "iOS brand staged: $BRAND"
