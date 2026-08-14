#!/usr/bin/env node
// Web OTA version derivation + latest.json generation
// (spec: docs/superpowers/specs/2026-08-14-web-ota-design.md §3.1 / §3.2 / §4).
//
// Subcommands:
//   version    print "{root package.json version}.{git rev-list --count HEAD}"
//   manifest   --version V --zip PATH --sig-file PATH --out PATH
//
// min_native / min_desktop / min_linux / min_bridge are DERIVED from
// webapp/src/types/bridge-version.ts + contracts/bridge-versions.json —
// never hand-written (hand-written min_native is the 2026-03 incident's
// root cause). Manifest `url` is RELATIVE TO THE MANIFEST'S OWN DIRECTORY
// ("{version}/web.zip"): both mobile resolveDownloadURL implementations
// join relative urls onto the manifest endpoint minus its filename.

import { createHash } from 'node:crypto';
import { execSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');

export function deriveVersion(pkgVersion, commitCount) {
  if (!/^\d+\.\d+\.\d+$/.test(pkgVersion)) {
    throw new Error(`root package.json version must be x.y.z, got: ${pkgVersion}`);
  }
  const n = Number(commitCount);
  if (!Number.isInteger(n) || n <= 0) {
    throw new Error(`commit count must be a positive integer, got: ${commitCount}`);
  }
  return `${pkgVersion}.${n}`;
}

export function readBridgeApiVersion(tsSource) {
  const m = tsSource.match(/export const BRIDGE_API_VERSION\s*=\s*(\d+)/);
  if (!m) throw new Error('BRIDGE_API_VERSION not found in webapp/src/types/bridge-version.ts');
  return Number(m[1]);
}

export function buildManifest({ version, size, sha256Hex, sigBase64, bridgeApiVersion, bridgeVersions, releasedAt }) {
  if (!/^\d+\.\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(`web OTA version must be x.y.z.n, got: ${version}`);
  }
  const entry = bridgeVersions[String(bridgeApiVersion)];
  if (!entry) {
    throw new Error(
      `contracts/bridge-versions.json has no entry for bridge version ${bridgeApiVersion} — ` +
        `the bridge contract gate should have caught this; add the entry and re-run it`,
    );
  }
  for (const key of ['native', 'desktop', 'linux']) {
    if (!/^\d+\.\d+\.\d+$/.test(entry[key] ?? '')) {
      throw new Error(`bridge-versions.json["${bridgeApiVersion}"].${key} must be x.y.z, got: ${entry[key]}`);
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
    min_native: entry.native,
    sig: sigBase64,
    min_bridge: bridgeApiVersion,
    min_desktop: entry.desktop,
    min_linux: entry.linux,
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
    const count = execSync('git rev-list --count HEAD', { cwd: ROOT }).toString().trim();
    process.stdout.write(deriveVersion(pkg.version, count) + '\n');
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
      bridgeVersions: JSON.parse(readFileSync(path.join(ROOT, 'contracts/bridge-versions.json'), 'utf8')),
      releasedAt: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
    });
    writeFileSync(args.out, JSON.stringify(manifest, null, 2) + '\n');
    console.log(`wrote ${args.out} (version ${manifest.version}, min_native ${manifest.min_native}, min_bridge ${manifest.min_bridge})`);
    return;
  }
  console.error('usage: web-ota-manifest.mjs version | manifest --version V --zip P --sig-file P --out P');
  process.exit(2);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
