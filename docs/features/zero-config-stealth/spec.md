# Zero-Config Stealth: Spec

## Meta

| Field | Value |
|-------|-------|
| Status | implemented |
| Implementation Date | 2026-02-12 |

## Goal

Server `k2s run` with zero mandatory config. Auto-generates certs, ECH config (forged from Cloudflare), auth tokens. Logs a complete k2v5:// URL. Clients paste URL and connect.

## Acceptance Criteria

### AC1: ECH Config Forging
- Server queries Cloudflare DNS HTTPS record, extracts real ECH config template
- Generates ECH config with: config_id = Cloudflare's + 1, same cipher_suites/kem_id/public_name, our HPKE public key
- Falls back to cached template or hardcoded defaults if DNS fails
- Unit test: parse real Cloudflare ECH config, verify template extraction

### AC2: ECH Config Rotation
- Server maintains multiple ECH configs (shared HPKE private key, different config metadata)
- All configs stored in cert_dir/ech-configs/
- `EncryptedClientHelloKeys` loaded as slice — old clients with old configs still connect
- Unit test: two configs with different config_ids both accepted by server

### AC3: Client CA Handling
- Verification chain: pin > insecure > default CA pool
- Default CA pool = system CA + embedded Kaitu CA - Chinese CA blacklist
- Chinese CA blacklist: CNNIC, WoSign, WoTrus, CFCA, GDCA, SHECA
- Unit test: pool includes Kaitu CA, excludes CNNIC

### AC4: Server Zero-Config Startup
- `k2s run` with no config file works (all defaults auto-derived)
- Defaults: listen=:443, cert_dir=~/.k2s/, ech.provider=local
- Auto-generates: dual certs + ECH configs + auth token
- Logs complete k2v5:// URL to stdout
- Integration test: zero-config server starts and client connects

### AC5: Cleanup
- `sni` URL parameter removed, `www.microsoft.com` default removed
- Outer SNI = ECH config's public_name (forced by uTLS, = cloudflare-ech.com)
- Protocol spec (k2v5-protocol-spec.md) updated to reflect all changes
- Existing tests still pass after SNI removal

## Non-Goals
- Fake SNI randomization (uTLS forces outer SNI = ECH public_name)
- ECH config auto-rotation at runtime (restart to refresh)
- Custom CA parameter (removed, use pin or default pool)
