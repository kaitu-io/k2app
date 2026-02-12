# Plan: Zero-Config Stealth

## Tasks

### T1: ECH Config Forging from Cloudflare DNS
- **AC**: AC1
- **depends_on**: []
- **files**:
  - NEW `wire/ech_provider.go` — ECHProvider interface + LocalECHProvider
  - MODIFY `wire/ech.go` — add GenerateECHKeyFromTemplate (accepts template fields)
  - NEW `wire/ech_provider_test.go`
- **details**:
  1. Create `ECHTemplate` struct: ConfigID uint8, KemID uint16, CipherSuites []byte, PublicName string
  2. Create `ECHProvider` interface with `FetchTemplate() (*ECHTemplate, error)`
  3. Implement `LocalECHProvider`:
     - DNS query `cloudflare.com` type HTTPS (65) using `github.com/miekg/dns` (already in go.mod)
     - Parse response: `dns.HTTPS` → iterate `Value` for `*dns.SVCBECHConfig` → `.ECH` bytes
     - Parse ECHConfigList bytes → extract first ECHConfig → decode config_id, kem_id, cipher_suites, public_name
     - Return `ECHTemplate`
  4. Add `ParseECHConfigBytes(data []byte) (*ECHTemplate, error)` — binary parser for ECHConfig wire format:
     - version(2) + length(2) + config_id(1) + kem_id(2) + pub_key_len(2) + pub_key(N) + cs_len(2) + cs(M) + max_name_len(1) + pub_name_len(1) + pub_name(P) + ext_len(2) + ext(Q)
  5. Add `GenerateECHKeyFromTemplate(tmpl *ECHTemplate, privKey *ecdh.PrivateKey) (tls.EncryptedClientHelloKey, []byte, error)`:
     - Build ECHConfig with config_id = tmpl.ConfigID + 1, copy kem_id/cipher_suites/public_name, use provided public key
     - Return key + ECHConfigList bytes
  6. Fallback: cache template to `cert_dir/ech_template.json` on success, load from cache if DNS fails, hardcoded defaults as last resort
  7. Tests: parse known ECHConfig bytes, DNS fetch (may need mock or skip in CI), template → forged config round-trip

### T2: ECH Config Rotation (Multi-Config)
- **AC**: AC2
- **depends_on**: [T1]
- **files**:
  - MODIFY `wire/ech.go` — new EnsureECHKeys function, split key storage from config storage
  - MODIFY `server/server.go` — load multiple ECH configs
  - NEW `wire/ech_rotation_test.go`
- **details**:
  1. Split ECH key storage:
     - `cert_dir/ech.key` — HPKE X25519 private key only (persistent, never regenerated)
     - `cert_dir/ech-configs/` — directory of ECH config files, one per config_id
     - File format per config: raw ECHConfig bytes (not ECHConfigList — no length prefix)
  2. New functions:
     - `SaveECHPrivateKey(path string, privKey []byte) error`
     - `LoadECHPrivateKey(path string) (*ecdh.PrivateKey, error)`
     - `EnsureECHPrivateKey(path string) (*ecdh.PrivateKey, error)` — load or generate + save
     - `SaveECHConfig(dir string, configID uint8, config []byte) error` — saves to ech-configs/config-{id}.bin
     - `LoadAllECHConfigs(dir string) ([][]byte, error)` — loads all .bin files from ech-configs/
     - `BuildECHKeys(privKey *ecdh.PrivateKey, configs [][]byte) []tls.EncryptedClientHelloKey`
  3. Server startup flow (server.go):
     - `privKey := EnsureECHPrivateKey(cert_dir/ech.key)`
     - `tmpl := provider.FetchTemplate()` (with fallback)
     - `newConfig := GenerateECHKeyFromTemplate(tmpl, privKey)` → save to ech-configs/
     - `allConfigs := LoadAllECHConfigs(cert_dir/ech-configs/)`
     - `s.TLSConfig.EncryptedClientHelloKeys = BuildECHKeys(privKey, allConfigs)`
     - Latest config (highest config_id) → used for URL output
  4. Backward compat: if old-format `ech.key` file exists (contains both key+config), migrate to new split format
  5. Tests: generate two configs with different config_ids, verify server TLS accepts both

### T3: Client CA Handling
- **AC**: AC3
- **depends_on**: []
- **files**:
  - NEW `wire/ca.go` — BuildDefaultCAPool + blockedCAOrgs
  - NEW `wire/kaitu_ca.pem` — Kaitu CA cert (download from k2.52j.me/api/ca at dev time)
  - MODIFY `wire/ech.go` — update BuildClientTLSConfig default branch
  - NEW `wire/ca_test.go`
