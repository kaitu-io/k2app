#!/usr/bin/env bash
# api integration suite against a real MySQL/MariaDB.
#
# `go test ./...` in api/ silently SKIPS every test guarded by skipIfNoConfig()
# when ../center/config.yml is absent — and center/ is gitignored, so that is
# every CI checkout. At 0.4.8 that was 256 of 1085 tests (subscription
# reconcile, Stripe credit, brand isolation, device auth, cloud overage…), all
# reported as a green run. This script is the CI half that actually runs them.
#
# Discriminators (a green exit must mean the DB half executed):
#   1. zero "config.yml not available" skips in -v output
#   2. a floor on top-level PASS lines — a suite that compiled half its files
#      or died early cannot pass by exiting 0
#   3. the fresh-DB migrate itself (api/migrate.go used to abort on an empty
#      database; this job is its regression test)
#
# Usage: bash scripts/ci/api-db-test.sh [config.yml]
#   default config: .github/ci/center-config.yml (expects a MariaDB at
#   127.0.0.1:3306, root:ci, database `kaitu` — see ci.yml `services:`)
# Local: point it at your own center config, or run against the dev container
#   with the default file (same DSN shape as the shared dev instance).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

CFG="${1:-.github/ci/center-config.yml}"
# Floor = ~90% of the top-level `--- PASS` count observed on the 0.4.8
# candidate with the DB present: 687 (fresh MariaDB 10.6, 0 config-skips —
# see docs/release-verification-v0.4.8.md). Subtests are indented and not
# counted. Raise it when the suite grows; never lower it to make a run pass.
PASS_FLOOR="${API_DB_PASS_FLOOR:-620}"

if [ ! -f "$CFG" ]; then
  echo "ERROR: config $CFG not found" >&2
  exit 1
fi

mkdir -p center
cp "$CFG" center/config.yml

echo "=== migrate (fresh schema) ==="
( cd api/cmd && go run . migrate -c ../../center/config.yml )

echo "=== go test ./... -count=1 -v (real DB) ==="
LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT
set +e
( cd api && go test ./... -count=1 -v ) > "$LOG" 2>&1
status=$?
set -e

# Package-level summary lines and any top-level failure/panic, without the
# full -v stream (that stays in the log for the failure tail below).
grep -E '^(ok|FAIL|--- FAIL|panic:)' "$LOG" || true

SKIPPED=$(grep -c 'config.yml not available' "$LOG" || true)
PASSED=$(grep -c '^--- PASS' "$LOG" || true)
echo "api DB suite: exit=$status top-level-pass=$PASSED config-skips=$SKIPPED (floor $PASS_FLOOR)"

if [ "$SKIPPED" != "0" ]; then
  echo "ERROR: $SKIPPED test(s) skipped for missing config — the DB half of the suite did not run" >&2
  exit 1
fi
if [ "$PASSED" -lt "$PASS_FLOOR" ]; then
  echo "ERROR: only $PASSED top-level tests passed (floor $PASS_FLOOR) — the suite did not run to completion" >&2
  tail -100 "$LOG" >&2
  exit 1
fi
if [ "$status" != "0" ]; then
  echo "--- failure tail ---" >&2
  grep -n -B2 -A25 -E '^--- FAIL|^panic:' "$LOG" | tail -200 >&2
  exit "$status"
fi
echo "api DB suite OK"
