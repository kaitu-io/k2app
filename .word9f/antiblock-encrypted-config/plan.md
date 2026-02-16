# Execution Plan: antiblock-encrypted-config

## Summary

Replace base64 obfuscation with AES-256-GCM encryption for antiblock config.
Two parallel tasks: CI encryption tooling + client decryption rewrite.

## Complexity: Simple

4 new/modified source files, 2 independent work streams with zero file overlap.

## Dependency Graph

```
T1 (CI Tooling) ──────┐
                       ├── (merge to main)
T2 (Client Decrypt) ──┘
```

T1 and T2 are fully parallel — different directories, no shared code files.

## Test Command

```bash
cd webapp && yarn test
node scripts/antiblock-encrypt.js --test  # T1 self-test
```

---

## T1: CI Encryption Tooling

**Title**: Encryption scripts + GitHub Actions publish workflow
**Depends on**: none
**Files**:
- `scripts/antiblock-encrypt.js` — NEW
- `scripts/antiblock-keygen.js` — NEW
- `.github/workflows/publish-antiblock.yml` — NEW

### RED (write failing tests)

Since these are Node.js scripts (not webapp), tests are inline self-test mode:

`scripts/antiblock-encrypt.js --test` runs built-in assertions:

- `test_encrypt_produces_jsonp` — Output matches `void function(){var c={...}}();` pattern
- `test_encrypt_output_has_v_and_data` — Parsed JSON has `v: 1` and `data: string`
- `test_encrypt_data_is_base64` — `data` field is valid base64
- `test_encrypt_data_not_plaintext` — `atob(data)` does NOT contain plaintext URLs
- `test_keygen_produces_64_hex` — `antiblock-keygen.js` outputs 64-char hex string

### GREEN (implement to pass)

1. **`scripts/antiblock-keygen.js`**:
   - `crypto.randomBytes(32).toString('hex')` → stdout
   - Exit 0

2. **`scripts/antiblock-encrypt.js`**:
   - Read `ENTRIES` env (JSON array string) and `ENCRYPTION_KEY` env (64-char hex)
   - Validate inputs (array of URLs, 64-char hex key)
   - AES-256-GCM encrypt: `JSON.stringify({entries})` → `base64(iv + ciphertext + tag)`
   - Wrap in JSONP: `void function(){var c={"v":1,"data":"<base64>"}}();\n`
   - Write to `config.js` in current directory
   - `--test` flag: run self-test with fixture key + entries, verify output format

3. **`.github/workflows/publish-antiblock.yml`**:
   - `workflow_dispatch` with optional `entries` input
   - Steps: checkout → setup node → encrypt → push to `kaitu-io/ui-theme` orphan `dist` branch → purge jsDelivr cache
   - Env: `ENTRIES` from input or `ANTIBLOCK_ENTRIES` secret, `ENCRYPTION_KEY` from `ANTIBLOCK_KEY` secret
   - Push uses `CONFIG_REPO_TOKEN` PAT

### REFACTOR

- `[SHOULD]` Extract encrypt function to reusable module if >30 lines
- `[SHOULD]` Add `--help` flag to scripts

### AC Coverage

| AC  | Test |
|-----|------|
| AC1 | test_encrypt_produces_jsonp, test_encrypt_output_has_v_and_data |
| AC2 | test_keygen_produces_64_hex |
| AC3 | Workflow YAML review (inputs + secret fallback) |
| AC4 | Workflow YAML review (push + purge steps) |
| AC5 | test_encrypt_data_not_plaintext |

---

## T2: Client Decryption Rewrite

**Title**: Rewrite antiblock.ts with AES-256-GCM decryption
**Depends on**: none
**Files**:
- `webapp/src/api/antiblock.ts` — MODIFIED (full rewrite)
- `webapp/src/api/__tests__/antiblock.test.ts` — MODIFIED (rewrite tests)

### RED (write failing tests)

Tests in `webapp/src/api/__tests__/antiblock.test.ts`:

- `test_decrypt_roundtrip` — Web Crypto encrypt → decrypt → matches original entries (AC14)
- `test_decrypt_wrong_key_returns_null` — Decrypt with different key → returns null, no throw (AC15)
- `test_decrypt_tampered_payload_returns_null` — Flip byte in ciphertext → returns null (AC16)
- `test_cache_hit_skips_fetch` — localStorage has entry → resolveEntry returns immediately, no fetch (AC17)
- `test_all_cdn_fail_returns_default` — Both CDN sources fail → returns DEFAULT_ENTRY (AC18)
- `test_cdn_sources_are_github_urls` — CDN_SOURCES contain `jsdelivr.net/gh/kaitu-io/ui-theme` and `statically.io/gh/kaitu-io/ui-theme` (AC8)
- `test_no_atob_in_source` — Source code does not contain `atob(` (AC13)
- `test_key_is_64_hex` — DECRYPTION_KEY constant matches `/^[0-9a-f]{64}$/` (AC7)
- `test_background_refresh_on_cache_hit` — Cache hit triggers async background CDN fetch (AC9)
- `test_default_entry_is_plain_url` — DEFAULT_ENTRY starts with `https://` (AC10)

