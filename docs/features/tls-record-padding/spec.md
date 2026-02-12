# Feature Spec: TLS Record Padding

## Meta

| Field | Value |
|-------|-------|
| Feature | tls-record-padding |
| Status | Implemented |
| Created | 2026-02-12 |
| Baseline | k2-baseline (wire/ layer, server/ layer) |
| Spec ref | wire/k2v5-protocol-spec.md Section 3.3-3.4 |
| Design ref | docs/todos/p0-zero-config-stealth-design.md |

## Problem Statement

TLS 1.3 encrypts handshake messages, but encrypted record **lengths** are observable. k2v5 server uses a single small self-signed cert (~500B) while real websites have multi-level CA chains (~3.8KB). Additionally, CertificateVerify signature length differs by algorithm (ECDSA ~72B vs RSA ~256B). These length differences allow censors to fingerprint the server as a proxy.

## Scope

### In Scope

1. Go crypto/tls monkey-patch for server-side TLS record padding (RFC 8446 §5.4)
2. Dual keypair generation (RSA-2048 + ECDSA P-256)
3. Target site probing (connect to outer SNI, measure cert characteristics)
4. Per-connection certificate selection (match outer SNI's algorithm)
5. Client-side public-key pin verification (`pin=sha256:A,sha256:B`)

### Out of Scope

- Outer SNI randomization (Cloudflare domain pool) — separate feature
- Server auto-provisioning (one-command startup) — separate feature
- Protocol spec updates — done separately

## Design

### D1: TLS Record Padding Monkey-Patch

RFC 8446 §5.4 defines standard record padding:
```
TLSInnerPlaintext: [content][ContentType 1B][0x00 ... padding]
                   └────────── AEAD encrypted ─────────────────┘
```

Go crypto/tls `halfConn.encrypt()` does not add padding. We monkey-patch via `//go:linkname` or unsafe access to inject padding bytes between ContentType and AEAD Seal.

**Approach**: Create `wire/tlspad/` package that patches Go's crypto/tls server-side encrypt path to support configurable per-record padding. The patch must:
- Add zero-byte padding after ContentType, before AEAD seal
- Support per-connection padding configuration
- Only affect server-side handshake records (Certificate, CertificateVerify)
- Be transparent to any standard TLS 1.3 client (padding removal is automatic)

### D2: Dual Keypair Generation

Server generates and persists two self-signed certificates:
- RSA-2048 (for sites using RSA)
- ECDSA P-256 (for sites using EC)

Both certs: 10-year validity, minimal size (short subject, no extras).

### D3: Target Probing

`ProbeTarget(host string)` connects to `host:443`, completes TLS handshake, returns:
- Key algorithm family (RSA or EC)
- Certificate chain total DER length
- Individual record lengths for Certificate and CertificateVerify

Results cached in `map[string]*TargetProfile`.

### D4: Per-Connection Certificate Selection

Server uses `tls.Config.GetCertificate` callback:
1. Extract outer SNI from `ClientHelloInfo.ServerName`
2. Look up cached `TargetProfile` for that SNI
3. Return matching cert (RSA or EC) based on target's algorithm
4. Configure padding amount for this connection

### D5: Client Pin Verification

URL format: `pin=sha256:<rsa_pubkey_hash>,sha256:<ec_pubkey_hash>`

Client `VerifyPeerCertificate` callback:
1. Parse certificate, extract public key
2. `sha256(MarshalPKIXPublicKey(pubkey))`
3. Match against ANY of the configured pins

## Acceptance Criteria

### TLS Record Padding
- **AC1**: Server TLS handshake records include RFC 8446 §5.4 padding
- **AC2**: Padded Certificate record length matches target site's Certificate record length (±16B)
- **AC3**: Standard TLS 1.3 clients (Go, uTLS, browsers) successfully handshake with padded server
- **AC4**: Padding is configurable per-connection (different amounts for different outer SNIs)

### Dual Keypair
- **AC5**: Server generates RSA-2048 and ECDSA P-256 self-signed certs
- **AC6**: Certs are persisted to disk and reloaded on restart
- **AC7**: CertificateVerify length matches when algorithm family matches target site

### Target Probing
- **AC8**: `ProbeTarget` correctly identifies RSA vs EC algorithm from real sites
- **AC9**: `ProbeTarget` measures Certificate message total length accurately
- **AC10**: Probe results are cached per hostname

### Certificate Selection
- **AC11**: Server selects RSA cert for RSA-algorithm targets, EC cert for EC targets
- **AC12**: `GetCertificate` callback returns correct cert per outer SNI

### Client Pin
- **AC13**: Client `VerifyPeerCertificate` validates public key hash against pin list
- **AC14**: Pin verification works for both RSA and EC server certificates
- **AC15**: `BuildClientTLSConfig` integrates pin verification when `pin` parameter present

## File Impact

| File | Change Type | Description |
|------|-------------|-------------|
| `wire/tlspad/` | New package | TLS record padding monkey-patch |
| `wire/certgen.go` | New file | Dual keypair generation + target probing |
| `wire/ech.go` | Modify | Add pin verification to `BuildClientTLSConfig` |
| `wire/config.go` | Modify | Add `Pin` field to `Config`, parse multi-pin format |
| `server/server.go` | Modify | `GetCertificate` callback, padding integration |
| `wire/tlspad/*_test.go` | New | Padding tests |
| `wire/certgen_test.go` | New | Keypair + probe tests |
| `wire/ech_test.go` | Modify | Pin verification tests |

## Risks

| Risk | Mitigation |
|------|------------|
| `//go:linkname` breaks on Go version upgrade | Pin Go version, add build-time test that verifies patch works |
| Padding amount calculation inaccurate | Measure actual encrypted record lengths via net.Conn wrapper in tests |
| `GetCertificate` receives inner SNI not outer SNI with ECH | Test with real ECH config; may need to use ClientHello peek data instead |
