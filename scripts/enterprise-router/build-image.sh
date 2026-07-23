#!/usr/bin/env bash
# Reproducible enterprise-router firmware image via OpenWrt ImageBuilder.
# Usage: PROFILE=<device-profile> K2R_BIN=path/to/k2r ./build-image.sh
set -euo pipefail

OPENWRT_VERSION="${OPENWRT_VERSION:-23.05.5}"
TARGET="${TARGET:-mediatek/filogic}"        # MT7981 reference target
PROFILE="${PROFILE:?device profile required (openwrt profile name)}"
K2R_BIN="${K2R_BIN:?path to k2r binary required}"
WORKDIR="${WORKDIR:-$(pwd)/.ib-work}"

# Resolve script-relative paths BEFORE any cd — $0 may be relative.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K2R_BIN="$(cd "$(dirname "$K2R_BIN")" && pwd)/$(basename "$K2R_BIN")"

IB="openwrt-imagebuilder-${OPENWRT_VERSION}-${TARGET//\//-}.Linux-x86_64"
mkdir -p "$WORKDIR" && cd "$WORKDIR"
[ -d "$IB" ] || {
  curl -fsSLO "https://downloads.openwrt.org/releases/${OPENWRT_VERSION}/targets/${TARGET}/${IB}.tar.xz"
  tar xf "${IB}.tar.xz"
}

FILES="$SCRIPT_DIR/files"
STAGING="$WORKDIR/files"
rm -rf "$STAGING" && cp -a "$FILES" "$STAGING"
install -Dm755 "$K2R_BIN" "$STAGING/usr/bin/k2r"

make -C "$IB" image \
  PROFILE="$PROFILE" \
  PACKAGES="kmod-nft-tproxy conntrack curl" \
  FILES="$STAGING"

echo "Images at: $WORKDIR/$IB/bin/targets/${TARGET}/"
