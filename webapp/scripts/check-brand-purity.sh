#!/usr/bin/env bash
# Brand purity guard (spec: docs/superpowers/specs/2026-07-14-brand-split-design.md
# §错误处理与测试要点). Text files only (-I skips binaries).
#   kaitu artifact    must contain zero: overleap.io
#   overleap artifact must contain zero: kaitu.io | 开途 | 開途
# Bare "kaitu" tokens (X-K2-Client product token, localStorage key) are
# protocol identifiers, intentionally NOT matched.
set -euo pipefail

brand="${1:?usage: check-brand-purity.sh <kaitu|overleap> [dist-dir]}"
dist="${2:-dist}"

case "$brand" in
  kaitu)    pattern='overleap\.io' ;;
  overleap) pattern='kaitu\.io|开途|開途' ;;
  *) echo "unknown brand: $brand" >&2; exit 2 ;;
esac

matches=$(grep -rEliI "$pattern" "$dist" || true)
if [ -n "$matches" ]; then
  echo "BRAND PURITY VIOLATION ($brand build):" >&2
  echo "$matches" >&2
  # shellcheck disable=SC2086
  grep -rEnoiI "$pattern" $matches | head -40 >&2
  exit 1
fi
echo "brand purity OK ($brand, $dist)"
