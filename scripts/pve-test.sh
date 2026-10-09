#!/usr/bin/env bash
# Run this worktree's tests on the PVE test box (k2ci, CT 134) instead of the Mac.
#
#   scripts/pve-test.sh               # suites picked from what this branch changed vs main
#   scripts/pve-test.sh api webapp    # explicit suites
#   scripts/pve-test.sh all           # everything ci.yml's Linux jobs run
#   scripts/pve-test.sh --list        # show the picked suites and exit
#
# Suites: webapp web overleap api mcp k2 rust ci-scripts — each mirrors its ci.yml job
# (see scripts/ci/k2ci-run.sh). Not covered here, still Mac/CI only: test-macos (Tauri on
# macOS, darwin k2 builds), test-windows, check-versions, iOS/Android native.
#
# The working tree is rsynced as-is — uncommitted changes included, .gitignore'd files and
# .git excluded — so the box needs no GitHub credentials (the private k2/kuic submodules
# arrive as files). Each worktree gets its own copy, deps and database on the box, so
# parallel sessions don't collide. Box setup: testlab repo k2ci/.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
SLUG="$(basename "$ROOT" | tr -c 'a-zA-Z0-9_.-\n' '_')"
REMOTE_DIR="/srv/ci/work/$SLUG"
ALL_SUITES="webapp web overleap api mcp k2 rust ci-scripts"

# Tailscale direct (works away from home) → fall back to a jump through PVE.
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=5)
if ! "${SSH[@]}" ci@k2ci true 2>/dev/null; then
  SSH+=(-J pve); HOST=ci@10.10.10.34
  "${SSH[@]}" "$HOST" true || { echo "pve-test: k2ci unreachable (tailscale k2ci, or pve → 10.10.10.34)" >&2; exit 2; }
else
  HOST=ci@k2ci
fi

changed_files() {
  local base; base="$(git merge-base HEAD main 2>/dev/null || echo HEAD)"
  { git diff --name-only "$base"          # committed on this branch + uncommitted tracked
    git ls-files --others --exclude-standard; } | sort -u
}

pick_suites() {
  local f s=""
  while read -r f; do
    case "$f" in
      webapp/*|mobile/plugins/k2-plugin/*|package.json|yarn.lock) s+=" webapp" ;;
      web/*)                                    s+=" web" ;;
      sites/overleap/*)                         s+=" overleap" ;;
      api/*|contracts/*|.github/ci/center-config.yml|scripts/ci/api-db-test.sh) s+=" api" ;;
      mcp/*)                                    s+=" mcp" ;;
      k2|k2/*)                                  s+=" k2" ;;
      desktop/*)                                s+=" rust" ;;
      scripts/ci/*.mjs)                         s+=" ci-scripts" ;;
    esac
  done < <(changed_files)
  # keep ALL_SUITES order, dedupe
  local out="" x
  for x in $ALL_SUITES; do [[ " $s " == *" $x "* ]] && out+=" $x"; done
  echo "${out# }"
}

case "${1:-}" in
  --list) echo "suites: $(pick_suites)"; exit 0 ;;
  all)    SUITES="$ALL_SUITES" ;;
  "")     SUITES="$(pick_suites)" ;;
  *)      SUITES="$*" ;;
esac
if [ -z "$SUITES" ]; then
  echo "pve-test: nothing testable changed vs main (docs only?) — pass suites explicitly to force"
  exit 0
fi

echo "pve-test: $SLUG → $HOST  suites: $SUITES"
t0=$(date +%s)
# Ship exactly what git sees: tracked files (even ones a .gitignore also matches, e.g. the
# committed k2-plugin dist/) + untracked-not-ignored, submodules expanded. An rsync
# .gitignore filter would drop the tracked-but-ignored ones.
LIST="$(mktemp)"; trap 'rm -f "$LIST"' EXIT
{ git ls-files -z -c -o --exclude-standard
  # shellcheck disable=SC2016
  git submodule foreach --recursive --quiet \
    'git ls-files -z -c -o --exclude-standard | while IFS= read -r -d "" f; do printf "%s/%s\0" "$displaypath" "$f"; done'
} > "$LIST"
"${SSH[@]}" "$HOST" "mkdir -p $REMOTE_DIR/.k2ci"
rsync -a --from0 --files-from="$LIST" --ignore-missing-args -e "${SSH[*]}" ./ "$HOST:$REMOTE_DIR/"
# The box drops files that were in the previous list but not this one (deleted on the branch);
# its own state (node_modules, build output, .k2ci) was never listed, so it survives.
rsync -a -e "${SSH[*]}" "$LIST" "$HOST:$REMOTE_DIR/.k2ci/files.new"
echo "pve-test: synced $(tr -cd '\0' < "$LIST" | wc -c | tr -d ' ') files in $(( $(date +%s) - t0 ))s"

LOG="/srv/ci/logs/$SLUG-$(date +%Y%m%d-%H%M%S).log"
# shellcheck disable=SC2086
"${SSH[@]}" "$HOST" "bash -lc 'cd $REMOTE_DIR && bash scripts/ci/k2ci-run.sh $SLUG $SUITES 2>&1 | tee $LOG; exit \${PIPESTATUS[0]}'"
