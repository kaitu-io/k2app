import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { deriveVersion, readBridgeApiVersion, buildManifest } from './web-ota-manifest.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

test('deriveVersion appends commit count as 4th segment', () => {
  assert.equal(deriveVersion('0.4.8', 2845), '0.4.8.2845');
  assert.equal(deriveVersion('0.4.8', '2845'), '0.4.8.2845');
});

test('deriveVersion rejects malformed inputs', () => {
  assert.throws(() => deriveVersion('0.4.8-beta.1', 2845), /x\.y\.z/);
  assert.throws(() => deriveVersion('0.4.8', 0), /positive integer/);
  assert.throws(() => deriveVersion('0.4.8', 'abc'), /positive integer/);
});

test('readBridgeApiVersion extracts the exported const', () => {
  assert.equal(readBridgeApiVersion('export const BRIDGE_API_VERSION = 1;'), 1);
  assert.equal(readBridgeApiVersion('// doc\nexport const BRIDGE_API_VERSION = 12;\n'), 12);
  assert.throws(() => readBridgeApiVersion('export const OTHER = 1;'), /BRIDGE_API_VERSION not found/);
});

const FIXTURE = {
  version: '0.4.8.2845',
  size: 1234567,
  sha256Hex: 'a'.repeat(64),
  sigBase64: 'ZmFrZS1zaWctZm9yLXRlc3Q=',
  bridgeApiVersion: 1,
  bridgeVersions: { 1: { native: '0.4.8', desktop: '0.4.9', linux: '0.4.9' } },
  releasedAt: '2026-08-14T12:00:00Z',
};

test('buildManifest emits the exact backward-compatible shape', () => {
  const m = buildManifest(FIXTURE);
  // Legacy fields the deployed mobile natives parse — names and shapes frozen.
  assert.equal(m.version, '0.4.8.2845');
  assert.equal(m.url, '0.4.8.2845/web.zip'); // relative to the manifest's OWN dir (NOT "web/...")
  assert.equal(m.hash, `sha256:${'a'.repeat(64)}`);
  assert.equal(m.size, 1234567);
  assert.equal(m.released_at, '2026-08-14T12:00:00Z');
  assert.equal(m.min_native, '0.4.8'); // derived from bridgeVersions, never hand-written
  // New fields (additive; old natives ignore them).
  assert.equal(m.sig, 'ZmFrZS1zaWctZm9yLXRlc3Q=');
  assert.equal(m.min_bridge, 1);
  assert.equal(m.min_desktop, '0.4.9');
  assert.equal(m.min_linux, '0.4.9');
  assert.deepEqual(Object.keys(m), [
    'version', 'url', 'hash', 'size', 'released_at', 'min_native',
    'sig', 'min_bridge', 'min_desktop', 'min_linux',
  ]);
});

test('buildManifest hard-fails on gaps instead of emitting a hollow manifest', () => {
  assert.throws(() => buildManifest({ ...FIXTURE, version: '0.4.8' }), /x\.y\.z\.n/);
  assert.throws(() => buildManifest({ ...FIXTURE, bridgeApiVersion: 2 }), /no entry for bridge version 2/);
  assert.throws(
    () => buildManifest({ ...FIXTURE, bridgeVersions: { 1: { native: 'soon', desktop: '0.4.9', linux: '0.4.9' } } }),
    /native must be x\.y\.z/,
  );
  assert.throws(() => buildManifest({ ...FIXTURE, sigBase64: '' }), /sig/);
  assert.throws(() => buildManifest({ ...FIXTURE, sigBase64: 'has space' }), /sig/);
});

test('CLI `version` prints a 4-segment version derived from the repo', () => {
  const out = execFileSync('node', [path.join(here, 'web-ota-manifest.mjs'), 'version'], {
    encoding: 'utf8',
  }).trim();
  assert.match(out, /^\d+\.\d+\.\d+\.\d+$/);
});
