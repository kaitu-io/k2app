#!/usr/bin/env bash
# check-install-script-sync.sh — fail when the install script served at
# https://kaitu.io/i/k2r (the static file web/public/i/k2r, deployed with
# the website) drifts from its source of truth k2/scripts/install-k2r.sh
# (the copy the k2r release workflow uploads to the CDN).
#
# The two are hand-synced; nothing else keeps them equal. In 2026-06 the
# mips/mipsle fix landed in k2/scripts and never reached kaitu.io/i/k2r,
# and the copy users actually run stayed broken on OpenWrt 25.x for months.
#
# Run from the repo root with the k2 submodule checked out.
set -euo pipefail

cd "$(dirname "$0")/.."

SRC="k2/scripts/install-k2r.sh"
SERVED="web/public/i/k2r"

[ -f "$SRC" ] || { echo "missing $SRC (k2 submodule not initialised?)"; exit 1; }
[ -f "$SERVED" ] || { echo "missing $SERVED"; exit 1; }

if ! diff -q "$SRC" "$SERVED" >/dev/null; then
    echo "::error::$SERVED is out of sync with $SRC — run: cp $SRC $SERVED"
    diff -u "$SRC" "$SERVED" || true
    exit 1
fi
echo "install-k2r.sh: served copy matches k2/scripts"
