#!/usr/bin/env node
// Web OTA version derivation + latest.json generation
// (spec: docs/superpowers/specs/2026-08-14-web-ota-design.md §3.1 / §3.2 / §4, R2).
//
// Subcommands:
//   version    print "{root package.json version}.{time-based build number}"
//   manifest   --version V --zip PATH --sig-file PATH --out PATH
//
// R2 semantics:
// - 4th version segment = Unix seconds since 2026-01-01T00:00:00Z, computed at
//   publish time. Globally monotonic across ALL publishing workflows (webapp
//   tag / release-desktop / build-mobile) and across refs — republishing an
//   old ref still yields a HIGHER version, which is what makes dispatch-based
//   rollback work (spec §7). Commit count could not guarantee that.
// - min_native / min_desktop / min_linux / min_bridge come from
//   contracts/webapp-support-floor.json — the SUPPORT FLOOR (oldest shell
//   versions the latest webapp still supports), NOT "what this webapp
//   requires". Bumping BRIDGE_API_VERSION does not change the manifest;
//   bumping the floor is an explicit support-drop decision (spec §4.2).
//   Hand-written min_native was the 2026-03 incident's root cause — values
//   stay derived, never inline in the workflow.
// - Manifest `url` is RELATIVE TO THE MANIFEST'S OWN DIRECTORY
//   ("{version}/web.zip"): both mobile resolveDownloadURL implementations
//   join relative urls onto the manifest endpoint minus its filename.

import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');

export const WEB_OTA_EPOCH_MS = Date.UTC(2026, 0, 1); // 1767225600000

export function computeBuildNumber(nowMs) {
  const n = Math.floor((nowMs - WEB_OTA_EPOCH_MS) / 1000);
  if (!Number.isSafeInteger(n) || n <= 0) {
    throw new Error(`build number must be a positive integer (clock before 2026-01-01?), got: ${n}`);
  }
  return n;
}

export function deriveVersion(pkgVersion, buildNumber) {
  if (!/^\d+\.\d+\.\d+$/.test(pkgVersion)) {
    throw new Error(`root package.json version must be x.y.z, got: ${pkgVersion}`);
  }
  const n = Number(buildNumber);
  if (!Number.isInteger(n) || n <= 0) {
    throw new Error(`build number must be a positive integer, got: ${buildNumber}`);
  }
  return `${pkgVersion}.${n}`;
}

export function readBridgeApiVersion(tsSource) {
  const m = tsSource.match(/export const BRIDGE_API_VERSION\s*=\s*(\d+)/);
  if (!m) throw new Error('BRIDGE_API_VERSION not found in webapp/src/types/bridge-version.ts');
  return Number(m[1]);
}

// Compare the x.y.z base segments of two versions. Extra segments (the web OTA
// 4th segment, prerelease suffixes) are ignored. Returns -1 / 0 / 1.
export function compareBase(a, b) {
  const seg = (v) =>
    String(v)
      .split('.')
      .slice(0, 3)
      .map((s) => Number(s.replace(/\D.*$/, '')) || 0);
  const [pa, pb] = [seg(a), seg(b)];
  for (let i = 0; i < 3; i++) {
    if (pa[i] !== pb[i]) return pa[i] < pb[i] ? -1 : 1;
  }
  return 0;
}

// Validates the support-floor object (contracts/webapp-support-floor.json).
// Ignores the "//" comment key. Returns {native, desktop, linux, bridge}.
export function readSupportFloor(obj) {
  for (const key of ['native', 'desktop', 'linux']) {
    if (!/^\d+\.\d+\.\d+$/.test(obj?.[key] ?? '')) {
      throw new Error(`webapp-support-floor.json .${key} must be x.y.z, got: ${obj?.[key]}`);
    }
  }
  if (!Number.isInteger(obj.bridge) || obj.bridge < 1) {
    throw new Error(`webapp-support-floor.json .bridge must be a positive integer, got: ${obj?.bridge}`);
  }
  return { native: obj.native, desktop: obj.desktop, linux: obj.linux, bridge: obj.bridge };
}

export function buildManifest({ version, size, sha256Hex, sigBase64, bridgeApiVersion, floor, releasedAt }) {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`web OTA version must be x.y.z.n, got: ${version}`);
  }
  const f = readSupportFloor(floor);
  if (f.bridge > bridgeApiVersion) {
    throw new Error(
      `support floor bridge (${f.bridge}) exceeds compiled BRIDGE_API_VERSION (${bridgeApiVersion}) — ` +
        `the floor cannot be newer than the current bridge surface`,
    );
  }
  // Same invariant, version dimension: a floor above the version being published
  // means that shell is gated out of its own web OTA by the min_* it just wrote
  // (silently — evaluate_manifest/meetsMinVersion return a plain "skip"). Shipped
  // 0.4.8 desktop/Linux against a 0.4.9 floor for exactly this reason.
  const publishedBase = version.split('.').slice(0, 3).join('.');
  for (const key of ['native', 'desktop', 'linux']) {
    if (compareBase(f[key], publishedBase) > 0) {
      throw new Error(
        `support floor ${key} (${f[key]}) is newer than the version being published (${publishedBase}) — ` +
          `that shell would be gated out of its own web OTA by min_${key}`,
      );
    }
  }
  if (typeof sigBase64 !== 'string' || sigBase64.length === 0 || /\s/.test(sigBase64)) {
    throw new Error('sig must be non-empty single-line base64 (base64 of the whole .minisig file)');
  }
  return {
    version,
    url: `${version}/web.zip`,
    hash: `sha256:${sha256Hex}`,
    size,
    released_at: releasedAt,
    min_native: f.native,
    sig: sigBase64,
    min_bridge: f.bridge,
    min_desktop: f.desktop,
    min_linux: f.linux,
  };
}

