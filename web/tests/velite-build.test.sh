#!/usr/bin/env bash
# Velite Foundation Setup — Integration Test Suite
# Tests run against the web/ directory.
# Phase RED: all tests should FAIL before velite is installed/configured.
# Phase GREEN: all tests should PASS after implementation.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PASS=0
FAIL=0
ERRORS=()

run_test() {
  local name="$1"
  local fn="$2"
  echo "  running: ${name}"
  if ${fn}; then
    echo "  [PASS] ${name}"
    PASS=$((PASS + 1))
  else
    echo "  [FAIL] ${name}"
    FAIL=$((FAIL + 1))
    ERRORS+=("${name}")
  fi
}

# ──────────────────────────────────────────────────────────────────────────────
# test_velite_build_succeeds
# Runs `npx velite build` inside web/ and verifies .velite/ output is generated.
# ──────────────────────────────────────────────────────────────────────────────
test_velite_build_succeeds() {
  cd "${WEB_DIR}"

  # Remove any previous output so we start clean
  rm -rf .velite

  # Run velite build — must succeed (exit 0)
  if ! npx velite build 2>&1; then
    echo "    velite build command failed"
    return 1
  fi

  # .velite/ directory must exist
  if [[ ! -d ".velite" ]]; then
    echo "    .velite/ directory not created"
    return 1
  fi

  # At minimum an index.js or index.d.ts or posts.json must be present
  local output_files
  output_files=$(find .velite -maxdepth 1 -type f | wc -l | tr -d ' ')
  if [[ "${output_files}" -eq 0 ]]; then
    echo "    .velite/ directory is empty"
    return 1
  fi

  echo "    .velite/ created with ${output_files} file(s)"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_velite_types_importable
# Verifies that:
#   1. The #velite path alias is configured in tsconfig.json
#   2. The .velite/index.d.ts type file is generated with the Post type
#   3. tsc introduces no NEW errors beyond the pre-existing auth.test.ts ones
# ──────────────────────────────────────────────────────────────────────────────
test_velite_types_importable() {
  cd "${WEB_DIR}"

  # Ensure velite output exists (may have been cleaned)
  if [[ ! -d ".velite" ]]; then
    npx velite build 2>&1 || true
  fi

  # 1. tsconfig.json must declare #velite path alias
  if ! grep -q '"#velite"' tsconfig.json; then
    echo "    #velite path alias not found in tsconfig.json"
    return 1
  fi
  echo "    #velite alias present in tsconfig.json"

  # 2. .velite/index.d.ts must exist and export Post type
  if [[ ! -f ".velite/index.d.ts" ]]; then
    echo "    .velite/index.d.ts not found"
    return 1
  fi
  if ! grep -q "export type Post" .velite/index.d.ts; then
    echo "    Post type not found in .velite/index.d.ts"
    return 1
  fi
  echo "    .velite/index.d.ts exports Post type"

  # 3. Run tsc and verify that errors are ONLY in pre-existing files
  #    (auth.test.ts has pre-existing errors unrelated to this feature)
  local tsc_output
  tsc_output=$(npx tsc --noEmit 2>&1) || true

  # Filter out known pre-existing errors
  local new_errors
  new_errors=$(echo "${tsc_output}" | grep "^src/" | grep -v "src/lib/__tests__/auth.test.ts") || true

  if [[ -n "${new_errors}" ]]; then
    echo "    tsc found NEW errors introduced by this feature:"
    echo "${new_errors}"
    return 1
  fi

  echo "    tsc: no new errors (only pre-existing auth.test.ts errors)"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_invalid_frontmatter_build_fails
# Creates a temp md file missing the required `title` field, runs velite build,
# and verifies the build reports a schema validation error for `title`.
#
# Note: velite exits 0 even with validation errors (by design), but it
# outputs the error to stdout. We check for the error message in the output.
# ──────────────────────────────────────────────────────────────────────────────
test_invalid_frontmatter_build_fails() {
  cd "${WEB_DIR}"

  # Create a temporary invalid content file (missing required `title`).
  # Must NOT start with underscore — velite skips underscore files.
  local tmp_dir="content/zh-CN/blog"
  local tmp_file="${tmp_dir}/invalid-frontmatter-test.md"
  mkdir -p "${tmp_dir}"

  cat > "${tmp_file}" <<'MARKDOWN'
---
date: 2026-02-20
summary: "This file is missing the required title field"
---

Body content here.
MARKDOWN

  # Run velite build — capture output (velite exits 0 but reports issues)
  local build_output
  build_output=$(npx velite build 2>&1)

  # Clean up temp file immediately
  rm -f "${tmp_file}"

  # Velite should report the title Required error in its output
  if echo "${build_output}" | grep -q "title"; then
    echo "    velite correctly reported title validation error"
    return 0
  fi

  echo "    velite did NOT report title validation error; output was:"
  echo "${build_output}"
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────────────────
echo ""
echo "=== Velite Foundation — Integration Tests ==="
echo ""

run_test "test_velite_build_succeeds" test_velite_build_succeeds
run_test "test_velite_types_importable" test_velite_types_importable
run_test "test_invalid_frontmatter_build_fails" test_invalid_frontmatter_build_fails

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

if [[ ${FAIL} -gt 0 ]]; then
  echo "Failed tests:"
  for err in "${ERRORS[@]}"; do
    echo "  - ${err}"
  done
  exit 1
fi

exit 0
