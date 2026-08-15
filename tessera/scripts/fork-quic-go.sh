#!/usr/bin/env bash
# Materialize the apernet/quic-go fork under .fork/ and apply the Tessera seam patch.
#
# The patched fork is NOT committed (4.2 MB / 452 files); patches/*.patch is the
# real artifact. Re-run this after changing the patch or bumping the version.
set -euo pipefail

MODULE="github.com/apernet/quic-go"
VERSION="v0.57.2-0.20260111184307-eec823306178" # keep in sync with go.mod
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$(go env GOMODCACHE)/${MODULE}@${VERSION}"
DEST="${ROOT}/.fork/quic-go"

[ -d "$SRC" ] || { echo "module cache miss: $SRC" >&2
                   echo "run: go mod download ${MODULE}" >&2; exit 1; }

rm -rf "$DEST" && mkdir -p "$(dirname "$DEST")"
cp -R "$SRC" "$DEST"
chmod -R u+w "$DEST"   # the module cache is read-only

# Keep a pristine copy so `make patch` can regenerate the diff.
rm -rf "${ROOT}/.fork/quic-go.orig" && cp -R "$DEST" "${ROOT}/.fork/quic-go.orig"

if [ -f "${ROOT}/patches/quic-go-utls-seam.patch" ]; then
  patch -p1 -d "$DEST" < "${ROOT}/patches/quic-go-utls-seam.patch"
  echo "fork ready (patched): $DEST"
else
  echo "fork ready (UNPATCHED — patch file absent): $DEST"
fi