// Provenance sidecar written NEXT TO the manifest (`latest-source.json`) and
// kept immutably per version (`{version}/source.json`).
//
// It is deliberately NOT a manifest field: every shipped client parses
// latest.json, and one strict parser among them (any brand, any platform,
// any version still in the wild) would turn a schema addition into a
// field-wide outage. Nothing reads this file except
// scripts/ci/web-ota-gate.mjs, which needs to know which commit produced the
// bundle that is live in order to refuse a publish that would move stable
// backwards. Second use: support can finally answer "which UI is this device
// running", which the manifest alone never allowed.
export function buildProvenance({ version, commit, ref, brand, runId, runAttempt, workflow, publishedAt }) {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(version ?? '')) {
    throw new Error(`provenance version must be x.y.z.n, got: ${version}`);
  }
  // A short sha would make the gate's ancestry check ambiguous, and an empty
  // one would make it silently unresolvable — refuse at write time instead.
  if (!/^[0-9a-f]{40}$/.test(commit ?? '')) {
    throw new Error(`provenance commit must be a full 40-hex sha, got: ${commit}`);
  }
  if (!brand) throw new Error('provenance brand is required');
  return {
    version,
    commit,
    brand,
    ref: ref ?? '',
    run_id: runId ?? '',
    run_attempt: runAttempt ?? '',
    workflow: workflow ?? '',
    published_at: publishedAt,
  };
}

function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 2) {
    if (!argv[i].startsWith('--') || argv[i + 1] === undefined) {
      throw new Error(`bad argument pair: ${argv[i]} ${argv[i + 1] ?? ''}`);
    }
    args[argv[i].slice(2)] = argv[i + 1];
  }
  return args;
}

function main() {
  const [cmd, ...rest] = process.argv.slice(2);
  if (cmd === 'version') {
    const pkg = JSON.parse(readFileSync(path.join(ROOT, 'package.json'), 'utf8'));
    process.stdout.write(deriveVersion(pkg.version, computeBuildNumber(Date.now())) + '\n');
    return;
  }
  if (cmd === 'manifest') {
    const args = parseArgs(rest);
    for (const key of ['version', 'zip', 'sig-file', 'out']) {
      if (!args[key]) throw new Error(`--${key} is required`);
    }
    const zip = readFileSync(args.zip);
    const manifest = buildManifest({
      version: args.version,
      size: zip.length,
      sha256Hex: createHash('sha256').update(zip).digest('hex'),
      sigBase64: readFileSync(args['sig-file']).toString('base64'),
      bridgeApiVersion: readBridgeApiVersion(
        readFileSync(path.join(ROOT, 'webapp/src/types/bridge-version.ts'), 'utf8'),
      ),
      floor: JSON.parse(readFileSync(path.join(ROOT, 'contracts/webapp-support-floor.json'), 'utf8')),
      releasedAt: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
    });
    writeFileSync(args.out, JSON.stringify(manifest, null, 2) + '\n');
    console.log(`wrote ${args.out} (version ${manifest.version}, min_native ${manifest.min_native}, min_bridge ${manifest.min_bridge})`);
    return;
  }
  if (cmd === 'source') {
    const args = parseArgs(rest);
    for (const key of ['version', 'commit', 'brand', 'out']) {
      if (!args[key]) throw new Error(`--${key} is required`);
    }
    const provenance = buildProvenance({
      version: args.version,
      commit: args.commit,
      brand: args.brand,
      ref: args.ref,
      runId: args['run-id'],
      runAttempt: args['run-attempt'],
      workflow: args.workflow,
      publishedAt: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
    });
    writeFileSync(args.out, JSON.stringify(provenance, null, 2) + '\n');
    console.log(`wrote ${args.out} (version ${provenance.version}, commit ${provenance.commit.slice(0, 12)})`);
    return;
  }
  console.error(
    'usage: web-ota-manifest.mjs version\n' +
      '     | manifest --version V --zip P --sig-file P --out P\n' +
      '     | source --version V --commit SHA --brand B --out P [--ref R --run-id N --run-attempt N --workflow W]',
  );
  process.exit(2);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
