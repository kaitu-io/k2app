#!/usr/bin/env node
// Web OTA release plan: which brand(s) a `webapp/*` tag (or a dispatch) publishes.
//
// The webapp is shared by two brands (kaitu, overleap) but they are released
// INDEPENDENTLY: a `webapp/x.y.z` tag ships both, a `webapp/x.y.z-kaitu` /
// `webapp/x.y.z-overleap` tag ships one. This is the same `-kaitu` / `-overleap`
// / bare=both scoping that scripts/ci/release-plan.mjs uses for app releases, so
// a brand scope means the same thing for a web OTA tag as for an app tag.
//
// Kept SEPARATE from release-plan.mjs on purpose: that script parses `v*` tags
// into platform×brand legs; a webapp OTA tag has no platform dimension and a
// different `webapp/` prefix, so reusing it would mean forcing a platform tool
// onto a platformless problem. The two share a naming convention, not code.
//
// The trigger is `webapp/*` alone (no other workflow owns that namespace), so an
// unrecognised suffix is NOT "belongs to someone else" as it is in release-plan;
// it is just a bare tag whose version the workflow's package.json check guards.
//
// Usage (prints KEY=value lines; append to $GITHUB_OUTPUT):
//   node scripts/ci/web-ota-plan.mjs --ref webapp/0.4.12          (push)
//   node scripts/ci/web-ota-plan.mjs --brand kaitu|overleap|both  (workflow_dispatch)

import { fileURLToPath } from 'node:url';

export const BRANDS = ['kaitu', 'overleap'];

// Ordered brand suffixes on a webapp/ tag body; bare = both brands. A tag body
// can end with at most one of these, so order is not load-bearing — but the
// exhaustive test pins the behaviour so it cannot drift.
export const SUFFIX_RULES = [
  ['-kaitu', ['kaitu']],
  ['-overleap', ['overleap']],
];

// tag ref → { brands, tagVersion }. tagVersion is the ref minus a leading
// `webapp/` and minus the matched brand suffix; it is only meaningful for tag
// refs (the workflow compares it to package.json — the version source of truth).
export function planForRef(refName) {
  const body = refName.startsWith('webapp/') ? refName.slice('webapp/'.length) : refName;
  for (const [suffix, brands] of SUFFIX_RULES) {
    if (body.endsWith(suffix)) return { brands: [...brands], tagVersion: body.slice(0, -suffix.length) };
  }
  return { brands: [...BRANDS], tagVersion: body };
}

// workflow_dispatch `brand` input → brand list. An unknown value throws rather
// than silently publishing nothing (empty list) or everything (both) — a typo'd
// brand must fail the run, not quietly ship the wrong audience.
export function planForDispatch(brand) {
  if (brand === undefined || brand === '' || brand === 'both') return [...BRANDS];
  if (BRANDS.includes(brand)) return [brand];
  throw new Error(`unknown brand: ${brand} (expected kaitu | overleap | both)`);
}

// Space-separated brand list so the workflow's `for BRAND in ${brands}` loop and
// the gate's `--brands` arg (which splits on comma OR whitespace) both consume
// it directly, with no JSON parsing in bash.
export function toOutputs({ brands, tagVersion }) {
  const lines = [`brands=${brands.join(' ')}`];
  if (tagVersion !== undefined) lines.push(`tag_version=${tagVersion}`);
  return lines.join('\n');
}

// { ref?, brand? } → { brands, tagVersion? }. Mirrors release-plan.mjs: the ref
// (if given) always supplies tagVersion, and an explicit `brand` OVERRIDES the
// brand set on top. So a workflow_dispatch on a `webapp/*` tag (which passes
// both) still validates the tag version while letting the operator pick brands.
export function resolve({ ref, brand }) {
  if (ref === undefined && brand === undefined) {
    throw new Error('one of --ref or --brand is required');
  }
  const plan = ref !== undefined ? planForRef(ref) : { brands: [...BRANDS] };
  if (brand !== undefined) plan.brands = planForDispatch(brand);
  return plan;
}

function arg(argv, name) {
  const i = argv.indexOf(`--${name}`);
  return i >= 0 && i + 1 < argv.length ? argv[i + 1] : undefined;
}

function main(argv) {
  process.stdout.write(toOutputs(resolve({ ref: arg(argv, 'ref'), brand: arg(argv, 'brand') })) + '\n');
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (e) {
    process.stderr.write(`web-ota-plan: ${e.message}\n`);
    process.exit(2);
  }
}
