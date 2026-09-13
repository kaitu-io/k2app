#!/usr/bin/env node
// Release plan: which brand × platform legs a release ref builds.
//
// Single source of truth for BOTH release-desktop.yml and build-mobile.yml.
// The two workflows used to carry hand-mirrored bash `case` tables and they
// drifted: bare v* built overleap on mobile only behind a repo variable
// (OVERLEAP_MOBILE_CI), `-overleap` meant "overleap desktop" while mobile
// skipped it, and overleap 0.4.10 ended up with desktop live and iOS/Android
// built from a commit 50 behind kaitu 0.4.10. Rule now:
//
//   A brand scope means the same thing on every platform.
//
//   ref suffix           kaitu legs                 overleap legs
//   (bare)               macos windows linux        macos windows
//                        ios android                ios android
//   -kaitu               macos windows linux ios android   —
//   -overleap            —                          macos windows ios android
//   -desktop             macos windows linux        —
//   -mobile              ios android                —
//   -overleap-desktop    —                          macos windows
//   -overleap-mobile     —                          ios android
//   -macos -windows -linux -ios -android            (kaitu, one platform)
//   -overleap-macos -overleap-windows -overleap-ios -overleap-android
//   -overleap-linux      nothing (overleap has no linux channel)
//   any other `-…`       nothing (the ref belongs to another workflow)
//
// Linux has no overleap channel (scripts/ci/upload-release.sh refuses it), so
// overleap never gets a linux leg — that is a platform that does not exist
// for the brand, not an inconsistency. Unprefixed platform suffixes stay
// kaitu, as they always were.
//
// Uploads are not releases: every leg only puts versioned artifacts on the
// CDN / App Store Connect / the run. Update pointers (publish-desktop.sh,
// publish-mobile.sh), App Review submission and the Play Console stay manual.
//
// Usage (prints KEY=value lines; append to $GITHUB_OUTPUT):
//   node scripts/ci/release-plan.mjs --ref <ref_name>
//   node scripts/ci/release-plan.mjs --ref <ref_name> --platform ios|android|both --brand kaitu|overleap|both
//     (the second form is build-mobile's workflow_dispatch: explicit inputs, ref ignored for legs)

import { fileURLToPath } from 'node:url';

export const BRANDS = ['kaitu', 'overleap'];
export const PLATFORMS = ['macos', 'windows', 'linux', 'ios', 'android'];
const DESKTOP = ['macos', 'windows', 'linux'];
const MOBILE = ['ios', 'android'];

// Platforms a brand can ship on at all.
const AVAILABLE = {
  kaitu: PLATFORMS,
  overleap: ['macos', 'windows', 'ios', 'android'],
};

// Ordered: the first suffix the ref ends with wins. `-overleap-ios` also ends
// with `-ios`, so every overleap form sits above every kaitu form; the
// exhaustive test in release-plan.test.mjs fails if a later rule is shadowed.
export const RULES = [
  ['-overleap-desktop', { overleap: DESKTOP }],
  ['-overleap-mobile', { overleap: MOBILE }],
  ['-overleap-macos', { overleap: ['macos'] }],
  ['-overleap-windows', { overleap: ['windows'] }],
  ['-overleap-ios', { overleap: ['ios'] }],
  ['-overleap-android', { overleap: ['android'] }],
  // Not a platform overleap has; listed so it plans nothing instead of
  // falling through to `-linux` and building kaitu.
  ['-overleap-linux', { overleap: ['linux'] }],
  ['-overleap', { overleap: PLATFORMS }],
  ['-kaitu', { kaitu: PLATFORMS }],
  ['-desktop', { kaitu: DESKTOP }],
  ['-mobile', { kaitu: MOBILE }],
  ['-macos', { kaitu: ['macos'] }],
  ['-windows', { kaitu: ['windows'] }],
  ['-linux', { kaitu: ['linux'] }],
  ['-ios', { kaitu: ['ios'] }],
  ['-android', { kaitu: ['android'] }],
];

const BARE = { kaitu: PLATFORMS, overleap: PLATFORMS };

function legsFrom(scope) {
  const legs = Object.fromEntries(PLATFORMS.map((p) => [p, []]));
  for (const brand of BRANDS) {
    for (const p of scope[brand] ?? []) {
      if (AVAILABLE[brand].includes(p)) legs[p].push(brand);
    }
  }
  return legs;
}

// → { legs: {macos:[brands]…}, suffix, tagVersion }
// tagVersion is the ref minus a leading `v` and minus the matched suffix; it
// is only meaningful for tag refs (the workflows compare it to package.json).
export function planForRef(refName) {
  const stripV = refName.startsWith('v') ? refName.slice(1) : refName;
  for (const [suffix, scope] of RULES) {
    if (refName.endsWith(suffix)) {
      return { legs: legsFrom(scope), suffix, tagVersion: stripV.slice(0, -suffix.length) };
    }
  }
  if (refName.includes('-')) {
    return { legs: legsFrom({}), suffix: null, tagVersion: stripV };
  }
  return { legs: legsFrom(BARE), suffix: '', tagVersion: stripV };
}

export function planForMobileDispatch(platform, brand) {
  const brands = brand === 'both' ? BRANDS : BRANDS.includes(brand) ? [brand] : ['kaitu'];
  const platforms = platform === 'both' ? MOBILE : MOBILE.includes(platform) ? [platform] : [];
  return { legs: legsFrom(Object.fromEntries(brands.map((b) => [b, platforms]))) };
}

export function toOutputs(plan) {
  const lines = PLATFORMS.map((p) => `${p}_brands=${JSON.stringify(plan.legs[p])}`);
  if (plan.tagVersion !== undefined) lines.push(`tag_version=${plan.tagVersion}`);
  return lines.join('\n');
}

function main(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 2) {
    if (!argv[i].startsWith('--') || argv[i + 1] === undefined) {
      throw new Error(`bad arguments: ${argv.join(' ')}`);
    }
    args[argv[i].slice(2)] = argv[i + 1];
  }
  if (args.ref === undefined) throw new Error('--ref is required');
  let plan = planForRef(args.ref);
  if (args.platform !== undefined || args.brand !== undefined) {
    plan = { ...planForMobileDispatch(args.platform ?? 'both', args.brand ?? 'both'), tagVersion: plan.tagVersion };
  }
  process.stdout.write(toOutputs(plan) + '\n');
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (e) {
    process.stderr.write(`release-plan: ${e.message}\n`);
    process.exit(2);
  }
}
