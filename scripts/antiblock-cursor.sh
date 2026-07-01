#!/usr/bin/env bash
# antiblock-cursor.sh — compute the next antiblock seed cursor for a ui-theme
# `dist` checkout. Single source of truth shared by the publish workflow
# (.github/workflows/publish-antiblock.yml) and its test (antiblock-cursor.test.sh).
#
# Next cursor = (numeric max of v/<N>.js filenames) + 1, or 1 when none exist.
# `sort -n` is load-bearing: lexical sort would rank "9" above "10". Non-numeric
# filenames (config.js, *.js.bak, abc.js) are filtered out.
#
# APPEND-ONLY INVARIANT: this script only READS. The published v/<N>.js files are
# immutable — callers must NEVER delete or overwrite an existing v/*.js. That
# invariant is what makes per-mirror CDN staleness graceful instead of a brick.
#
# Usage (direct):  antiblock-cursor.sh <dist_dir>   → prints the next cursor
# Usage (sourced): source antiblock-cursor.sh; compute_cursor <dist_dir>

compute_cursor() {
  dist_dir="$1"
  cursor=1
  if [ -d "$dist_dir/v" ]; then
    max_n=$(find "$dist_dir/v" -maxdepth 1 -name '*.js' \
      | sed 's|.*/||; s/\.js$//' \
      | grep -E '^[0-9]+$' \
      | sort -n \
      | tail -1 || true)
    [ -n "$max_n" ] && cursor=$((max_n + 1))
  fi
  printf '%s\n' "$cursor"
}

# Execute only when run directly, not when sourced.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  if [ "$#" -ne 1 ]; then
    echo "usage: $0 <dist_dir>" >&2
    exit 2
  fi
  compute_cursor "$1"
fi
