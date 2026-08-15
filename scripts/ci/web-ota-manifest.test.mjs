import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  WEB_OTA_EPOCH_MS, computeBuildNumber, deriveVersion,
  readBridgeApiVersion, readSupportFloor, buildManifest,
} from './web-ota-manifest.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));

test('computeBuildNumber is seconds since the 2026-01-01 epoch', () => {
  assert.equal(computeBuildNumber(WEB_OTA_EPOCH_MS + 1000), 1);
  assert.equal(computeBuildNumber(WEB_OTA_EPOCH_MS + 19_526_400_500), 19_526_400); // floor, not round
});

test('computeBuildNumber rejects a clock at/before the epoch', () => {
  assert.throws(() => computeBuildNumber(WEB_OTA_EPOCH_MS), /positive integer/);
  assert.throws(() => computeBuildNumber(WEB_OTA_EPOCH_MS - 5000), /positive integer/);
});

test('deriveVersion appends the build number as 4th segment', () => {
  assert.equal(deriveVersion('0.4.9', 19526400), '0.4.9.19526400');
  assert.equal(deriveVersion('0.4.9', '19526400'), '0.4.9.19526400');
});

test('deriveVersion rejects malformed inputs', () => {
  assert.throws(() => deriveVersion('0.4.9-beta.1', 19526400), /x\.y\.z/);
  assert.throws(() => deriveVersion('0.4.9', 0), /positive integer/);
  assert.throws(() => deriveVersion('0.4.9', 'abc'), /positive integer/);
});

test('readBridgeApiVersion extracts the exported const', () => {
  assert.equal(readBridgeApiVersion('export const BRIDGE_API_VERSION = 1;'), 1);
  assert.equal(readBridgeApiVersion('// doc\nexport const BRIDGE_API_VERSION = 12;\n'), 12);
  assert.throws(() => readBridgeApiVersion('export const OTHER = 1;'), /BRIDGE_API_VERSION not found/);
});

const FLOOR = { native: '0.4.8', desktop: '0.4.9', linux: '0.4.9', bridge: 1 };

test('readSupportFloor validates shape and ignores the comment key', () => {
  assert.deepEqual(readSupportFloor({ '//': 'doc', ...FLOOR }), FLOOR);
  assert.throws(() => readSupportFloor({ ...FLOOR, native: 'soon' }), /native must be x\.y\.z/);
  assert.throws(() => readSupportFloor({ ...FLOOR, bridge: 0 }), /bridge must be a positive integer/);
  assert.throws(() => readSupportFloor({ ...FLOOR, bridge: '1' }), /bridge must be a positive integer/);
});

const FIXTURE = {
  version: '0.4.9.19526400',
  size: 1234567,
  sha256Hex: 'a'.repeat(64),
  sigBase64: 'ZmFrZS1zaWctZm9yLXRlc3Q=',
  bridgeApiVersion: 1,
  floor: FLOOR,
  releasedAt: '2026-08-15T12:00:00Z',
};

test('buildManifest emits the exact backward-compatible shape', () => {
  const m = buildManifest(FIXTURE);
  // Legacy fields the deployed mobile natives parse — names and shapes frozen.
  assert.equal(m.version, '0.4.9.19526400');
  assert.equal(m.url, '0.4.9.19526400/web.zip'); // relative to the manifest's OWN dir (NOT "web/...")
  assert.equal(m.hash, `sha256:${'a'.repeat(64)}`);
  assert.equal(m.size, 1234567);
  assert.equal(m.released_at, '2026-08-15T12:00:00Z');
  assert.equal(m.min_native, '0.4.8'); // derived from the support floor, never hand-written
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
  assert.throws(() => buildManifest({ ...FIXTURE, version: '0.4.9' }), /x\.y\.z\.n/);
  assert.throws(() => buildManifest({ ...FIXTURE, floor: undefined }), /must be x\.y\.z/);
  assert.throws(
    () => buildManifest({ ...FIXTURE, floor: { ...FLOOR, bridge: 2 }, bridgeApiVersion: 1 }),
    /floor bridge \(2\) exceeds compiled BRIDGE_API_VERSION \(1\)/,
  );
  assert.throws(() => buildManifest({ ...FIXTURE, sigBase64: '' }), /sig/);
  assert.throws(() => buildManifest({ ...FIXTURE, sigBase64: 'has space' }), /sig/);
});

test('CLI `version` prints a 4-segment version whose 4th segment tracks the wall clock', () => {
  const out = execFileSync('node', [path.join(here, 'web-ota-manifest.mjs'), 'version'], {
    encoding: 'utf8',
  }).trim();
  assert.match(out, /^\d+\.\d+\.\d+\.\d+$/);
  const seg4 = Number(out.split('.')[3]);
  // Within 60s of what this process computes — proves time-based, not commit count.
  assert.ok(Math.abs(seg4 - computeBuildNumber(Date.now())) < 60, `seg4=${seg4} not time-based`);
});
