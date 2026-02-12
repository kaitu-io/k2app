# Execution Plan: TLS Record Padding

## Meta

| Field | Value |
|-------|-------|
| Feature | tls-record-padding |
| Spec | docs/features/tls-record-padding/spec.md |
| Tasks | 3 |
| Parallelism | T1 ∥ T2, then T3 |

## Task Graph

```
T1 (TLS padding patch) ──┐
                          ├── T3 (Server integration + E2E)
T2 (Keypair + probe)   ──┘
```

## Tasks

### T1: TLS Record Padding Monkey-Patch

**ID**: T1
**depends_on**: []
**files**: `wire/tlspad/` (new package)
**AC coverage**: AC1, AC2, AC3, AC4

Create `wire/tlspad/` package that monkey-patches Go crypto/tls to support server-side TLS 1.3 record padding.

#### Context

Go crypto/tls `halfConn.encrypt()` in `conn.go` builds TLS 1.3 records as:
```go
record = append(record, payload...)
record = append(record, byte(typ))  // ContentType
// NO padding here
encrypted = hc.aead.Seal(...)
```

RFC 8446 §5.4 allows zero-byte padding after ContentType before AEAD seal. Any compliant TLS 1.3 client automatically strips padding.

#### Implementation

**Approach: monkey-patch via `//go:linkname` or `unsafe`**

1. Analyze Go 1.25 `crypto/tls/conn.go` source to find exact hook point:
   - Read `/usr/local/go/src/crypto/tls/conn.go` — find `halfConn.encrypt()` method
   - Read `/usr/local/go/src/crypto/tls/handshake_server_tls13.go` — find handshake flow
   - Identify how `c.buffering = true` collects records, and how `c.flush()` sends them

2. Implement the patch — TWO possible approaches, try A first, fall back to B:

   **Approach A — net.Conn interception (preferred if feasible)**:
   Wrap the underlying `net.Conn` BEFORE passing to `tls.Server()`. After TLS encrypt writes a record to the conn, we can't modify it (already encrypted). So this won't work for padding. Skip to B.

   **Approach B — `//go:linkname` access to internals**:
   Use `//go:linkname` to access unexported functions/types from `crypto/tls`. Create a modified version of the encrypt path that adds padding. Key steps:
   - Access `halfConn` struct layout via unsafe
   - Before `Handshake()`, install a hook that adds padding bytes
   - Alternatively: create a custom `tls.Conn` wrapper that intercepts `writeRecordLocked`

   **Approach C — Replace cert content to match target size**:
   If linkname is too fragile, fall back to making the certificate DER itself the target size by adding X.509 extensions with padding data. This avoids touching crypto/tls entirely. The `x509.Certificate.ExtraExtensions` field allows adding arbitrary extensions with padding bytes. Generate cert with exact target size = no record padding needed.

3. Create `wire/tlspad/pad.go`:
   ```go
   package tlspad

   // PaddedTLSConfig wraps a tls.Config to add record padding to server handshake.
   // paddingLen is the number of zero bytes to add to Certificate records.
   func PaddedTLSConfig(base *tls.Config, certPadding int) *tls.Config

   // Or if using cert-level padding:
   // PadCertificate returns a new tls.Certificate with the cert DER padded
   // to targetLen using X.509 extensions.
   func PadCertificate(cert tls.Certificate, targetLen int) (tls.Certificate, error)
   ```

4. Create `wire/tlspad/pad_test.go`:
   - Test: TLS handshake succeeds with padding
   - Test: Padded record length matches expected value
   - Test: Standard Go TLS client can connect to padded server
   - Test: uTLS client can connect to padded server
   - Measure actual encrypted record lengths by wrapping net.Conn to capture bytes

#### TDD Steps

**RED**: Write test that creates a TLS server with padding=2000, connects a client, measures the Certificate encrypted record length, asserts it's at least 2000 bytes larger than without padding.

**GREEN**: Implement the padding mechanism (whichever approach works).

**REFACTOR**: Clean API, ensure padding amount is per-connection configurable.

---

### T2: Dual Keypair Generation + Target Probing

**ID**: T2
**depends_on**: []
**files**: `wire/certgen.go`, `wire/certgen_test.go`
**AC coverage**: AC5, AC6, AC7, AC8, AC9, AC10

#### Implementation

