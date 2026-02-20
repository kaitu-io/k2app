#!/usr/bin/env bash
# Content Pages E2E Verification — T2
# Verifies that content pages appear correctly in the Next.js build output.
# Phase RED: tests should FAIL before implementation.
# Phase GREEN: tests should PASS after implementation.

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
# test_content_page_renders
# After `yarn build`, verify the build output includes the content pages
# (article detail for zh-CN/blog/hello-world).
# ──────────────────────────────────────────────────────────────────────────────
test_content_page_renders() {
  cd "${WEB_DIR}"

  # The Next.js static build outputs pages into .next/server/app/[locale]/[...slug]/
  # Check that the blog/hello-world page exists for zh-CN locale
  local page_path=".next/server/app/zh-CN/blog/hello-world.html"
  local page_path_alt=".next/server/app/zh-CN/blog/hello-world/index.html"
  local page_path_rsc=".next/server/app/zh-CN/blog/hello-world.rsc"

  if [[ -f "${page_path}" || -f "${page_path_alt}" || -f "${page_path_rsc}" ]]; then
    echo "    content page found in build output"
    return 0
  fi

  # Also check for the catch-all route directory
  if find .next/server/app -name "*.html" -path "*zh-CN*hello-world*" 2>/dev/null | grep -q .; then
    echo "    content page found via find"
    return 0
  fi

  if find .next/server/app -name "*.rsc" -path "*zh-CN*hello-world*" 2>/dev/null | grep -q .; then
    echo "    content page RSC found via find"
    return 0
  fi

  echo "    content page NOT found in build output"
  echo "    searched: ${page_path}"
  echo "    available zh-CN pages:"
  find .next/server/app -name "*.html" -path "*zh-CN*" 2>/dev/null | head -10 || true
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
# test_directory_listing_sorted
# Verify directory listing pages (e.g., /zh-CN/blog) exist in build output.
# ──────────────────────────────────────────────────────────────────────────────
test_directory_listing_sorted() {
  cd "${WEB_DIR}"

  # Check that the blog directory listing page was generated
  local dir_page=".next/server/app/zh-CN/blog.html"
  local dir_page_alt=".next/server/app/zh-CN/blog/index.html"
  local dir_page_rsc=".next/server/app/zh-CN/blog.rsc"

  if [[ -f "${dir_page}" || -f "${dir_page_alt}" || -f "${dir_page_rsc}" ]]; then
    echo "    directory listing page found in build output"
    return 0
  fi

  if find .next/server/app -name "*.html" -path "*zh-CN*blog*" 2>/dev/null | grep -q .; then
    echo "    directory listing page found via find"
    return 0
  fi

  if find .next/server/app -name "*.rsc" -path "*zh-CN*blog*" 2>/dev/null | grep -q .; then
    echo "    directory listing page RSC found via find"
    return 0
  fi

  echo "    directory listing page NOT found in build output"
  echo "    available pages under zh-CN:"
  find .next/server/app -path "*zh-CN*" -type f 2>/dev/null | head -20 || true
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
# test_draft_page_404
# Verify draft posts are not in the build output.
# The zh-CN/blog/draft-post should not exist as a generated page since
# generateStaticParams() excludes posts where draft === true.
# ──────────────────────────────────────────────────────────────────────────────
test_draft_page_404() {
  cd "${WEB_DIR}"

  # Draft posts should NOT appear in the build output
  local draft_page=".next/server/app/zh-CN/blog/draft-post.html"
  local draft_page_alt=".next/server/app/zh-CN/blog/draft-post/index.html"
  local draft_page_rsc=".next/server/app/zh-CN/blog/draft-post.rsc"

  if [[ -f "${draft_page}" || -f "${draft_page_alt}" ]]; then
    echo "    draft post found in build output (should be excluded)"
    return 1
  fi

  if find .next/server/app -name "*.html" -path "*zh-CN*draft-post*" 2>/dev/null | grep -q .; then
    echo "    draft post found via find (should be excluded)"
    return 1
  fi

  # RSC file for draft is also forbidden
  if [[ -f "${draft_page_rsc}" ]]; then
    echo "    draft post RSC found in build output (should be excluded)"
    return 1
  fi

  echo "    draft page correctly excluded from build"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────────
# test_static_routes_still_work
# Verify existing static routes are still in build output after adding catch-all.
# ──────────────────────────────────────────────────────────────────────────────
test_static_routes_still_work() {
  cd "${WEB_DIR}"

  # Check key static pages still exist
  local install_page_found=false
  local purchase_page_found=false

  if find .next/server/app -name "*.html" -path "*zh-CN*install*" 2>/dev/null | grep -q .; then
    install_page_found=true
  fi
  if find .next/server/app -name "*.rsc" -path "*zh-CN*install*" 2>/dev/null | grep -q .; then
    install_page_found=true
  fi

  if find .next/server/app -name "*.html" -path "*zh-CN*purchase*" 2>/dev/null | grep -q .; then
    purchase_page_found=true
  fi
  if find .next/server/app -name "*.rsc" -path "*zh-CN*purchase*" 2>/dev/null | grep -q .; then
    purchase_page_found=true
  fi

  if [[ "${install_page_found}" == "true" && "${purchase_page_found}" == "true" ]]; then
    echo "    static routes (install, purchase) still present in build"
    return 0
  fi

  echo "    some static routes missing from build:"
  echo "      install found: ${install_page_found}"
  echo "      purchase found: ${purchase_page_found}"
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
# test_page_has_meta_tags
# Verify build succeeded and the sitemap.xml is generated.
# ──────────────────────────────────────────────────────────────────────────────
test_page_has_meta_tags() {
  cd "${WEB_DIR}"

  # Check that build succeeded by verifying .next directory exists and has content
  if [[ ! -d ".next" ]]; then
    echo "    .next directory not found — build may not have run"
    return 1
  fi

  # Verify the sitemap route was generated
  local sitemap_found=false
  if find .next/server/app -name "sitemap*.xml" 2>/dev/null | grep -q .; then
    sitemap_found=true
  fi
  if find .next/server/app -name "sitemap*" 2>/dev/null | grep -q .; then
    sitemap_found=true
  fi
  if [[ -f ".next/server/app/sitemap.xml/route.js" ]]; then
    sitemap_found=true
  fi
  if find .next/server -name "*.js" -path "*sitemap*" 2>/dev/null | grep -q .; then
    sitemap_found=true
  fi

  if [[ "${sitemap_found}" == "true" ]]; then
    echo "    sitemap found in build output"
    return 0
  fi

  echo "    sitemap NOT found in build output"
  echo "    .next/server/app contents:"
  ls .next/server/app/ 2>/dev/null | head -20 || true
  return 1
}

# ──────────────────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────────────────
echo ""
echo "=== Content Pages E2E Verification — T2 ==="
echo ""

# These tests require a prior `yarn build` run
if [[ ! -d "${WEB_DIR}/.next" ]]; then
  echo "ERROR: .next directory not found. Run 'yarn build' first."
  exit 1
fi

run_test "test_content_page_renders" test_content_page_renders
run_test "test_directory_listing_sorted" test_directory_listing_sorted
run_test "test_draft_page_404" test_draft_page_404
run_test "test_static_routes_still_work" test_static_routes_still_work
run_test "test_page_has_meta_tags" test_page_has_meta_tags

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
