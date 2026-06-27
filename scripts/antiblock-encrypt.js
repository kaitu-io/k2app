#!/usr/bin/env node
// antiblock-encrypt.js — Encrypt antiblock config JSON into JSONP with AES-256-GCM

'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const { execFileSync } = require('node:child_process');
const path = require('node:path');

/**
 * Encrypt a config object into a JSONP string.
 *
 * Output format (JSONP — sets window.<globalName> for <script> tag loading):
 *   window.__k2ac={"v":1,"data":"<base64>"};   (legacy default)
 *   window.__k2sd={"v":1,"data":"<base64>"};   (versioned seed, globalName='__k2sd')
 *
 * Where `data` is AES-256-GCM encrypted, base64-encoded:
 *   base64( iv(12 bytes) | ciphertext | GCM tag(16 bytes) )
 *
 * @param {object} config     - The config object (e.g. { entries: ["https://..."] })
 * @param {string} keyHex     - 64-char hex AES-256 key
 * @param {string} [globalName='__k2ac'] - JS global variable name to set
 * @returns {string} JSONP-wrapped encrypted config
 */
function encrypt(config, keyHex, globalName = '__k2ac') {
  const key = Buffer.from(keyHex, 'hex');
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', key, iv);
  const plaintext = JSON.stringify(config);
  const encrypted = Buffer.concat([cipher.update(plaintext, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();
  const data = Buffer.concat([iv, encrypted, tag]).toString('base64');
  return `window.${globalName}={"v":1,"data":"${data}"};`;
}

// ---------------------------------------------------------------------------
// Defaults — hardcoded (same key as webapp/src/api/antiblock.ts DECRYPTION_KEY)
// ---------------------------------------------------------------------------

const DEFAULT_KEY = '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';
const DEFAULT_ENTRIES = ['https://d1l0lk9fcyd6r8.cloudfront.net', 'https://k2.52j.me'];

// ---------------------------------------------------------------------------
// Self-test mode: node scripts/antiblock-encrypt.js --test
// ---------------------------------------------------------------------------

if (process.argv.includes('--test')) {
  runTests().catch((err) => {
    console.error('test harness crashed:', err);
    process.exit(1);
  });
}

async function runTests() {
  const results = [];
  const testConfig = { entries: ['https://example.com/api', 'https://fallback.example.com/api'] };
  const testKey = 'a'.repeat(64); // 64-char hex (32 bytes)

  // ---- test_encrypt_produces_jsonp ----
  results.push(runTest('test_encrypt_produces_jsonp', () => {
    const output = encrypt(testConfig, testKey);
    const pattern = /^window\.__k2ac=\{.*\};$/;
    assert(pattern.test(output), `Output must match JSONP pattern, got: ${truncate(output)}`);
  }));

  // ---- test_encrypt_output_has_v_and_data ----
  results.push(runTest('test_encrypt_output_has_v_and_data', () => {
    const output = encrypt(testConfig, testKey);
    const json = extractJson(output);
    assert(json !== null, 'Must be able to extract JSON from JSONP wrapper');
    const parsed = JSON.parse(json);
    assert(parsed.v === 1, `Expected v:1, got v:${parsed.v}`);
    assert(typeof parsed.data === 'string', `Expected data to be string, got ${typeof parsed.data}`);
  }));

  // ---- test_encrypt_data_is_base64 ----
  results.push(runTest('test_encrypt_data_is_base64', () => {
    const output = encrypt(testConfig, testKey);
    const json = extractJson(output);
    assert(json !== null, 'Must be able to extract JSON from JSONP wrapper');
    const parsed = JSON.parse(json);
    const base64Pattern = /^[A-Za-z0-9+/]+=*$/;
    assert(base64Pattern.test(parsed.data), `data field must be valid base64, got: ${truncate(parsed.data)}`);
    // Verify it decodes without error
    const decoded = Buffer.from(parsed.data, 'base64');
    assert(decoded.length > 0, 'base64-decoded data must be non-empty');
  }));

  // ---- test_encrypt_data_not_plaintext ----
  results.push(runTest('test_encrypt_data_not_plaintext', () => {
    const output = encrypt(testConfig, testKey);
    const json = extractJson(output);
    assert(json !== null, 'Must be able to extract JSON from JSONP wrapper');
    const parsed = JSON.parse(json);
    const decoded = Buffer.from(parsed.data, 'base64').toString('utf-8');
    assert(!decoded.includes('https://example.com'), 'Decoded data must NOT contain plaintext URLs (encryption required)');
    assert(!decoded.includes('entries'), 'Decoded data must NOT contain plaintext key names');
  }));

  // ---- test_keygen_produces_64_hex ----
  results.push(runTest('test_keygen_produces_64_hex', () => {
    const keygenPath = path.join(__dirname, 'antiblock-keygen.js');
    let keyOutput;
    try {
      keyOutput = execFileSync(process.execPath, [keygenPath], {
        encoding: 'utf-8',
        stdio: ['pipe', 'pipe', 'pipe'],
        timeout: 5000,
      }).trim();
    } catch (err) {
      // Extract the root cause from the child process stderr
      const stderr = err.stderr ? err.stderr.toString().trim() : '';
      const firstLine = stderr.split('\n').find((l) => l.includes('Error:')) || err.message;
      throw new Error(`keygen script failed: ${firstLine.trim()}`);
    }
    const hexPattern = /^[0-9a-f]{64}$/;
    assert(hexPattern.test(keyOutput), `keygen must output 64-char lowercase hex, got: ${truncate(keyOutput)}`);
  }));

  // ---- test_encrypt_custom_global ----
  results.push(runTest('test_encrypt_custom_global', () => {
    const out = encrypt({ entries: ['x'] }, DEFAULT_KEY, '__k2sd');
    assert(/^window\.__k2sd=\{/.test(out), `Must emit __k2sd global, got: ${truncate(out)}`);
  }));

  // ---- test_versioned_payload_roundtrips_nodes ----
  results.push(runTest('test_versioned_payload_roundtrips_nodes', () => {
    const payload = { entries: ['https://k2.52j.me'], nodes: [{ ip: '1.2.3.4', pin: 'sha256:AAA=', ech: 'AEX-x' }] };
    const out = encrypt(payload, DEFAULT_KEY, '__k2sd');
    assert(/^window\.__k2sd=/.test(out), `Must emit __k2sd global, got: ${truncate(out)}`);
    const json = extractJson(out);
    assert(json !== null, 'Must extract JSON from JSONP wrapper');
    const { data } = JSON.parse(json);
    const decrypted = decryptData(data, DEFAULT_KEY);
    assert(Array.isArray(decrypted.entries), 'entries must survive roundtrip');
    assert(decrypted.entries[0] === 'https://k2.52j.me', `entry[0] must match, got: ${decrypted.entries[0]}`);
    assert(Array.isArray(decrypted.nodes), 'nodes must survive roundtrip');
    assert(decrypted.nodes[0].ip === '1.2.3.4', `ip must match, got: ${decrypted.nodes[0].ip}`);
    assert(decrypted.nodes[0].pin === 'sha256:AAA=', `pin must match, got: ${decrypted.nodes[0].pin}`);
    assert(decrypted.nodes[0].ech === 'AEX-x', `ech must match, got: ${decrypted.nodes[0].ech}`);
  }));

  // ---- test_config_js_excludes_nodes ----
  results.push(runTest('test_config_js_excludes_nodes', () => {
    const out = encrypt({ entries: ['https://k2.52j.me'] }, DEFAULT_KEY);
    const json = extractJson(out);
    assert(json !== null, 'Must extract JSON from JSONP wrapper');
    const { data } = JSON.parse(json);
    const decrypted = decryptData(data, DEFAULT_KEY);
    assert(decrypted.nodes === undefined, 'legacy config plaintext must not contain nodes key');
    assert(!JSON.stringify(decrypted).includes('"ip"'), 'legacy config must not contain ip key');
  }));

  // ---- test_webcrypto_decrypts_node_gcm ----
  // CROSS-RUNTIME CONTRACT: bytes produced by Node `crypto.createCipheriv`
  // (this script, the CDN publisher) MUST decode under the webapp consumer's
  // Web Crypto `subtle.decrypt`. The webapp slices iv(12) off the front and
  // hands `ciphertext||tag` to subtle.decrypt — proven here against the live
  // encrypt() output (not a static fixture, so drift on EITHER side breaks it).
  results.push(await runAsyncTest('test_webcrypto_decrypts_node_gcm', async () => {
    const payload = { entries: ['https://k2.52j.me'], nodes: [{ ip: '13.54.164.215', pin: 'sha256:abc+/==', ech: 'AEX+ech/x' }] };
    // Random IV each run; loop to exercise all base64 padding alignments.
    for (let i = 0; i < 32; i++) {
      const out = encrypt(payload, DEFAULT_KEY, '__k2sd');
      const { data } = JSON.parse(extractJson(out));
      const plain = await webCryptoDecrypt(data, DEFAULT_KEY);
      assert(plain !== null, `web crypto must decrypt node-gcm output (iter ${i})`);
      const parsed = JSON.parse(plain);
      assert(parsed.nodes[0].ip === '13.54.164.215', `ip must survive (iter ${i})`);
      assert(parsed.nodes[0].ech === 'AEX+ech/x', `ech with +/ must survive (iter ${i})`);
    }
  }));

  // ---- test_webcrypto_rejects_tamper ----
  // GCM auth tag must reject a single-byte mutation — the mirror-poisoning
  // defense (key-embedded obfuscation accepts that mirrors see ciphertext; the
  // tag is what stops a malicious mirror from injecting forged nodes).
  results.push(await runAsyncTest('test_webcrypto_rejects_tamper', async () => {
    const out = encrypt({ entries: ['https://x'], nodes: [] }, DEFAULT_KEY, '__k2sd');
    const { data } = JSON.parse(extractJson(out));
    const raw = Buffer.from(data, 'base64');
    raw[20] ^= 0xff; // flip a byte in the ciphertext region
    const plain = await webCryptoDecrypt(raw.toString('base64'), DEFAULT_KEY);
    assert(plain === null, 'tampered ciphertext must NOT decrypt under GCM');
  }));

  // ---- Report ----
  console.log('\n--- Antiblock Encrypt Tests ---\n');
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

async function runAsyncTest(name, fn) {
  try {
    await fn();
    return { name, passed: true, error: null };
  } catch (err) {
    return { name, passed: false, error: err.message };
  }
}

/**
 * Decrypt VERBATIM as the webapp consumer does (antiblock-crypto.ts decrypt):
 * base64 → iv(12) | rest; hand `ciphertext||tag` straight to Web Crypto
 * subtle.decrypt. Returns the plaintext string, or null on any failure (incl.
 * GCM auth-tag rejection). Uses globalThis.crypto.subtle — the SAME WebCrypto
 * primitive the browser/vitest runtime uses.
 */
async function webCryptoDecrypt(dataBase64, keyHex) {
  try {
    const data = Buffer.from(dataBase64, 'base64');
    const iv = data.subarray(0, 12);
    const ciphertext = data.subarray(12); // ct || tag — what AES-GCM expects
    const rawKey = Buffer.from(keyHex, 'hex');
    const key = await globalThis.crypto.subtle.importKey('raw', rawKey, 'AES-GCM', false, ['decrypt']);
    const plain = await globalThis.crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ciphertext);
    return Buffer.from(plain).toString('utf8');
  } catch {
    return null;
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function extractJson(jsonp) {
  const match = jsonp.match(/=(\{.*\});$/);
  return match ? match[1] : null;
}

/**
 * Inverse of encrypt: base64-decode → split iv(12)|ct|tag(16) → AES-256-GCM decrypt → JSON.parse.
 * Used only in self-tests.
 */
function decryptData(dataBase64, keyHex) {
  const raw = Buffer.from(dataBase64, 'base64');
  const iv = raw.slice(0, 12);
  const tag = raw.slice(raw.length - 16);
  const ct = raw.slice(12, raw.length - 16);
  const key = Buffer.from(keyHex, 'hex');
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, iv);
  decipher.setAuthTag(tag);
  return JSON.parse(Buffer.concat([decipher.update(ct), decipher.final()]).toString('utf8'));
}

function truncate(str, len = 80) {
  if (typeof str !== 'string') return String(str);
  return str.length > len ? str.slice(0, len) + '...' : str;
}

// ---------------------------------------------------------------------------
// Main (non-test): read config from env, write JSONP files to cwd
// ---------------------------------------------------------------------------

if (require.main === module && !process.argv.includes('--test')) {
  const entriesRaw = process.env.ENTRIES;
  const nodesRaw = process.env.NODES;
  const cursorRaw = process.env.CURSOR;
  const keyHex = process.env.ENCRYPTION_KEY || DEFAULT_KEY;

  if (!/^[0-9a-f]{64}$/i.test(keyHex)) {
    console.error('Error: ENCRYPTION_KEY must be a 64-char hex string');
    process.exit(1);
  }

  let entries;
  if (entriesRaw) {
    try {
      entries = JSON.parse(entriesRaw);
    } catch {
      console.error('Error: ENTRIES must be valid JSON');
      process.exit(1);
    }
    if (!Array.isArray(entries)) {
      console.error('Error: ENTRIES must be a JSON array');
      process.exit(1);
    }
  } else {
    entries = DEFAULT_ENTRIES;
  }

  let nodes = null;
  if (nodesRaw) {
    try {
      nodes = JSON.parse(nodesRaw);
    } catch {
      console.error('Error: NODES must be valid JSON');
      process.exit(1);
    }
    if (!Array.isArray(nodes)) {
      console.error('Error: NODES must be a JSON array');
      process.exit(1);
    }
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      if (
        !n ||
        typeof n.ip !== 'string' || !n.ip ||
        typeof n.pin !== 'string' || !n.pin ||
        typeof n.ech !== 'string' || !n.ech
      ) {
        console.error(`Error: NODES[${i}] must have non-empty string fields: ip, pin, ech`);
        process.exit(1);
      }
    }
  }

  // Always write config.js — legacy contract (entries only, __k2ac global). FROZEN.
  const legacyJsonp = encrypt({ entries }, keyHex);
  const outPath = path.join(process.cwd(), 'config.js');
  fs.writeFileSync(outPath, legacyJsonp + '\n', 'utf8');
  console.log(`Wrote ${outPath} (${Buffer.byteLength(legacyJsonp + '\n')} bytes)`);

  // Write v/<CURSOR>.js when CURSOR is provided — versioned seed (__k2sd global, includes nodes).
  if (cursorRaw !== undefined && cursorRaw !== '') {
    const cursor = parseInt(cursorRaw, 10);
    if (!Number.isInteger(cursor) || isNaN(cursor) || cursor < 0) {
      console.error('Error: CURSOR must be a non-negative integer');
      process.exit(1);
    }
    const versionedJsonp = encrypt({ entries, nodes: nodes || [] }, keyHex, '__k2sd');
    const vDir = path.join(process.cwd(), 'v');
    fs.mkdirSync(vDir, { recursive: true });
    const vPath = path.join(vDir, `${cursor}.js`);
    fs.writeFileSync(vPath, versionedJsonp + '\n', 'utf8');
    console.log(`Wrote ${vPath} (${Buffer.byteLength(versionedJsonp + '\n')} bytes)`);
  }
}

module.exports = { encrypt };
