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
# Verifies that the #velite path alias resolves and tsc --noEmit passes.
# ──────────────────────────────────────────────────────────────────────────────
test_velite_types_importable() {
  cd "${WEB_DIR}"

  # Ensure velite output exists (may have been cleaned)
  if [[ ! -d ".velite" ]]; then
    npx velite build 2>&1 || true
  fi

  # tsc --noEmit must exit 0
  if ! npx tsc --noEmit 2>&1; then
    echo "    tsc --noEmit reported errors"
    return 1
  fi

  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_invalid_frontmatter_build_fails
# Creates a temp md file missing the required `title` field, runs velite build,
# and verifies the build FAILS (reports a schema validation error).
# ──────────────────────────────────────────────────────────────────────────────
test_invalid_frontmatter_build_fails() {
  cd "${WEB_DIR}"

  # Create a temporary invalid content file (missing required `title`)
  local tmp_dir="content/zh-CN/blog"
  local tmp_file="${tmp_dir}/_invalid_test_$(date +%s).md"
  mkdir -p "${tmp_dir}"

  cat > "${tmp_file}" <<'MARKDOWN'
---
date: 2026-02-20
summary: "This file is missing the required title field"
---

Body content here.
MARKDOWN

  # Run velite build — expect it to FAIL (non-zero exit)
  local build_output
  build_output=$(npx velite build 2>&1) || true
  local build_exit=$?

  # Clean up temp file immediately
  rm -f "${tmp_file}"

  if [[ ${build_exit} -eq 0 ]]; then
    echo "    velite build succeeded but should have failed on missing title"
    return 1
  fi

  echo "    velite build correctly failed (exit ${build_exit})"
  return 0
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