- **details**:
  1. `wire/kaitu_ca.pem` — EC P-256 CA certificate, `//go:embed kaitu_ca.pem` in ca.go
  2. Chinese CA blacklist (matched against cert Subject.Organization):
     ```
     "CNNIC", "WoSign", "WoTrus", "CFCA", "GDCA", "SHECA",
     "China Financial Certification Authority",
     "Shanghai Electronic Certification Authority",
     "Guang Dong Certificate Authority"
     ```
  3. `BuildDefaultCAPool() *x509.CertPool`:
     - Go's `x509.SystemCertPool()` returns an opaque pool (no iteration API)
     - Strategy: use `x509.SystemCertPool()`, then set `VerifyPeerCertificate` to reject blocked certs post-verification
     - Better approach: use system pool as-is + Kaitu CA, block Chinese CAs via `VerifyPeerCertificate` callback that checks leaf/chain issuers against blacklist AFTER standard verification passes
  4. Update `BuildClientTLSConfig`:
     - pin set → current behavior (unchanged)
     - insecure → current behavior (unchanged)
     - default (no pin, no insecure) → `RootCAs = systemPool + kaituCA`, `VerifyPeerCertificate = rejectBlockedCAs`
  5. Tests: verify Kaitu CA is trusted, verify CNNIC-signed cert is rejected

### T4: Server Zero-Config Startup
- **AC**: AC4
- **depends_on**: [T1, T2]
- **files**:
  - MODIFY `config/config.go` — cert_dir default, ECHConfig struct update
  - MODIFY `server/server.go` — unified auto-provisioning in New()
  - MODIFY `cmd/k2s/main.go` — allow no config file
- **details**:
  1. Config defaults (config/config.go):
     - `cert_dir` default: `~/.k2s/` (Linux/macOS), `%APPDATA%\k2s\` (Windows)
     - `ech.provider` default: `"local"`
     - `listen` default: `":443"` (already exists)
     - New field: `ECHConfig.Provider string` (default "local")
     - Remove: `ECHConfig.KeyPath` and `ECHConfig.PublicName` (derived from provider)
  2. Server auto-provisioning (server.go New()):
     - Ensure cert_dir exists (os.MkdirAll)
     - `EnsureCertPair(cert_dir)` — existing, generates RSA+EC certs
     - `EnsureECHPrivateKey(cert_dir/ech.key)` — from T2
     - `LocalECHProvider.FetchTemplate()` — from T1
     - `GenerateECHKeyFromTemplate()` + save — from T1
     - `LoadAllECHConfigs()` + `BuildECHKeys()` — from T2
     - `ProbeTarget(template.PublicName)` — existing, for padding
     - Apply padding — existing
     - Generate auth token: `cert_dir/auth.json` with random token + UDID
     - Set `EncryptedClientHelloKeys` on TLSConfig
  3. URL logging:
     - After setup, log: `k2v5://UDID:TOKEN@HOST:PORT?ech=<b64>&pin=<dual_pins>`
     - HOST: from config or auto-detect (best-effort: external IP query or hostname)
  4. cmd/k2s/main.go: if no -s flag, create default ServerConfig with all defaults
  5. Tests: integration test — New() with minimal config, verify cert_dir populated, URL logged

### T5: Cleanup (SNI Removal + Spec Update)
- **AC**: AC5
- **depends_on**: [T1, T2, T3, T4]
- **files**:
  - MODIFY `wire/wire.go` — remove SNI field, remove sni parsing, remove www.microsoft.com default
  - MODIFY `wire/wire_test.go` — update tests
  - MODIFY `wire/integration_test.go` — update test URLs
  - MODIFY `wire/k2v5-protocol-spec.md` — Sections 2, 3.1, 3.2, 3.3, 12.4
  - MODIFY `docs/todos/p0-zero-config-stealth.md` — mark items complete
  - MODIFY `docs/todos/p0-zero-config-stealth-design.md` — update status table, revise decision #2
- **details**:
  1. wire/wire.go:
     - Remove `SNI string` from Config struct
     - Remove `cfg.SNI = "www.microsoft.com"` default
     - Remove `if v := q.Get("sni"); v != "" { cfg.SNI = v }` from ParseURL
     - Grep all usages of `cfg.SNI` and remove/update
  2. wire/wire_test.go + integration_test.go:
     - Remove sni from test URLs
     - Update expected Config values
  3. Protocol spec updates:
     - Section 2: Remove `sni` from URL format table, note outer SNI = ECH public_name
     - Section 3.1: Fix identity model — outer SNI = `cloudflare-ech.com` (from ECH config)
     - Section 3.2: Add ECH config forging subsection (DNS fetch, config_id+1, template mimicry)
     - Section 3.3: Dual pin format: `sha256:<rsa_b64>,sha256:<ec_b64>`
     - Section 12.4: Rewrite auto-provisioning to describe DNS-based flow
  4. Todo docs: mark all stealth items as complete except outer SNI randomization (removed from scope)
  5. Run `go test ./...` to verify nothing breaks
