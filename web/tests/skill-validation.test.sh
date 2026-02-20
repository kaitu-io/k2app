#!/usr/bin/env bash
# T3 - Publish Content Skill Validation Tests
# Tests that .claude/skills/publish-content/SKILL.md exists and is well-formed.
# Phase RED: all tests should FAIL before the skill file is created.
# Phase GREEN: all tests should PASS after the skill file is created.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SKILL_FILE="${REPO_ROOT}/.claude/skills/publish-content/SKILL.md"

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
# test_skill_file_exists
# Verify that .claude/skills/publish-content/SKILL.md exists.
# ──────────────────────────────────────────────────────────────────────────────
test_skill_file_exists() {
  if [[ ! -f "${SKILL_FILE}" ]]; then
    echo "    SKILL.md not found at: ${SKILL_FILE}"
    return 1
  fi
  echo "    found: ${SKILL_FILE}"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_skill_frontmatter_valid
# Verify SKILL.md starts with YAML frontmatter containing `name:` and
# `description:` fields.
# ──────────────────────────────────────────────────────────────────────────────
test_skill_frontmatter_valid() {
  if [[ ! -f "${SKILL_FILE}" ]]; then
    echo "    SKILL.md not found — cannot validate frontmatter"
    return 1
  fi

  # File must begin with ---
  local first_line
  first_line=$(head -1 "${SKILL_FILE}")
  if [[ "${first_line}" != "---" ]]; then
    echo "    SKILL.md does not start with YAML frontmatter (expected '---')"
    return 1
  fi

  # Must contain name: field
  if ! grep -q "^name:" "${SKILL_FILE}"; then
    echo "    frontmatter missing 'name:' field"
    return 1
  fi

  # Must contain description: field
  if ! grep -q "^description:" "${SKILL_FILE}"; then
    echo "    frontmatter missing 'description:' field"
    return 1
  fi

  echo "    frontmatter valid: name and description present"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_skill_references_schema
# Verify SKILL.md documents the content frontmatter schema fields:
# title, date, summary, tags, coverImage, draft
# ──────────────────────────────────────────────────────────────────────────────
test_skill_references_schema() {
  if [[ ! -f "${SKILL_FILE}" ]]; then
    echo "    SKILL.md not found — cannot validate schema references"
    return 1
  fi

  local missing=()

  for field in title date summary tags coverImage draft; do
    if ! grep -q "${field}" "${SKILL_FILE}"; then
      missing+=("${field}")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "    SKILL.md missing schema field references: ${missing[*]}"
    return 1
  fi

  echo "    all schema fields present: title, date, summary, tags, coverImage, draft"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────────────────
echo ""
echo "=== T3 Publish Content Skill — Validation Tests ==="
echo ""

run_test "test_skill_file_exists" test_skill_file_exists
run_test "test_skill_frontmatter_valid" test_skill_frontmatter_valid
run_test "test_skill_references_schema" test_skill_references_schema

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
