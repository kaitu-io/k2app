#!/usr/bin/env node
// gen-embedded-seed.js — Fetch latest published relay-node seed and rewrite
// webapp/src/services/antiblock-seed-embedded.ts at build time.
//
// Fail-soft: any network/parse/decrypt/404 error → stderr warning + exit 0.
// The committed TS floor is NEVER overwritten on error. Build always continues.
//
// Usage:
//   node scripts/gen-embedded-seed.js           # network path (CI / release builds)
//   node scripts/gen-embedded-seed.js --test    # formatter self-test only (no network)
//
// Env:
//   UI_THEME_REPO   GitHub repo (default: kaitu-io/ui-theme)
//   GH_TOKEN        Optional GitHub token for higher API rate limits

'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const DEFAULT_KEY = '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';
const UI_THEME_REPO = process.env.UI_THEME_REPO || 'kaitu-io/ui-theme';
const GH_TOKEN = process.env.GH_TOKEN || '';

const OUT_PATH = path.resolve(__dirname, '..', 'webapp', 'src', 'services', 'antiblock-seed-embedded.ts');

// ---------------------------------------------------------------------------
// Formatter — pure function, no I/O, TDD-able
// ---------------------------------------------------------------------------

/**
 * Emit the full content of antiblock-seed-embedded.ts from a seed object.
 * Uses JSON.stringify (2-space indent) so the emitted literal is valid JSON
 * and therefore valid TypeScript, and can be round-tripped with JSON.parse.
 *
 * @param {{ cursor: number, entries: string[], nodes: Array<{ip:string,pin:string,ech:string}> }} seed
 * @returns {string} Full TypeScript file content (ends with newline)
 */
function emitEmbeddedTs({ cursor, entries, nodes }) {
  const obj = { cursor, entries, nodes };
  const pretty = JSON.stringify(obj, null, 2);
  return [
    `import type { NodeEntry } from './node-descriptor';`,
    ``,
    `export const EMBEDDED_SEED: { cursor: number; entries: string[]; nodes: NodeEntry[] } = ${pretty};`,
    ``,
  ].join('\n');
}

// ---------------------------------------------------------------------------
// Decrypt — exact inverse of antiblock-encrypt.js `encrypt()`
//   encode: iv(12) | ciphertext | tag(16) → base64
//   decode: base64 → iv(12) | ct | tag(16) → AES-256-GCM decrypt → JSON.parse
// ---------------------------------------------------------------------------

function decryptSeed(dataBase64, keyHex) {
  const raw = Buffer.from(dataBase64, 'base64');
  const iv = raw.slice(0, 12);
  const tag = raw.slice(raw.length - 16);
  const ct = raw.slice(12, raw.length - 16);
  const key = Buffer.from(keyHex, 'hex');
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, iv);
  decipher.setAuthTag(tag);
  return JSON.parse(Buffer.concat([decipher.update(ct), decipher.final()]).toString('utf8'));
}

// ---------------------------------------------------------------------------
// Self-test (--test flag): pure formatter round-trip only — no network calls
// ---------------------------------------------------------------------------

if (process.argv.includes('--test')) {
  runTests();
}

