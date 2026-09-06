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

# Asset catalog sets swapped per brand: AppIcon + Splash. Both brands have a
# brand/<brand>/ copy (kaitu's is a byte copy of the historical catalog), so
# the staging is symmetric and a missing set fails loudly instead of leaving
# the previous brand's artwork in place.
for set in AppIcon.appiconset Splash.imageset; do
  src="$APP/brand/$BRAND/$set"
  [ -d "$src" ] || { echo "ERROR: missing $src" >&2; exit 1; }
  rsync -a --delete "$src/" "$APP/Assets.xcassets/$set/"
done

echo "iOS brand staged: $BRAND"
