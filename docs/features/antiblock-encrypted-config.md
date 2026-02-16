# Feature: Antiblock Encrypted Config

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | antiblock-encrypted-config               |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-17                               |
| Tests     | Unit tests for decryption + JSONP parsing |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-16 | Initial spec: AES-256-GCM encryption, public repo CDN     |

## Overview

Replace the current base64 obfuscation in `antiblock.ts` with AES-256-GCM encryption. Publish encrypted config to a public GitHub repo, served via free CDN (jsDelivr GitHub mode). GitHub Actions workflow handles encryption and publishing. No backward compatibility with old base64 format.

**Current state**: `antiblock.ts` fetches `unlock-it/config.js` from npm CDN, base64-decodes entry URLs. Base64 is trivially reversible — automated text scanning can extract URLs.

**Target state**: Encrypted config in `kaitu-io/ui-theme` public repo (orphan `dist` branch), AES-256-GCM decryption in client via Web Crypto API, CI-automated publish + CDN purge.

## Product Requirements

### PR1: Encrypted Config Publishing

- GitHub Actions `workflow_dispatch` workflow to encrypt and publish config
- Operator inputs entry URLs (JSON array) or uses default from GitHub Secret
- CI encrypts with AES-256-GCM → generates `config.js` → pushes to public repo → purges CDN cache
- No npm account or token needed — only git push to a public repo

### PR2: Client Decryption

- `antiblock.ts` rewritten to decrypt AES-256-GCM using Web Crypto API
- Decryption key hardcoded in `antiblock.ts` (acceptable with force upgrade as key rotation mechanism)
- Drop base64 `atob()` path entirely — no backward compatibility
- Maintain existing behavior: localStorage cache, background refresh, multi-CDN fallback, default entry fallback

### PR3: Key Rotation via Force Upgrade

- When decryption key is compromised: release new client version with new key → set `minClientVersion` in Cloud API → `ForceUpgradeDialog` blocks old clients
- `DEFAULT_ENTRY` fallback ensures force upgrade notification reaches clients even if CDN entries are all blocked
- Key rotation is a manual, infrequent operation (not automated)

## Technical Decisions

### TD1: Encryption — AES-256-GCM via Web Crypto API

AES-256-GCM provides authenticated encryption (confidentiality + integrity) with zero JS dependencies.

**Encryption (CI side, Node.js)**:
```javascript
import { randomBytes, createCipheriv } from 'crypto';

function encrypt(plaintext, keyHex) {
  const key = Buffer.from(keyHex, 'hex');     // 32 bytes
  const iv = randomBytes(12);                  // 12 bytes
  const cipher = createCipheriv('aes-256-gcm', key, iv);
  const encrypted = Buffer.concat([cipher.update(plaintext, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();             // 16 bytes
  // Format: base64(iv + ciphertext + tag)
  return Buffer.concat([iv, encrypted, tag]).toString('base64');
}
```

**Decryption (client side, Web Crypto API)**:
```typescript
async function decrypt(encoded: string, keyHex: string): Promise<string> {
  const data = Uint8Array.from(atob(encoded), c => c.charCodeAt(0));
  const iv = data.slice(0, 12);
  const ciphertext = data.slice(12);  // includes 16-byte tag (GCM appends it)
  const key = await crypto.subtle.importKey(
    'raw',
    hexToBytes(keyHex),
    'AES-GCM',
    false,
    ['decrypt'],
  );
  const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ciphertext);
  return new TextDecoder().decode(plain);
}
```

**Why AES-256-GCM**:
- Web Crypto API native — zero bundle size impact
- Authenticated encryption — tampered payloads rejected
- Industry standard, no exotic dependencies
- Node.js `crypto` module on CI side — also zero deps

**Why not XChaCha20-Poly1305**: Requires `libsodium-wrappers` (~180KB). Overkill for this use case.

### TD2: Config Distribution — Public GitHub Repo + jsDelivr

Publish encrypted config to a public GitHub repository. jsDelivr serves GitHub files for free.

**CDN URLs**:
```
https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js
https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js
```

**Why not npm**: npm requires account + token + package.json + versioning. Public repo needs only `git push` — CI already has GitHub token.

**Cache purge**: jsDelivr GitHub mode caches up to 12h. After push, CI calls:
```
GET https://purge.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js
```

**Fallback CDN**: Add statically.io as second source (also serves from GitHub):
```
https://cdn.statically.io/gh/kaitu-io/ui-theme/dist/config.js
```

### TD3: Config File Format — JSONP with Encrypted Payload

```javascript
// config.js — JSONP-compatible, served from CDN
void function(){var c={"v":1,"data":"<base64(iv+ciphertext+tag)>"}}();
```

- `v`: schema version (for future format changes)
- `data`: AES-256-GCM encrypted JSON string of `{"entries":["https://...","https://..."]}`
- JSONP-style wrapper: extractable via regex `\{[\s\S]*\}` (same pattern as current)

**Why keep JSONP wrapper**: Proven extraction pattern, works with `<script>` tag loading as future fallback if `fetch()` is blocked.

### TD4: Key Management — Hardcoded + Force Upgrade Rotation

Decryption key is a 256-bit hex string hardcoded in `antiblock.ts`.

**Threat model**:
- Goal: prevent **automated text scanning** of CDN content → extracting entry URLs
- Non-goal: prevent determined reverse engineering (impossible in client-side JS)
- Any JS-embedded key is extractable by a determined attacker
- Encryption raises the bar from "grep base64 + atob" to "reverse-engineer JS + extract key + run AES-GCM decrypt"