function runTests() {
  const TEST_INPUT = {
    cursor: 3,
    entries: ['https://k2.52j.me'],
    nodes: [{ ip: '1.2.3.4', pin: 'sha256:AAA=', ech: 'AEX-x' }],
  };

  const results = [];

  // test_emit_contains_import: emitted content has the import + export const lines
  results.push(runTest('test_emit_contains_import', () => {
    const out = emitEmbeddedTs(TEST_INPUT);
    assert(
      out.includes(`import type { NodeEntry } from './node-descriptor';`),
      'Must include NodeEntry import line',
    );
    assert(out.includes('export const EMBEDDED_SEED'), 'Must include EMBEDDED_SEED export');
  }));

  // test_emit_roundtrip: extracting the object literal and JSON-parsing yields same values
  results.push(runTest('test_emit_roundtrip', () => {
    const out = emitEmbeddedTs(TEST_INPUT);
    // Match the JSON object after the `= ` of EMBEDDED_SEED (type annotation contains {/} too,
    // but [^=]+ consumes up to the first `=`, which is the assignment `=`).
    const match = out.match(/export const EMBEDDED_SEED[^=]+=\s*(\{[\s\S]*\});\s*$/);
    assert(match !== null, `Must extract object literal; got:\n${out}`);
    const parsed = JSON.parse(match[1]);
    assert(parsed.cursor === 3, `cursor must be 3, got ${parsed.cursor}`);
    assert(Array.isArray(parsed.entries), 'entries must be an array');
    assert(parsed.entries[0] === 'https://k2.52j.me', `entry[0] must match, got: ${parsed.entries[0]}`);
    assert(Array.isArray(parsed.nodes) && parsed.nodes.length === 1, 'nodes must have 1 element');
    assert(parsed.nodes[0].ip === '1.2.3.4', `ip must be 1.2.3.4, got: ${parsed.nodes[0].ip}`);
    assert(parsed.nodes[0].pin === 'sha256:AAA=', `pin must match, got: ${parsed.nodes[0].pin}`);
    assert(parsed.nodes[0].ech === 'AEX-x', `ech must match, got: ${parsed.nodes[0].ech}`);
  }));

  // test_emit_ends_with_newline: TS files should end with newline
  results.push(runTest('test_emit_ends_with_newline', () => {
    const out = emitEmbeddedTs(TEST_INPUT);
    assert(out.endsWith('\n'), 'Emitted TS must end with a newline');
  }));

  // test_emit_zero_nodes: empty nodes array is valid
  results.push(runTest('test_emit_zero_nodes', () => {
    const out = emitEmbeddedTs({ cursor: 0, entries: ['https://k2.52j.me'], nodes: [] });
    const match = out.match(/export const EMBEDDED_SEED[^=]+=\s*(\{[\s\S]*\});\s*$/);
    assert(match !== null, 'Must extract object literal for zero-nodes case');
    const parsed = JSON.parse(match[1]);
    assert(parsed.cursor === 0, `cursor must be 0, got ${parsed.cursor}`);
    assert(Array.isArray(parsed.nodes) && parsed.nodes.length === 0, 'nodes must be empty array');
  }));

  // ---- Report ----
  console.log('\n--- gen-embedded-seed self-tests ---\n');
  let passed = 0;
  let failed = 0;
  for (const r of results) {
    const status = r.passed ? 'PASS' : 'FAIL';
    const detail = r.passed ? '' : ` — ${r.error}`;
    console.log(`  [${status}] ${r.name}${detail}`);
    if (r.passed) passed++;
    else failed++;
  }
  console.log(`\n  ${passed} passed, ${failed} failed, ${results.length} total\n`);
  process.exit(failed > 0 ? 1 : 0);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function runTest(name, fn) {
  try {
    fn();
    return { name, passed: true, error: null };
  } catch (err) {
    return { name, passed: false, error: err.message };
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

// ---------------------------------------------------------------------------
// Network path — discover and fetch the highest v/<N>.js from ui-theme dist
// ---------------------------------------------------------------------------

async function fetchJson(url, headers) {
  const resp = await fetch(url, { headers });
  if (!resp.ok) throw new Error(`HTTP ${resp.status} ${resp.statusText} — ${url}`);
  return resp.json();
}

async function fetchText(url, headers) {
  const resp = await fetch(url, { headers });
  if (!resp.ok) throw new Error(`HTTP ${resp.status} ${resp.statusText} — ${url}`);
  return resp.text();
}

async function discoverAndRewrite() {
  const headers = { 'User-Agent': 'gen-embedded-seed/1.0' };
  if (GH_TOKEN) headers['Authorization'] = `Bearer ${GH_TOKEN}`;

  // 1. List v/ directory on dist branch via GitHub contents API
  const apiUrl = `https://api.github.com/repos/${UI_THEME_REPO}/contents/v?ref=dist`;
  let listing;
  try {
    listing = await fetchJson(apiUrl, headers);
  } catch (err) {
    throw new Error(`GitHub contents API failed for ${apiUrl}: ${err.message}`);
  }
  if (!Array.isArray(listing) || listing.length === 0) {
    throw new Error(`v/ directory is empty or not found on dist branch of ${UI_THEME_REPO}`);
  }

  // 2. Pick the highest numeric N from v/<N>.js filenames
  let highestN = -1;
  for (const entry of listing) {
    const m = entry.name && entry.name.match(/^(\d+)\.js$/);
    if (m) {
      const n = parseInt(m[1], 10);
      if (n > highestN) highestN = n;
    }
  }
  if (highestN < 0) throw new Error(`No v/<N>.js files found in dist branch of ${UI_THEME_REPO}`);

  // 3. Fetch raw JSONP from raw.githubusercontent.com (bypasses CDN cache)
  const rawUrl = `https://raw.githubusercontent.com/${UI_THEME_REPO}/dist/v/${highestN}.js`;
  let rawText;
  try {
    rawText = await fetchText(rawUrl, { 'User-Agent': 'gen-embedded-seed/1.0' });
  } catch (err) {
    throw new Error(`Failed to fetch ${rawUrl}: ${err.message}`);
  }

  // 4. Extract JSONP: window.__k2sd={"v":1,"data":"<base64>"};
  const jsonpMatch = rawText.match(/window\.__k2sd=(\{.*?\});/s);
  if (!jsonpMatch) {
    throw new Error(`Could not parse __k2sd JSONP from v/${highestN}.js (content: ${rawText.slice(0, 120)})`);
  }
  let envelope;
  try {
    envelope = JSON.parse(jsonpMatch[1]);
  } catch (err) {
    throw new Error(`Failed to JSON-parse JSONP envelope: ${err.message}`);
  }
  if (envelope.v !== 1 || typeof envelope.data !== 'string') {
    throw new Error(`Unexpected JSONP envelope shape: v=${envelope.v}, data type=${typeof envelope.data}`);
  }

  // 5. AES-256-GCM decrypt — inverse of antiblock-encrypt.js encrypt()
  let plaintext;
  try {
    plaintext = decryptSeed(envelope.data, DEFAULT_KEY);
  } catch (err) {
    throw new Error(`AES-256-GCM decryption failed for v/${highestN}.js: ${err.message}`);
  }
  if (!Array.isArray(plaintext.entries) || !Array.isArray(plaintext.nodes)) {
    throw new Error(`Decrypted plaintext missing entries or nodes arrays`);
  }

  // 6. Emit TS content and overwrite — only reached on full success
  const tsContent = emitEmbeddedTs({
    cursor: highestN,
    entries: plaintext.entries,
    nodes: plaintext.nodes,
  });
  fs.writeFileSync(OUT_PATH, tsContent, 'utf8');
  process.stdout.write(
    `gen-embedded-seed: cursor=${highestN}, ${plaintext.entries.length} entries, ${plaintext.nodes.length} nodes → ${path.relative(process.cwd(), OUT_PATH)}\n`,
  );
}

// ---------------------------------------------------------------------------
// Main entry
// ---------------------------------------------------------------------------

if (require.main === module && !process.argv.includes('--test')) {
  discoverAndRewrite().catch((err) => {
    process.stderr.write(`gen-embedded-seed: WARNING — ${err.message}\n`);
    process.stderr.write('gen-embedded-seed: keeping committed floor (build continues)\n');
    process.exit(0);
  });
}

module.exports = { emitEmbeddedTs, decryptSeed };
