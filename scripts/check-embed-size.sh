#!/usr/bin/env bash
# Fail if the COMMITTED k2 embed archive exceeds the placeholder budget.
# The build-fetched full archive (~2MB) is transient and must never be committed;
# the tracked copy is the small cn-baseline placeholder. Checks HEAD's blob size,
# immune to a working-tree overwrite from `make fetch-rules-embed`.
set -euo pipefail
MAX=307200   # 300 KB
PATH_IN_K2="rule/embed/all.krs.tar.gz"

size=$(git -C k2 cat-file -s "HEAD:${PATH_IN_K2}" 2>/dev/null || echo "missing")
if [ "$size" = "missing" ]; then
  echo "check-embed-size: ${PATH_IN_K2} not committed in k2 HEAD" >&2
  exit 1
fi
if [ "$size" -gt "$MAX" ]; then
  echo "check-embed-size: FAIL committed embed is ${size} bytes (> ${MAX})." >&2
  echo "  The 2MB build-fetched archive was committed by mistake. Restore the" >&2
  echo "  placeholder: git -C k2 checkout -- ${PATH_IN_K2}" >&2
  exit 1
fi
echo "check-embed-size: OK (${size} bytes <= ${MAX})"