**Key rotation flow**:
1. Generate new 256-bit key
2. Update `antiblock.ts` with new key
3. Re-encrypt config with new key → publish to repo
4. Release new client version
5. Set `minClientVersion` in Cloud API → old clients forced to upgrade
6. Old key no longer in any active client

**Bootstrap safety**: `DEFAULT_ENTRY` is not encrypted — it's a hardcoded fallback URL in `antiblock.ts`. Even if all CDN entries are blocked, clients can reach Cloud API via default entry to receive force upgrade notification.

### TD5: GitHub Actions Workflow — workflow_dispatch

```yaml
name: Publish Antiblock Config

on:
  workflow_dispatch:
    inputs:
      entries:
        description: 'Entry URLs as JSON array (leave empty for default)'
        required: false
        type: string

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - name: Encrypt config
        run: node scripts/antiblock-encrypt.js
        env:
          ENTRIES: ${{ inputs.entries || secrets.ANTIBLOCK_ENTRIES }}
          ENCRYPTION_KEY: ${{ secrets.ANTIBLOCK_KEY }}
      - name: Push to config repo (orphan dist branch)
        run: |
          cd /tmp/config-repo
          git clone --single-branch -b dist https://x-access-token:${{ secrets.CONFIG_REPO_TOKEN }}@github.com/kaitu-io/ui-theme.git . || {
            git init && git remote add origin https://x-access-token:${{ secrets.CONFIG_REPO_TOKEN }}@github.com/kaitu-io/ui-theme.git
            git checkout --orphan dist
          }
          cp $GITHUB_WORKSPACE/config.js .
          git add config.js
          git commit -m "update $(date -u +%Y%m%d%H%M%S)" || true
          git push origin dist
      - name: Purge CDN cache
        run: |
          curl -s https://purge.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js
```

**Required GitHub Secrets**:
- `ANTIBLOCK_ENTRIES`: Default entry URL JSON array, e.g. `["https://w.app.52j.me"]`
- `ANTIBLOCK_KEY`: 256-bit hex encryption key (64 hex chars)
- `CONFIG_REPO_TOKEN`: PAT with push access to the public config repo

## Architecture

### File Structure (new/modified)

```
scripts/
  antiblock-encrypt.js       NEW — Node.js encryption script for CI
  antiblock-keygen.js         NEW — One-time key generation utility

webapp/src/api/
  antiblock.ts                MODIFIED — AES-256-GCM decryption, new CDN URLs

.github/workflows/
  publish-antiblock.yml       NEW — workflow_dispatch publish workflow
```

### Data Flow

```
Operator (workflow_dispatch)
  │
  ├─ entries: ["https://w.app.52j.me", "https://w2.app.52j.me"]
  ├─ key: ANTIBLOCK_KEY from GitHub Secret
  │
  ▼
scripts/antiblock-encrypt.js
  │
  ├─ AES-256-GCM encrypt(JSON.stringify({entries}), key)
  ├─ Wrap in JSONP: void function(){var c={v:1,data:"<base64>"}}();
  ├─ Output: config.js
  │
  ▼
git push → public config repo
  │
  ▼
CDN (jsDelivr + statically.io)
  │
  ▼
webapp/src/api/antiblock.ts
  │
  ├─ fetch CDN URL → extract JSON → decrypt data field
  ├─ Parse entries array → cache first entry in localStorage
  ├─ Fallback: DEFAULT_ENTRY (hardcoded, unencrypted)
  │
  ▼
cloudApi requests use resolved entry URL
```

### Client Resolution Chain (updated)

```
1. localStorage cache hit? → use cached, background refresh → done
2. Fetch CDN source 1 (jsDelivr) → decrypt → cache → done
3. Fetch CDN source 2 (statically.io) → decrypt → cache → done
4. All CDN fail → DEFAULT_ENTRY fallback → done
```

## Acceptance Criteria

### Encryption & Publishing

- **AC1**: `scripts/antiblock-encrypt.js` encrypts a JSON entries array with AES-256-GCM, outputs `config.js` in JSONP format
- **AC2**: `scripts/antiblock-keygen.js` generates a random 256-bit hex key to stdout
- **AC3**: `publish-antiblock.yml` workflow_dispatch accepts optional `entries` input, falls back to `ANTIBLOCK_ENTRIES` secret
- **AC4**: Workflow pushes `config.js` to public config repo and purges jsDelivr cache
- **AC5**: Encrypted payload is not reversible without the key — `atob()` alone produces garbage

### Client Decryption

- **AC6**: `antiblock.ts` decrypts AES-256-GCM payload using Web Crypto API with hardcoded key
- **AC7**: Decryption key is a 64-char hex string constant in `antiblock.ts`
- **AC8**: CDN sources updated to jsDelivr + statically.io GitHub repo URLs
- **AC9**: localStorage caching preserved — cache hit returns immediately, background refresh
- **AC10**: `DEFAULT_ENTRY` fallback unchanged — always available without decryption
- **AC11**: Decryption failure (wrong key, tampered payload, network error) falls through to next CDN source or default entry — never throws to caller

### Key Rotation

- **AC12**: Changing the key in `antiblock.ts` + re-encrypting config + setting `minClientVersion` in Cloud API rotates all clients to new key
- **AC13**: Old base64 `atob()` decode path is fully removed — no backward compatibility code

### Testing

- **AC14**: Unit test: encrypt with known key → decrypt with same key → matches original entries
- **AC15**: Unit test: decrypt with wrong key → returns null (not throw)
- **AC16**: Unit test: tampered ciphertext → returns null (GCM auth tag verification fails)
- **AC17**: Unit test: localStorage cache hit skips CDN fetch
- **AC18**: Unit test: all CDN fail → returns `DEFAULT_ENTRY`
