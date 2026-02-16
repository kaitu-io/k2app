#!/usr/bin/env node
// antiblock-encrypt.js — Encrypt antiblock config JSON into JSONP with AES-256-GCM
// TODO-STUB: Implementation pending (T2)

'use strict';

const { execFileSync } = require('node:child_process');
const path = require('node:path');

/**
 * Encrypt a config object into a JSONP string.
 *
 * Output format:
 *   void function(){var c={v:1,data:"<base64>"}}();
 *
 * Where `data` is AES-256-GCM encrypted, base64-encoded ciphertext
 * of the JSON-serialized config.
 *
 * @param {object} config - The config object (e.g. { entries: ["https://..."] })
 * @param {string} keyHex - 64-char hex AES-256 key
 * @returns {string} JSONP-wrapped encrypted config
 */
function encrypt(config, keyHex) {
  // Stub: will use node:crypto aes-256-gcm
  throw new Error('encrypt not implemented');
}

// ---------------------------------------------------------------------------
// Self-test mode: node scripts/antiblock-encrypt.js --test
// ---------------------------------------------------------------------------

if (process.argv.includes('--test')) {
  runTests();
}

function runTests() {
  const results = [];
  const testConfig = { entries: ['https://example.com/api', 'https://fallback.example.com/api'] };
  const testKey = 'a'.repeat(64); // 64-char hex (32 bytes)

  // ---- test_encrypt_produces_jsonp ----
  results.push(runTest('test_encrypt_produces_jsonp', () => {
    const output = encrypt(testConfig, testKey);
    const pattern = /^void function\(\)\{var c=\{[\s\S]*\}\}\(\);$/;
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

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function extractJson(jsonp) {
  const match = jsonp.match(/\{[\s\S]*\}/);
  return match ? match[0] : null;
}

function truncate(str, len = 80) {
  if (typeof str !== 'string') return String(str);
  return str.length > len ? str.slice(0, len) + '...' : str;
}

// ---------------------------------------------------------------------------
// Main (non-test): read config from stdin, key from env/arg, write JSONP to stdout
// ---------------------------------------------------------------------------

if (require.main === module && !process.argv.includes('--test')) {
  console.error('Usage: echo \'{"entries":["https://..."]}\' | node antiblock-encrypt.js <key-hex>');
  console.error('       node antiblock-encrypt.js --test');
  process.exit(1);
}

module.exports = { encrypt };