### GREEN (implement to pass)

1. **`antiblock.ts` rewrite**:
   - `DECRYPTION_KEY`: 64-char hex constant (generate with keygen, hardcode)
   - `DEFAULT_ENTRY`: keep `'https://w.app.52j.me'`
   - `CDN_SOURCES`: update to jsDelivr + statically.io GitHub URLs for `kaitu-io/ui-theme@dist`
   - `decrypt(encoded, keyHex)`: Web Crypto API AES-256-GCM decryption
     - Parse base64 → extract iv (12 bytes) + ciphertext+tag (rest)
     - `crypto.subtle.importKey` + `crypto.subtle.decrypt`
     - Return plaintext string or null on any error
   - `fetchEntryFromCDN()`: fetch → extract JSON via regex → `decrypt(config.data, DECRYPTION_KEY)` → parse entries → cache first
   - `resolveEntry()`: same logic (cache → CDN → default), swap `decodeEntries` for `decrypt`
   - Remove: `decodeEntries()`, all `atob()` usage

2. **Test helpers**:
   - `encryptForTest(plaintext, keyHex)`: Web Crypto API encrypt (used only in tests)
   - Mock `fetch` with encrypted fixture payloads

### REFACTOR

- `[SHOULD]` Extract `hexToBytes` to shared util if used elsewhere
- `[SHOULD]` Inline type for config schema `{ v: number; data: string }`

### AC Coverage

| AC  | Test |
|-----|------|
| AC6 | test_decrypt_roundtrip |
| AC7 | test_key_is_64_hex |
| AC8 | test_cdn_sources_are_github_urls |
| AC9 | test_background_refresh_on_cache_hit |
| AC10 | test_default_entry_is_plain_url |
| AC11 | test_decrypt_wrong_key_returns_null, test_decrypt_tampered_payload_returns_null, test_all_cdn_fail_returns_default |
| AC12 | Design verification (key rotation is operational, not code) |
| AC13 | test_no_atob_in_source |
| AC14 | test_decrypt_roundtrip |
| AC15 | test_decrypt_wrong_key_returns_null |
| AC16 | test_decrypt_tampered_payload_returns_null |
| AC17 | test_cache_hit_skips_fetch |
| AC18 | test_all_cdn_fail_returns_default |

---

## Deliverable Ownership Check

| Deliverable | Task |
|---|---|
| `scripts/antiblock-encrypt.js` | T1 |
| `scripts/antiblock-keygen.js` | T1 |
| `.github/workflows/publish-antiblock.yml` | T1 |
| `webapp/src/api/antiblock.ts` | T2 |
| `webapp/src/api/__tests__/antiblock.test.ts` | T2 |

All deliverables assigned. No orphans.

## AC Coverage Summary

| AC | Test(s) | Task |
|----|---------|------|
| AC1 | test_encrypt_produces_jsonp, test_encrypt_output_has_v_and_data | T1 |
| AC2 | test_keygen_produces_64_hex | T1 |
| AC3 | Workflow YAML (inputs + fallback) | T1 |
| AC4 | Workflow YAML (push + purge) | T1 |
| AC5 | test_encrypt_data_not_plaintext | T1 |
| AC6 | test_decrypt_roundtrip | T2 |
| AC7 | test_key_is_64_hex | T2 |
| AC8 | test_cdn_sources_are_github_urls | T2 |
| AC9 | test_background_refresh_on_cache_hit | T2 |
| AC10 | test_default_entry_is_plain_url | T2 |
| AC11 | test_decrypt_wrong_key_returns_null, test_decrypt_tampered_payload_returns_null, test_all_cdn_fail_returns_default | T2 |
| AC12 | Design verification | — |
| AC13 | test_no_atob_in_source | T2 |
| AC14 | test_decrypt_roundtrip | T2 |
| AC15 | test_decrypt_wrong_key_returns_null | T2 |
| AC16 | test_decrypt_tampered_payload_returns_null | T2 |
| AC17 | test_cache_hit_skips_fetch | T2 |
| AC18 | test_all_cdn_fail_returns_default | T2 |

All 18 ACs covered. AC12 is operational (key rotation flow), verified by design.