1. Create `wire/certgen.go`:
   ```go
   package wire

   // CertPair holds RSA + EC self-signed certificates.
   type CertPair struct {
       RSA tls.Certificate  // RSA-2048
       EC  tls.Certificate  // ECDSA P-256
       RSAPinHash [32]byte  // sha256(PKIX public key)
       ECPinHash  [32]byte
   }

   // GenerateCertPair creates RSA-2048 + ECDSA P-256 self-signed certificates.
   // Certs have 10-year validity, minimal subject.
   func GenerateCertPair() (*CertPair, error)

   // SaveCertPair persists both keypairs to disk.
   func SaveCertPair(dir string, pair *CertPair) error

   // LoadCertPair loads both keypairs from disk.
   func LoadCertPair(dir string) (*CertPair, error)

   // EnsureCertPair loads or generates + saves a cert pair.
   func EnsureCertPair(dir string) (*CertPair, error)

   // TargetProfile describes a target site's TLS characteristics.
   type TargetProfile struct {
       KeyAlgo    string // "rsa" or "ec"
       CertChainLen int  // total Certificate message DER length
   }

   // ProbeTarget connects to host:443 and measures TLS characteristics.
   func ProbeTarget(host string) (*TargetProfile, error)

   // FormatPins returns the pin URL parameter value for both keys.
   // Format: "sha256:<rsa_b64>,sha256:<ec_b64>"
   func (cp *CertPair) FormatPins() string
   ```

2. Create `wire/certgen_test.go`:
   - Test: `GenerateCertPair` produces valid RSA+EC certs
   - Test: RSA cert has RSA public key, EC cert has EC public key
   - Test: Save + Load round-trip preserves both certs
   - Test: `ProbeTarget("www.cloudflare.com")` returns valid profile (integration test, may skip in CI)
   - Test: Pin hashes are deterministic for same keypair

#### TDD Steps

**RED**: Test that `GenerateCertPair()` returns non-nil, RSA cert key type is RSA, EC cert key type is ECDSA.

**GREEN**: Implement `GenerateCertPair` using `crypto/rsa.GenerateKey` + `crypto/ecdsa.GenerateKey` + `x509.CreateCertificate`.

**REFACTOR**: Add Save/Load, EnsureCertPair, ProbeTarget, FormatPins.

---

### T3: Server Integration + Client Pin + E2E

**ID**: T3
**depends_on**: [T1, T2]
**files**: `wire/ech.go`, `wire/config.go`, `server/server.go`, `wire/ech_test.go`, `server/server_test.go`
**AC coverage**: AC11, AC12, AC13, AC14, AC15

#### Implementation

1. **Config changes** (`wire/config.go`):
   - Add `Pin string` field to `Config` struct
   - Parse `pin=sha256:AAA,sha256:BBB` from URL into `[][]byte`
   - Add `ParsePins(pin string) ([][32]byte, error)`

2. **Client pin verification** (`wire/ech.go`):
   - In `BuildClientTLSConfig`, when `cfg.Pin` is non-empty:
     - Set `InsecureSkipVerify: true`
     - Set `VerifyPeerCertificate` callback that:
       a. Parses the leaf cert
       b. Extracts public key via `x509.MarshalPKIXPublicKey`
       c. Computes `sha256(pubKeyDER)`
       d. Matches against any pin in the list
   - Keep existing `cfg.Insecure` behavior for backward compat

3. **Server cert selection** (`server/server.go`):
   - Load `CertPair` via `wire.EnsureCertPair(cfg.CertDir)`
   - Replace static `tls.Config.Certificates` with `GetCertificate` callback:
     ```go
     GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
         // hello.ServerName is outer SNI (before ECH decryption)
         profile := probeCache.Get(hello.ServerName)
         if profile.KeyAlgo == "rsa" {
             return &certPair.RSA, nil
         }
         return &certPair.EC, nil
     }
     ```
   - Note: `GetCertificate` is called BEFORE ECH decryption, so `ServerName` is the outer SNI

4. **Padding integration** (`server/server.go`):
   - Use T1's padding mechanism when creating TLS config
   - Padding amount = `profile.CertChainLen - ourCertLen`

5. **E2E test** (`server/server_test.go` or `wire/ech_test.go`):
   - Start server with dual certs + padding
   - Client connects with pin verification
   - Verify handshake succeeds
   - Verify correct cert is returned based on probe profile

#### TDD Steps

**RED**: Test client with `pin=sha256:...` connects to server, pin matches → success. Wrong pin → failure.

**GREEN**: Implement pin parsing + VerifyPeerCertificate + GetCertificate.

**REFACTOR**: Add padding integration, probe cache, e2e test.

## AC Coverage Map

| AC | Test | Task |
|----|------|------|
| AC1 | `TestPaddedHandshake` | T1 |
| AC2 | `TestPaddedRecordLength` | T1 |
| AC3 | `TestStandardClientWithPadding` | T1 |
| AC4 | `TestPerConnectionPadding` | T1 |
| AC5 | `TestGenerateCertPair` | T2 |
| AC6 | `TestCertPairSaveLoad` | T2 |
| AC7 | `TestCertVerifyLengthMatch` | T2 |
| AC8 | `TestProbeTargetAlgorithm` | T2 |
| AC9 | `TestProbeTargetCertLength` | T2 |
| AC10 | `TestProbeCache` | T2 |
| AC11 | `TestCertSelectionRSA` | T3 |
| AC12 | `TestGetCertificateCallback` | T3 |
| AC13 | `TestPinVerificationSuccess` | T3 |
| AC14 | `TestPinVerificationBothAlgos` | T3 |
| AC15 | `TestBuildClientTLSConfigWithPin` | T3 |
