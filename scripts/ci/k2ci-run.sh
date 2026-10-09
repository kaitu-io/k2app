#!/usr/bin/env bash
# Runs ON the k2ci test box (PVE CT 134), inside the rsynced copy of a worktree — one call per
# suite under labtest (.labtest.yml), or several from the pre-labtest `pve-test.sh --direct` path.
# Not meant to be run by hand on the Mac.
#
# Usage: bash scripts/ci/k2ci-run.sh <slug> <suite>...
#   suites: webapp web overleap api mcp k2 rust ci-scripts
#
# Each suite mirrors its ci.yml job's commands, so "green here" means what "green in CI"
# means. Differences that remain on purpose:
#   - api uses the box's resident MariaDB: labtest's per-job database ($LABTEST_MYSQL_DB, dropped
#     when the job ends), or kaitu_<slug> on the direct path — recreated empty every run either way.
#   - dependency installs are skipped when the lockfile hash is unchanged since last run.
set -uo pipefail

SLUG="$1"; shift
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
STAMPS="$ROOT/.k2ci"; mkdir -p "$STAMPS"
DB="${LABTEST_MYSQL_DB:-kaitu_$(printf '%s' "$SLUG" | tr -c 'a-zA-Z0-9_' '_')}"

# one run per worktree copy at a time (a second session on the same slug waits)
exec 9>"$STAMPS/lock"
flock -n 9 || { echo "k2ci: another run on '$SLUG' in progress — waiting"; flock 9; }

# Mirror deletions: drop files the previous sync shipped that this one no longer does.
if [ -f "$STAMPS/files.new" ]; then
  if [ -f "$STAMPS/files.list" ]; then
    comm -z -23 <(sort -z "$STAMPS/files.list") <(sort -z "$STAMPS/files.new") | xargs -0 -r rm -f --
  fi
  mv "$STAMPS/files.new" "$STAMPS/files.list"
fi

# Install deps only when the inputs changed. $1 = stamp name, $2.. = files whose content keys it.
stale() {
  local name="$1"; shift
  local want; want=$(cat "$@" 2>/dev/null | sha256sum | cut -c1-16)
  [ "$(cat "$STAMPS/$name" 2>/dev/null)" != "$want" ] && { echo "$want" > "$STAMPS/$name.next"; return 0; }
  return 1
}
fresh() { mv "$STAMPS/$1.next" "$STAMPS/$1"; }

root_deps() {
  # k2-plugin is a yarn v1 `file:` dep cached by its never-changing version: refresh the copy
  # whenever its sources change, or check-k2-plugin-fresh.sh (webapp pretest) fails here.
  if stale root-deps yarn.lock package.json webapp/package.json desktop/package.json mobile/package.json \
       $(find mobile/plugins/k2-plugin -path '*/node_modules' -prune -o -type f -print 2>/dev/null | sort); then
    rm -rf node_modules/k2-plugin
    yarn install --frozen-lockfile --force --network-timeout 600000 >/dev/null && fresh root-deps
  fi
}

suite_webapp() {
  root_deps || return 1
  ( cd webapp && yarn test ) &&
  ( cd webapp && K2_BRAND=overleap yarn test ) &&
  ( cd webapp && npx tsc --noEmit ) &&
  ( cd webapp && yarn build >/dev/null && bash scripts/check-brand-purity.sh kaitu dist ) &&
  ( cd webapp && K2_BRAND=overleap yarn build >/dev/null && bash scripts/check-brand-purity.sh overleap dist ) &&
  ( cd mobile/plugins/k2-plugin && npx tsc --noEmit ) &&
  bash scripts/check-k2-plugin-fresh.sh
}

suite_web() {
  if stale web-deps web/yarn.lock web/package.json; then
    ( cd web && yarn install --frozen-lockfile --network-timeout 600000 >/dev/null ) && fresh web-deps || return 1
  fi
  ( cd web && npx velite build >/dev/null && yarn test )
}

suite_overleap() {
  if stale overleap-deps sites/overleap/yarn.lock sites/overleap/package.json; then
    ( cd sites/overleap && yarn install --frozen-lockfile --network-timeout 600000 >/dev/null ) && fresh overleap-deps || return 1
  fi
  ( cd sites/overleap && npx eslint src tests && yarn test && yarn build >/dev/null )
}

suite_api() {
  mariadb -uroot -pci -h127.0.0.1 -e "DROP DATABASE IF EXISTS \`$DB\`; CREATE DATABASE \`$DB\` CHARACTER SET utf8mb4;" || return 1
  local cfg="$STAMPS/center-config.yml"
  sed "s#/kaitu?#/$DB?#" .github/ci/center-config.yml > "$cfg"
  grep -q "/$DB?" "$cfg" || { echo "k2ci: DSN rewrite to $DB failed" >&2; return 1; }
  bash scripts/ci/api-db-test.sh "$cfg"
}

suite_mcp() { ( cd mcp && go test ./... -count=1 ); }

suite_k2() {
  # Linux is where k2's linux-only packages (webui/, gateway/, daemon/*_linux.go) actually
  # compile — on the Mac they silently vanish from `go test ./...`.
  # TestSOMark*: need CAP_NET_ADMIN, which an unprivileged container does not have.
  ( cd k2 && go test ./... -count=1 -short -timeout=600s -skip 'SOMark|SoMark' ) &&
  ( cd k2 && go build -tags=deadlock_disable ./... )
}

suite_rust() {
  root_deps || return 1
  local triple; triple=$(rustc -vV | awk '/host/{print $2}')
  mkdir -p desktop/src-tauri/binaries && touch "desktop/src-tauri/binaries/k2-$triple"
  export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$CARGO_TARGET_DIR_BASE/$SLUG}"   # labtest injects it
  ( cd desktop/src-tauri && cargo test -- --nocapture 2>&1 | tee "$STAMPS/rust.out" ; exit "${PIPESTATUS[0]}" ) &&
  ( cd desktop/src-tauri && K2_BRAND=overleap cargo test ) &&
  ( cd mcp && go test ./... -v -count=1 > "$STAMPS/mcp.out" 2>&1 ) &&
  {
    # cross-language hardware ID gate (ci.yml test-linux-rust)
    local r g
    r=$(grep -m1 -o 'CROSS_LANG_GATE_HWID=.*' "$STAMPS/rust.out" | cut -d= -f2)
    g=$(grep -m1 -o 'CROSS_LANG_GATE_HWID=.*' "$STAMPS/mcp.out" | cut -d= -f2)
    echo "hardware ID: rust=$r go=$g"
    [ -n "$r" ] && [ "$r" = "$g" ]
  }
}

suite_ci-scripts() {
  node --test scripts/ci/web-ota-manifest.test.mjs scripts/ci/web-ota-plan.test.mjs \
    scripts/ci/web-ota-gate.test.mjs scripts/ci/release-plan.test.mjs
}

declare -a SUMMARY=()
FAILED=0
for s in "$@"; do
  if ! declare -F "suite_$s" >/dev/null; then echo "k2ci: unknown suite '$s'" >&2; FAILED=1; continue; fi
  echo; echo "════════ $s ════════"
  t0=$(date +%s)
  if "suite_$s"; then r=PASS; else r=FAIL; FAILED=1; fi
  SUMMARY+=("$(printf '%-11s %s  %4ss' "$s" "$r" "$(( $(date +%s) - t0 ))")")
done

echo; echo "════════ k2ci summary ($SLUG) ════════"
printf '%s\n' "${SUMMARY[@]}"
exit $FAILED
