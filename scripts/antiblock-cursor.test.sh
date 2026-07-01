#!/usr/bin/env bash
# Test for antiblock-cursor.sh. Run: bash scripts/antiblock-cursor.test.sh
# Proves the cursor computation the publish workflow relies on for immutability:
# numeric (not lexical) max, empty-dir default, junk filtering, and that a retry
# re-read after a concurrent push advances past the collision.

set -u
HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=scripts/antiblock-cursor.sh
. "$HERE/antiblock-cursor.sh"

PASS=0; FAIL=0

# expect_cursor <label> <dist_dir> <want>
expect_cursor() {
  local label="$1" dir="$2" want="$3" got
  got=$(compute_cursor "$dir")
  if [ "$got" = "$want" ]; then
    echo "  [PASS] $label"
    PASS=$((PASS + 1))
  else
    echo "  [FAIL] $label — got $got want $want"
    FAIL=$((FAIL + 1))
  fi
}

ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT
echo
echo "--- antiblock-cursor.sh tests ---"
echo

# no v/ dir → 1
W="$ROOT/a"; mkdir -p "$W"
expect_cursor "no v/ dir → 1" "$W" 1

# empty v/ → 1
W="$ROOT/b"; mkdir -p "$W/v"
expect_cursor "empty v/ → 1" "$W" 1

# numeric-sort trap: 1,2,9,10 → 11 (lexical would rank 9 above 10)
W="$ROOT/c"; mkdir -p "$W/v"; for n in 1 2 9 10; do : > "$W/v/$n.js"; done
expect_cursor "1,2,9,10 → 11 (numeric sort)" "$W" 11

# three-digit boundary: 99,100,101 → 102
W="$ROOT/d"; mkdir -p "$W/v"; for n in 1 5 99 100 101; do : > "$W/v/$n.js"; done
expect_cursor "...99,100,101 → 102" "$W" 102

# junk filtering: config.js / abc.js / 4.js.bak ignored, max(3,5)+1=6
W="$ROOT/e"; mkdir -p "$W/v"
: > "$W/v/3.js"; : > "$W/v/config.js"; : > "$W/v/abc.js"; : > "$W/v/4.js.bak"; : > "$W/v/5.js"
expect_cursor "junk filtered → 6" "$W" 6

# retry convergence: two runs see max=5 → both want 6; after A's push lands v/6.js,
# B's retry re-reads and advances to 7 (no same-N collision).
W="$ROOT/f"; mkdir -p "$W/v"; for n in 1 2 3 4 5; do : > "$W/v/$n.js"; done
A=$(compute_cursor "$W")
B1=$(compute_cursor "$W")
: > "$W/v/$A.js"
B2=$(compute_cursor "$W")
if [ "$A" = "6" ] && [ "$B1" = "6" ] && [ "$B2" = "7" ]; then
  echo "  [PASS] race: A=6,B=6 then retry→7"
  PASS=$((PASS + 1))
else
  echo "  [FAIL] race — A=$A B1=$B1 B2=$B2"
  FAIL=$((FAIL + 1))
fi

echo
echo "  $PASS passed, $FAIL failed"
echo
[ "$FAIL" -eq 0 ]
