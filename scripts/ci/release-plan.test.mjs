import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import {
  BRANDS, PLATFORMS, RULES, planForRef, planForMobileDispatch, toOutputs,
} from './release-plan.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const SCRIPT = path.join(ROOT, 'scripts/ci/release-plan.mjs');

const K = ['kaitu'];
const O = ['overleap'];
const KO = ['kaitu', 'overleap'];
const none = { macos: [], windows: [], linux: [], ios: [], android: [] };
const legs = (over) => ({ ...none, ...over });

// Every suffix the plan knows, plus the shapes that must plan nothing.
const TABLE = [
  ['v0.4.10', legs({ macos: KO, windows: KO, linux: K, ios: KO, android: KO }), '0.4.10'],
  ['v0.4.10-kaitu', legs({ macos: K, windows: K, linux: K, ios: K, android: K }), '0.4.10'],
  ['v0.4.10-overleap', legs({ macos: O, windows: O, ios: O, android: O }), '0.4.10'],
  ['v0.4.10-desktop', legs({ macos: K, windows: K, linux: K }), '0.4.10'],
  ['v0.4.10-mobile', legs({ ios: K, android: K }), '0.4.10'],
  ['v0.4.10-overleap-desktop', legs({ macos: O, windows: O }), '0.4.10'],
  ['v0.4.10-overleap-mobile', legs({ ios: O, android: O }), '0.4.10'],
  ['v0.4.10-macos', legs({ macos: K }), '0.4.10'],
  ['v0.4.10-windows', legs({ windows: K }), '0.4.10'],
  ['v0.4.10-linux', legs({ linux: K }), '0.4.10'],
  ['v0.4.10-ios', legs({ ios: K }), '0.4.10'],
  ['v0.4.10-android', legs({ android: K }), '0.4.10'],
  ['v0.4.10-overleap-macos', legs({ macos: O }), '0.4.10'],
  ['v0.4.10-overleap-windows', legs({ windows: O }), '0.4.10'],
  ['v0.4.10-overleap-ios', legs({ ios: O }), '0.4.10'],
  ['v0.4.10-overleap-android', legs({ android: O }), '0.4.10'],
  ['v0.4.11-beta.1-ios', legs({ ios: K }), '0.4.11-beta.1'],
  // Refs owned by other workflows / unknown suffixes → nothing.
  ['v0.4.11-beta.1', none, null],
  ['v0.4.10-k2s', none, null],
  ['v0.4.10-k2r', none, null],
  ['v0.4.10-overleap-linux', none, null],
  // Branch dispatch without a target parses the branch name like a tag.
  ['main', legs({ macos: KO, windows: KO, linux: K, ios: KO, android: KO }), null],
  ['fix/some-branch', none, null],
];

for (const [ref, expected, version] of TABLE) {
  test(`plan ${ref}`, () => {
    const plan = planForRef(ref);
    assert.deepEqual(plan.legs, expected);
    if (version !== null) assert.equal(plan.tagVersion, version);
  });
}

test('the core rule: a brand scope covers the same platforms everywhere it exists', () => {
  // For every ref, if a brand builds any desktop platform it builds all of
  // that brand's desktop platforms, and likewise for mobile — unless the
  // suffix names a single platform. Catches a future edit that re-introduces
  // "overleap on desktop but not mobile" for a multi-platform scope.
  const single = new Set(PLATFORMS.flatMap((p) => [`-${p}`, `-overleap-${p}`]));
  for (const [suffix] of RULES.filter(([s]) => !single.has(s)).concat([['', null]])) {
    const plan = planForRef(`v9.9.9${suffix}`);
    for (const brand of BRANDS) {
      const got = PLATFORMS.filter((p) => plan.legs[p].includes(brand));
      const avail = brand === 'overleap' ? ['macos', 'windows', 'ios', 'android'] : PLATFORMS;
      const desktop = got.filter((p) => ['macos', 'windows', 'linux'].includes(p));
      const mobile = got.filter((p) => ['ios', 'android'].includes(p));
      const availDesktop = avail.filter((p) => ['macos', 'windows', 'linux'].includes(p));
      if (desktop.length) assert.deepEqual(desktop, availDesktop, `${suffix} ${brand} desktop`);
      if (mobile.length) assert.deepEqual(mobile, ['ios', 'android'], `${suffix} ${brand} mobile`);
    }
  }
  const bare = planForRef('v9.9.9');
  for (const brand of BRANDS) {
    assert.ok(bare.legs.ios.includes(brand) && bare.legs.macos.includes(brand), `bare tag builds ${brand}`);
  }
});

test('no rule is shadowed by an earlier one', () => {
  for (const [suffix] of RULES) {
    assert.equal(planForRef(`v1.2.3${suffix}`).suffix, suffix, `${suffix} is shadowed`);
  }
});

test('overleap never gets a linux leg', () => {
  for (const [suffix] of RULES.concat([['', null]])) {
    assert.ok(!planForRef(`v1.2.3${suffix}`).legs.linux.includes('overleap'), suffix);
  }
});

test('mobile dispatch inputs', () => {
  assert.deepEqual(planForMobileDispatch('both', 'both').legs, legs({ ios: KO, android: KO }));
  assert.deepEqual(planForMobileDispatch('ios', 'overleap').legs, legs({ ios: O }));
  assert.deepEqual(planForMobileDispatch('android', 'kaitu').legs, legs({ android: K }));
});

test('CLI prints GITHUB_OUTPUT lines', () => {
  const out = execFileSync('node', [SCRIPT, '--ref', 'v0.4.10-overleap-mobile'], { encoding: 'utf8' });
  assert.equal(out, toOutputs(planForRef('v0.4.10-overleap-mobile')) + '\n');
  assert.match(out, /^ios_brands=\["overleap"\]$/m);
  assert.match(out, /^macos_brands=\[\]$/m);
  assert.match(out, /^tag_version=0\.4\.10$/m);
  const dispatch = execFileSync('node', [SCRIPT, '--ref', 'main', '--platform', 'ios', '--brand', 'both'], { encoding: 'utf8' });
  assert.match(dispatch, /^ios_brands=\["kaitu","overleap"\]$/m);
  assert.match(dispatch, /^android_brands=\[\]$/m);
  assert.match(dispatch, /^macos_brands=\[\]$/m);
});

// Structural guard: the tables must not be hand-copied back into the
// workflows — that duplication is how the two drifted in the first place.
test('both release workflows plan through this script and carry no suffix tables', () => {
  for (const wf of ['release-desktop.yml', 'build-mobile.yml']) {
    const src = readFileSync(path.join(ROOT, '.github/workflows', wf), 'utf8');
    assert.match(src, /node scripts\/ci\/release-plan\.mjs/, `${wf} must call release-plan.mjs`);
    assert.doesNotMatch(src, /TAG_VERSION%-/, `${wf} re-grew a hand-written suffix strip list`);
    assert.doesNotMatch(src, /\*-overleap-\w+\)/, `${wf} re-grew a bash case suffix table`);
    assert.doesNotMatch(src, /OVERLEAP_MOBILE_CI/, `${wf} re-grew a per-workflow overleap switch`);
  }
});
