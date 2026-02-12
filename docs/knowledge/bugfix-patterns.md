# Bugfix Patterns

Recurring bugs and their solutions, extracted from git history and development experience.

## TCP-WS ALPN Mismatch: uTLS Fingerprint vs Server NextProtos (verified, 2026-02-12)

**Issue**: TCP-WS transport fails TLS handshake with `tls: no application protocol`. QUIC works fine. Bug never triggered before because TransportManager always tries QUIC first.

**Symptom**: `wire: uTLS handshake: remote error: tls: no application protocol` only when forcing `tp=tcpws`.

**Root Cause Chain**:
1. Server `TLSConfig.NextProtos = []string{"h3"}` — only accepts H3
2. QUIC path: `quic.go` explicitly sets `NextProtos = ["h3"]` on client → matches server → OK
3. TCP-WS path: uTLS Chrome 120 PQ fingerprint hardcodes `ALPNExtension{AlpnProtocols: []string{"h2", "http/1.1"}}` (`u_parrots.go:777`)
4. TCP-WS client never overrides fingerprint's ALPN → sends `["h2", "http/1.1"]` → no overlap with `["h3"]` → `alertNoApplicationProtocol`

**Why never caught**:
- TransportManager tries QUIC first, falls back to TCP-WS only on QUIC failure
- QUIC always worked → TCP-WS path never exercised in production
- Only exposed when `tp=tcpws` URL parameter forces TCP-WS-only mode

**Fix**: Server-side — add `h2` and `http/1.1` to `NextProtos`:
```go
// server.go (3 locations)
NextProtos: []string{"h3", "h2", "http/1.1"}
```

**Why server-side, not client-side**: uTLS fingerprints are designed to mimic real browsers. Overriding ALPN defeats fingerprint purpose and may make TLS handshake detectable by DPI. Server accepting more protocols is invisible to network observers.

**ECH safety proof**: Go `crypto/tls` processes ECH decryption in `readClientHello()` (line 159: `processECHClientHello`) **before** returning clientHello to `processClientHello()` where ALPN negotiation happens (line 299: `negotiateALPN`). Comment in source: *"ECH processing has to be done before we do any other negotiation based on the contents of the client hello, since we may swap it out completely."* ALPN and ECH are orthogonal — changing `NextProtos` cannot affect ECH.

**Validated in**: Manual test — `tp=tcpws` connects successfully after fix. `tp=quic` unaffected.

**Pattern**: When using uTLS fingerprints, server must accept ALL protocols the fingerprint advertises. Don't assume server and client ALPN are independently configurable — fingerprints dictate client behavior.

---

## Base64 URL-Safe Encoding in k2v5 URLs (adoption-scan, unverified)

**Issue**: ECH config embedded in `k2v5://` URL as `?ech=...` corrupted during parsing.

**Symptom**: ECH config decodes to garbage. `+` in standard base64 becomes space after `url.Query()`.

**Root Cause**: Go's `url.Query()` decodes `+` as space (legacy form encoding). Standard base64 uses `+` and `/`.

**Solution**: Use `base64.URLEncoding` (uses `-` and `_` instead):
```go
// Encode for URL
echB64 := base64.URLEncoding.EncodeToString(echConfigBytes)

// Decode: accept both for compatibility
data, err := base64.URLEncoding.DecodeString(encoded)
if err != nil {
    data, err = base64.StdEncoding.DecodeString(encoded)
}
```

**Validated in**: `wire/ech.go` (ParseECHConfig), `wire/wire.go` (ParseURL), `wire/certgen.go` (CertPinURL)

**Pattern**: Always use `URLEncoding` for data in URL parameters. Decode with fallback for compat.

---

## TransportManager Error Swallowing (adoption-scan, unverified)

**Issue**: TransportManager returned generic "wire: not connected" instead of actual transport error.

**Symptom**: Connection failures undebuggable. No way to tell if QUIC timeout, TCP TLS failure, or network unreachable.

**Root Cause**: Both QUIC and TCP-WS errors discarded, generic `ErrNotConnected` returned as fallback.

**Fix**: Preserve and chain errors with context:
```go
if quic != nil {
    conn, err := quic.DialTCP(ctx, addr)
    if err == nil { return conn, nil }
    lastErr = fmt.Errorf("QUIC: %w", err)
}
if tcpws != nil {
    conn, err := tcpws.DialTCP(ctx, addr)
    if err == nil { return conn, nil }
    lastErr = fmt.Errorf("TCP-WS: %w", err)
}
if lastErr != nil {
    return nil, lastErr  // Actual error, not generic
}
return nil, ErrNotConnected  // Only if transports not initialized
```

**Validated in**: `wire/transport.go` (DialTCP, DialUDP)

**Pattern**: Never swallow intermediate errors. Chain with `fmt.Errorf("context: %w", err)`, return last error.

---

## Self-Signed Cert Silent Failure (adoption-scan, unverified)

**Issue**: Self-signed TLS certs fail handshake silently. Combined with TransportManager error swallowing, connection just "doesn't work".

**Symptom**: Server starts fine, client gets "wire: not connected". No TLS error visible.

**Root Cause**: Go's default `InsecureSkipVerify=false` rejects self-signed certs. Error swallowed by transport fallback logic.

**Solution**: `insecure=1` URL parameter + pin verification as secure alternative:
```
# Dev/testing (insecure)
k2v5://...?ech=...&insecure=1

# Production (pin verification, no CA needed)
k2v5://...?ech=...&pin=sha256:<rsa>,sha256:<ec>
```

**Validated in**: `wire/wire.go` (ParseURL), `wire/ech.go` (BuildClientTLSConfig)

**Pattern**: Self-signed + pin = secure. Self-signed + insecure = dev-only. Always prefer pin.
