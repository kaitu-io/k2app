#!/usr/bin/env bash
# k2-plugin freshness gate — two drifts that both leave `tsc` and `vitest`
# type-checking the webapp against definitions nobody is shipping:
#
#  1. mobile/plugins/k2-plugin/dist/ (committed by hand) vs tsc(src/).
#     CI's `tsc --noEmit` proves src compiles, not that dist was rebuilt.
#     dist is what the webapp's tsc and the yarn `file:` install consume.
#
#  2. node_modules/k2-plugin vs mobile/plugins/k2-plugin.
#     mobile/package.json declares the plugin as `file:` — yarn v1 COPIES it
#     (no symlink) and caches the copy by name@version. The version is a
#     permanent "0.1.0", so a plain `yarn install` never refreshes the copy;
#     only `make build-k2-plugin` (rm + --force) does. During 0.4.8 prep a
#     developer copy was 6 days stale and missing confirmWebBootOk while CI
#     was green — a local `tsc` red that pointed at the wrong culprit.
#
# Wired as webapp `pretest` (so a stale local copy fails loudly with the fix
# named) and as a CI step in test-webapp-reusable.yml (where only check 1 can
# fail — the install there is always fresh).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLUGIN="$ROOT/mobile/plugins/k2-plugin"
TSC="$ROOT/node_modules/.bin/tsc"

if [ ! -x "$TSC" ]; then
  echo "ERROR: $TSC not found — run \`yarn install\` at the repo root" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

"$TSC" -p "$PLUGIN/tsconfig.json" --outDir "$TMP/dist"
if ! diff -r "$TMP/dist" "$PLUGIN/dist"; then
  echo "ERROR: mobile/plugins/k2-plugin/dist is stale relative to src." >&2
  echo "  fix: cd mobile/plugins/k2-plugin && npm run build   (then commit dist/)" >&2
  exit 1
fi

COPY="$ROOT/node_modules/k2-plugin"
if [ -d "$COPY" ]; then
  for d in src dist; do
    if ! diff -r "$PLUGIN/$d" "$COPY/$d"; then
      echo "ERROR: node_modules/k2-plugin/$d is a stale yarn file: copy of mobile/plugins/k2-plugin/$d." >&2
      echo "  fix: make build-k2-plugin   (rm -rf node_modules/k2-plugin && yarn install --force)" >&2
      exit 1
    fi
  done
fi

echo "k2-plugin fresh: dist == tsc(src); node_modules copy == plugin"
