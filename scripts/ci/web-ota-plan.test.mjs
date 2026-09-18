// Tests for the Web OTA release plan (scripts/ci/web-ota-plan.mjs).
//
// A `webapp/*` tag decides which brand(s) it publishes the same way an app
// release tag does (scripts/ci/release-plan.mjs): a bare tag is BOTH brands, a
// `-kaitu` / `-overleap` suffix narrows it to one. The table is asserted
// exhaustively so widening or renaming a scope has to mean editing this file on
// purpose.
//
// Run: node --test scripts/ci/web-ota-plan.test.mjs

import { test } from 'node:test';
import assert from 'node:assert/strict';

import { BRANDS, planForRef, planForDispatch, resolve, toOutputs } from './web-ota-plan.mjs';

// -------------------------------------------------------------- tag refs ---

test('a bare webapp/ tag publishes BOTH brands', () => {
  assert.deepEqual(planForRef('webapp/0.4.12'), { brands: ['kaitu', 'overleap'], tagVersion: '0.4.12' });
});

test('a -kaitu / -overleap suffix narrows to that brand', () => {
  assert.deepEqual(planForRef('webapp/0.4.12-kaitu'), { brands: ['kaitu'], tagVersion: '0.4.12' });
  assert.deepEqual(planForRef('webapp/0.4.12-overleap'), { brands: ['overleap'], tagVersion: '0.4.12' });
});

test('tagVersion strips exactly the webapp/ prefix and the matched brand suffix', () => {
  // A patch-level version with the suffix — the whole suffix, nothing more.
  assert.equal(planForRef('webapp/1.2.3-overleap').tagVersion, '1.2.3');
  // Nothing to strip on a bare tag.
  assert.equal(planForRef('webapp/1.2.3').tagVersion, '1.2.3');
});

test('brand suffix match is unambiguous regardless of rule order', () => {
  // A string can only end with one of the two brand suffixes; the last segment
  // wins, deterministically.
  assert.deepEqual(planForRef('webapp/0.4.12-kaitu-overleap').brands, ['overleap']);
  assert.deepEqual(planForRef('webapp/0.4.12-overleap-kaitu').brands, ['kaitu']);
});

test('an unknown suffix is NOT a brand scope — it falls through to both brands', () => {
  // Unlike release-plan.mjs (where an unknown suffix belongs to another
  // workflow), a webapp/ tag has no other owner: the trigger is `webapp/*`
  // alone. A tag we do not recognise as brand-scoped is treated as bare/both,
  // and the workflow's package.json version check is the real guard.
  assert.deepEqual(planForRef('webapp/0.4.12-rc1').brands, ['kaitu', 'overleap']);
});

// -------------------------------------------------------------- dispatch ---

test('dispatch: both / empty / undefined → both brands', () => {
  assert.deepEqual(planForDispatch('both'), ['kaitu', 'overleap']);
  assert.deepEqual(planForDispatch(''), ['kaitu', 'overleap']);
  assert.deepEqual(planForDispatch(undefined), ['kaitu', 'overleap']);
});

test('dispatch: a single brand narrows', () => {
  assert.deepEqual(planForDispatch('kaitu'), ['kaitu']);
  assert.deepEqual(planForDispatch('overleap'), ['overleap']);
});

test('dispatch: an unknown brand throws rather than publishing nothing (or everything)', () => {
  assert.throws(() => planForDispatch('kaito'), /unknown brand/);
  assert.throws(() => planForDispatch('both,kaitu'), /unknown brand/);
});

// --------------------------------------------------------------- resolve ---
// resolve() is what the CLI runs: ref supplies the version, an explicit brand
// overrides the brand set. This is the seam the `plan` job depends on.

test('resolve: a push tag comes straight from the ref (brands + version)', () => {
  assert.deepEqual(resolve({ ref: 'webapp/0.4.12' }), { brands: ['kaitu', 'overleap'], tagVersion: '0.4.12' });
  assert.deepEqual(resolve({ ref: 'webapp/0.4.12-overleap' }), { brands: ['overleap'], tagVersion: '0.4.12' });
});

test('resolve: a dispatch with only --brand has no version to validate', () => {
  assert.deepEqual(resolve({ brand: 'kaitu' }), { brands: ['kaitu'] });
  assert.equal(resolve({ brand: 'both' }).tagVersion, undefined);
});

test('resolve: a dispatch ON a webapp/ tag keeps the version AND lets --brand override', () => {
  // The regression this guards: dispatching the workflow on `webapp/0.4.12`
  // must still surface tagVersion=0.4.12 (so the package.json check runs) while
  // the operator's brand choice wins over the tag's own scope.
  assert.deepEqual(resolve({ ref: 'webapp/0.4.12', brand: 'overleap' }), {
    brands: ['overleap'],
    tagVersion: '0.4.12',
  });
  assert.deepEqual(resolve({ ref: 'webapp/0.4.12-kaitu', brand: 'both' }), {
    brands: ['kaitu', 'overleap'],
    tagVersion: '0.4.12',
  });
});

test('resolve: neither --ref nor --brand is an error, not a silent both-brands publish', () => {
  assert.throws(() => resolve({}), /one of --ref or --brand/);
});

// ---------------------------------------------------------------- output ---

test('toOutputs emits a space-separated brand list the bash for-loop consumes', () => {
  assert.equal(toOutputs(planForRef('webapp/0.4.12')), 'brands=kaitu overleap\ntag_version=0.4.12');
  assert.equal(toOutputs(planForRef('webapp/0.4.12-overleap')), 'brands=overleap\ntag_version=0.4.12');
});

test('toOutputs omits tag_version on a dispatch (no tag to validate)', () => {
  assert.equal(toOutputs({ brands: planForDispatch('kaitu') }), 'brands=kaitu');
});

test('BRANDS is the canonical pair and callers get copies, not the shared array', () => {
  assert.deepEqual(BRANDS, ['kaitu', 'overleap']);
  planForRef('webapp/0.4.12').brands.push('mutant');
  assert.deepEqual(BRANDS, ['kaitu', 'overleap'], 'planForRef must not hand out the module array');
  planForDispatch('both').push('mutant');
  assert.deepEqual(BRANDS, ['kaitu', 'overleap'], 'planForDispatch must not hand out the module array');
});
