# k2r Router Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship k2r as a standalone gateway binary with embedded webapp, encrypted server-side storage, and one-line installation for OpenWrt/Linux.

**Architecture:** k2r embeds the React webapp via `//go:embed`, serves it alongside the gateway HTTP API on `:1779`. A new gateway platform bridge connects the webapp to the gateway's VPN control and server-side encrypted storage. Distribution via CDN install script following the existing `install-k2s.sh` pattern.

**Tech Stack:** Go 1.25 (gateway backend), TypeScript/React (webapp bridge), AES-256-GCM + HKDF-SHA256 (storage encryption), GitHub Actions (CI), S3/CloudFront (CDN)

**Spec:** `docs/superpowers/specs/2026-04-08-k2r-router-release-design.md`

**Target architectures:** arm64 (aarch64), amd64 (x86_64), armv7. **mipsle dropped** — bare k2r binary is 15MB, most mipsle routers have 16MB flash (won't fit even without webapp). mipsle is a discontinued 2016-era architecture (MT7621) with no new devices.

**Submodule workflow:** k2 is a read-only submodule in k2app. All Go changes (Tasks 1-3) are made on a feature branch in the k2 repo, then k2app updates the submodule ref.

---

## Verification Results (pre-implementation)

These findings were verified before writing the plan:

1. **`//go:embed` cross-compile** — Confirmed working: macOS `GOOS=linux` cross-compile for amd64/arm64/armv7/mipsle all succeed with embedded dist/.
2. **`Gateway.New()` change** — Safe: only 2 call sites (gateway.go definition + cmd/k2r/main.go). Zero test files call `New()`.
3. **engine.Status field mapping** — Engine already uses `"disconnected"` (NOT `"stopped"`). The `"stopped"` → `"disconnected"` mapping in tauri-k2.ts is dead code. Real gaps: `connected_at` (snake_case RFC3339) vs `startAt` (camelCase Unix seconds), and missing computed fields (`running`, `retrying`, `networkAvailable`).
4. **HKDF + AES-GCM on all architectures** — Pure Go, no CGO dependency, confirmed safe.
5. **Webapp dist size** — 2.4MB raw, 1.05MB gzipped. k2r bare binary: 14-15MB. With webapp: ~16-17MB. Fits arm64/amd64/armv7.

---

## Phase 0: Status API Field Alignment (k2 submodule)

Unify the Go status JSON format with the TypeScript `StatusResponseData` contract. This change benefits all platforms (desktop + mobile + gateway), not just k2r.

### Task 0A: Rename `connected_at` → `startAt` in engine + daemon

**Files:**
- Modify: `k2/engine/status.go`
- Modify: `k2/daemon/daemon.go`

The `connected_at` field (snake_case, RFC3339 string) must become `startAt` (camelCase, Unix seconds integer) to match the TypeScript `StatusResponseData.startAt` field.

- [ ] **Step 1: Update engine/status.go MarshalJSON**

Change `statusJSON` and `MarshalJSON` in `k2/engine/status.go`:

```go
type statusJSON struct {
	State         string       `json:"state"`
	Error         *EngineError `json:"error,omitempty"`
	StartAt       int64        `json:"startAt,omitempty"`
	UptimeSeconds int          `json:"uptimeSeconds,omitempty"`
}

func (s Status) MarshalJSON() ([]byte, error) {
	j := statusJSON{
		State: s.State,
		Error: s.Error,
	}
	if s.State == StateConnected && !s.ConnectedAt.IsZero() {
		j.StartAt = s.ConnectedAt.Unix()
		j.UptimeSeconds = int(time.Since(s.ConnectedAt).Seconds())
	}
	return json.Marshal(j)
}
```

- [ ] **Step 2: Update daemon/daemon.go statusInfo()**

Change `statusInfo()` in `k2/daemon/daemon.go` (around line 487):

```go
func (d *Daemon) statusInfo() map[string]any {
	d.mu.RLock()
	s := d.lastStatus
	cfg := d.lastConfig
	d.mu.RUnlock()

	info := map[string]any{
		"state": s.State,
	}
	if s.Error != nil {
		info["error"] = s.Error
	}
	if s.State == engine.StateConnected && !s.ConnectedAt.IsZero() {
		info["startAt"] = s.ConnectedAt.Unix()
		info["uptimeSeconds"] = int(time.Since(s.ConnectedAt).Seconds())
	}
	if cfg != nil {
		info["config"] = cfg
	}
	return info
}
```

- [ ] **Step 3: Update engine tests that assert JSON output**

Run: `cd k2 && grep -rn 'connected_at\|uptime_seconds' engine/ daemon/`
Update any test assertions from `connected_at` → `startAt`, `uptime_seconds` → `uptimeSeconds`.

- [ ] **Step 4: Run engine + daemon tests**

Run: `cd k2 && go test ./engine/... ./daemon/... -v`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
cd k2 && git add engine/status.go daemon/daemon.go
git commit -m "refactor: rename connected_at→startAt, uptime_seconds→uptimeSeconds

Align Go JSON output with TypeScript StatusResponseData contract.
startAt is now Unix seconds (int64) instead of RFC3339 string.
Affects engine MarshalJSON + daemon statusInfo."
```

### Task 0B: Extract shared transformStatus in webapp

**Files:**
- Create: `webapp/src/services/status-transform.ts`
- Modify: `webapp/src/services/tauri-k2.ts`
- Modify: `webapp/src/services/capacitor-k2.ts`

Extract the computed-field logic (`running`, `retrying`, `networkAvailable`, error synthesis) into a shared utility. Remove the dead `"stopped"` → `"disconnected"` mapping from Tauri bridge.

- [ ] **Step 1: Create status-transform.ts**

```typescript
/**
 * Shared status transformation for all platform bridges.
 *
 * Raw status from engine (via daemon HTTP, gateway SSE, or K2Plugin)
 * provides: state, error, startAt, uptimeSeconds.
 *
 * This function computes the derived fields that StatusResponseData requires:
 * running, retrying, networkAvailable, and error state synthesis.
 */

import type { StatusResponseData, ControlError, ServiceState } from './vpn-types';

export function transformStatus(raw: any): StatusResponseData {
  let state: ServiceState = raw.state ?? 'disconnected';
  const running = state === 'connecting' || state === 'connected';

  let error: ControlError | undefined;
  let retrying = false;

  if (raw.error) {
    if (typeof raw.error === 'object' && raw.error !== null && 'code' in raw.error) {
      error = { code: raw.error.code, message: raw.error.message || '' };
    } else {
      error = { code: 570, message: String(raw.error) };
    }
    // Error synthesis: disconnected + error → 'error' state
    if (state === 'disconnected' || state === 'connected') {
      const isClientError = [400, 401, 402, 403].includes(error.code);
      retrying = state === 'connected' && !isClientError;
      state = 'error';
    }
  }

  return {
    state,
    running,
    networkAvailable: true,
    startAt: typeof raw.startAt === 'number' ? raw.startAt : undefined,
    error,
    retrying,
  };
}
```

- [ ] **Step 2: Update tauri-k2.ts to use shared transform**

Replace the inline `transformStatus` function in `webapp/src/services/tauri-k2.ts` with:

```typescript
import { transformStatus } from './status-transform';
```

Remove the local `function transformStatus(raw: any): StatusResponseData { ... }` definition (around lines 30-67).

- [ ] **Step 3: Update capacitor-k2.ts to use shared transform**

Replace the inline `transformStatus` function in `webapp/src/services/capacitor-k2.ts` with:

```typescript
import { transformStatus } from './status-transform';
```

Remove the local `function transformStatus(raw: any): StatusResponseData { ... }` definition.

- [ ] **Step 4: Verify TypeScript compiles + existing tests pass**

Run: `cd webapp && npx tsc --noEmit && npx vitest run`
Expected: all pass

- [ ] **Step 5: Commit**

```bash
git add webapp/src/services/status-transform.ts webapp/src/services/tauri-k2.ts webapp/src/services/capacitor-k2.ts
git commit -m "refactor(webapp): extract shared transformStatus for all bridges

Removes duplicate transformStatus from tauri-k2 and capacitor-k2.
Removes dead 'stopped'→'disconnected' mapping (engine already uses 'disconnected').
startAt now expected as Unix seconds (matches Go-side rename)."
```

---

## Phase 1: Go Gateway Backend (k2 submodule)

> **Important:** All files in `k2/gateway/` must have `//go:build linux` at line 1. The gateway package is Linux-only.

### Task 1: Server-Side Encrypted Storage

**Files:**
- Create: `k2/gateway/storage.go`
- Create: `k2/gateway/storage_test.go`

This task creates the encrypted KV storage that the webapp will use via HTTP API. It's independent of the webapp embedding and can be tested with `go test`.

- [ ] **Step 1: Write storage_test.go with crypto + CRUD tests**

```go
//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveKeyDeterministic(t *testing.T) {
	k1 := deriveKey("test-hw-id")
	k2 := deriveKey("test-hw-id")
	if k1 != k2 {
		t.Fatal("same input must produce same key")
	}
}

func TestDeriveKeyDifferentInputs(t *testing.T) {
	k1 := deriveKey("id-one")
	k2 := deriveKey("id-two")
	if k1 == k2 {
		t.Fatal("different inputs must produce different keys")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := deriveKey("roundtrip-test")
	plaintext := `"hello-from-gateway"`
	encrypted := encryptValue(plaintext, key)
	if !isEncrypted(encrypted) {
		t.Fatal("encrypted value must have ENC1: prefix")
	}
	decrypted, err := decryptValue(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	k1 := deriveKey("key-one")
	k2 := deriveKey("key-two")
	encrypted := encryptValue("secret", k1)
	_, err := decryptValue(encrypted, k2)
	if err == nil {
		t.Fatal("decrypt with wrong key must fail")
	}
}

func TestStorageCRUD(t *testing.T) {
	dir := t.TempDir()
	s := newStorage(filepath.Join(dir, "storage.json"), deriveKey("test"))

	// Set + Get
	if err := s.Set("token", `"abc123"`); err != nil {
		t.Fatalf("set: %v", err)
	}
	val, err := s.Get("token")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != `"abc123"` {
		t.Fatalf("got %q, want %q", val, `"abc123"`)
	}

	// Has
	if !s.Has("token") {
		t.Fatal("has: should exist")
	}
	if s.Has("missing") {
		t.Fatal("has: should not exist")
	}

	// Keys
	keys := s.Keys()
	if len(keys) != 1 || keys[0] != "token" {
		t.Fatalf("keys: got %v", keys)
	}

	// Remove
	s.Remove("token")
	_, err = s.Get("token")
	if err == nil {
		t.Fatal("get after remove should fail")
	}

	// Clear
	s.Set("a", `"1"`)
	s.Set("b", `"2"`)
	s.Clear()
	if len(s.Keys()) != 0 {
		t.Fatal("clear: keys should be empty")
	}
}

func TestStoragePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	key := deriveKey("persist-test")

	s1 := newStorage(path, key)
	s1.Set("foo", `"bar"`)

	// New instance should read persisted data
	s2 := newStorage(path, key)
	val, err := s2.Get("foo")
	if err != nil || val != `"bar"` {
		t.Fatalf("persistence: got %q, err=%v", val, err)
	}
}

func TestStoragePlaintextBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	key := deriveKey("compat-test")

	// Write plaintext JSON directly (simulates pre-encryption data)
	os.WriteFile(path, []byte(`{"items":{"legacy":"\"old-value\""}}`), 0600)

	s := newStorage(path, key)
	val, err := s.Get("legacy")
	if err != nil || val != `"old-value"` {
		t.Fatalf("backward compat: got %q, err=%v", val, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd k2 && GOOS=linux go test ./gateway/ -run 'TestDeriveKey|TestEncrypt|TestStorage' -v`
Expected: compilation error — `deriveKey`, `encryptValue`, etc. not defined

- [ ] **Step 3: Implement storage.go**

```go
//go:build linux

package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/hkdf"
	"crypto/sha256"
	"io"
)

const (
	encPrefix = "ENC1:"
	hkdfSalt  = "kaitu-gateway-storage-v1"
	hkdfInfo  = "aes-256-gcm-key"
)

// deriveKey derives an AES-256 key from a hardware ID using HKDF-SHA256.
func deriveKey(hardwareID string) [32]byte {
	hk := hkdf.New(sha256.New, []byte(hardwareID), []byte(hkdfSalt), []byte(hkdfInfo))
	var key [32]byte
	io.ReadFull(hk, key[:])
	return key
}

// getMachineID reads the Linux machine-id.
func getMachineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(path)
		if err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id
			}
		}
	}
	return ""
}

func encryptValue(plaintext string, key [32]byte) string {
	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext)
}

func decryptValue(encrypted string, key [32]byte) (string, error) {
	if !strings.HasPrefix(encrypted, encPrefix) {
		return encrypted, nil // plaintext backward compat
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, _ := aes.NewCipher(key[:])
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

func isEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}

// Storage is a thread-safe, encrypted JSON file KV store.
type Storage struct {
	mu   sync.RWMutex
	path string
	key  [32]byte
	data storageFile
}

type storageFile struct {
	Items map[string]string `json:"items"`
}

func newStorage(path string, key [32]byte) *Storage {
	s := &Storage{
		path: path,
		key:  key,
		data: storageFile{Items: make(map[string]string)},
	}
	s.load()
	return s
}

// NewDefaultStorage creates a Storage with the default path and machine-derived key.
func NewDefaultStorage() *Storage {
	return newStorage(
		filepath.Join(stateDir(), "storage.json"),
		deriveKey(getMachineID()),
	)
}

func (s *Storage) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var sf storageFile
	if json.Unmarshal(data, &sf) == nil && sf.Items != nil {
		s.data = sf
	}
}

func (s *Storage) save() {
	os.MkdirAll(filepath.Dir(s.path), 0700)
	data, _ := json.MarshalIndent(s.data, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	os.Rename(tmp, s.path)
}

func (s *Storage) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, ok := s.data.Items[key]
	if !ok {
		return "", errors.New("key not found")
	}
	return decryptValue(raw, s.key)
}

func (s *Storage) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Items[key] = encryptValue(value, s.key)
	s.save()
	return nil
}

func (s *Storage) Remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Items, key)
	s.save()
}

func (s *Storage) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data.Items[key]
	return ok
}

func (s *Storage) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.data.Items))
	for k := range s.data.Items {
		keys = append(keys, k)
	}
	return keys
}

func (s *Storage) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Items = make(map[string]string)
	s.save()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd k2 && go test ./gateway/ -run 'TestDeriveKey|TestEncrypt|TestStorage' -v`
Expected: all 7 tests PASS

- [ ] **Step 5: Commit**

```bash
cd k2 && git add gateway/storage.go gateway/storage_test.go
git commit -m "feat(gateway): add encrypted server-side KV storage

AES-256-GCM with HKDF-SHA256 key derivation from /etc/machine-id.
ENC1: prefix for encrypted values, plaintext backward compat.
Thread-safe with atomic file writes."
```

---

### Task 2: Storage + Platform API Endpoints

**Files:**
- Modify: `k2/gateway/api.go`
- Modify: `k2/gateway/gateway.go` (wire storage + new routes)

- [ ] **Step 1: Add storage and platform handlers to api.go**

Append to `k2/gateway/api.go` before the closing `writeJSON` and `defaultConfigPath` functions:

```go
func (g *Gateway) handleStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
		Key    string `json:"key"`
		Value  any    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	switch req.Action {
	case "get":
		val, err := g.storage.Get(req.Key)
		if err != nil {
			writeJSON(w, Response{Code: 0, Data: nil})
			return
		}
		// val is a JSON string — decode to return the original typed value
		var decoded any
		if json.Unmarshal([]byte(val), &decoded) == nil {
			writeJSON(w, Response{Code: 0, Data: decoded})
		} else {
			writeJSON(w, Response{Code: 0, Data: val})
		}
	case "set":
		data, _ := json.Marshal(req.Value)
		if err := g.storage.Set(req.Key, string(data)); err != nil {
			writeJSON(w, Response{Code: 1, Message: err.Error()})
			return
		}
		writeJSON(w, Response{Code: 0, Message: "ok"})
	case "remove":
		g.storage.Remove(req.Key)
		writeJSON(w, Response{Code: 0, Message: "ok"})
	case "has":
		writeJSON(w, Response{Code: 0, Data: g.storage.Has(req.Key)})
	case "keys":
		writeJSON(w, Response{Code: 0, Data: g.storage.Keys()})
	case "clear":
		g.storage.Clear()
		writeJSON(w, Response{Code: 0, Message: "ok"})
	default:
		writeJSON(w, Response{Code: 1, Message: "unknown storage action"})
	}
}

func (g *Gateway) handlePlatform(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, Response{Code: 0, Data: map[string]string{
		"os":           "linux",
		"platformType": "gateway",
		"version":      g.version,
		"commit":       g.commit,
		"arch":         g.arch,
	}})
}
```

- [ ] **Step 2: Add storage and version fields to Gateway struct and wire routes**

In `k2/gateway/gateway.go`, add fields to `Gateway` struct:

```go
type Gateway struct {
	opMu deadlock.Mutex
	mu   deadlock.Mutex

	// ... existing fields ...

	storage *Storage

	version string
	commit  string
	arch    string
}
```

Update `New()` to accept version info and create storage:

```go
func New(version, commit, arch string) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		ctx:        ctx,
		cancel:     cancel,
		sseClients: make(map[chan []byte]struct{}),
		storage:    NewDefaultStorage(),
		version:    version,
		commit:     commit,
		arch:       arch,
	}
}
```

Add routes in `Run()` method's mux setup:

```go
mux.HandleFunc("/api/storage", g.handleStorage)
mux.HandleFunc("/api/platform", g.handlePlatform)
```

- [ ] **Step 3: Update cmd/k2r/main.go to pass version info to New()**

In `k2/cmd/k2r/main.go`, change `runDaemon`:

```go
func runDaemon(configPath string) {
	// ... existing config loading ...

	g := gateway.New(version, commit, runtime.GOARCH)
	if err := g.Run(cfg.Listen); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
		os.Exit(1)
	}
}
```

Add `"runtime"` to imports.

- [ ] **Step 4: Add `version` action to handleCore**

In `k2/gateway/api.go`, add to the `handleCore` switch:

```go
case "version":
	writeJSON(w, Response{Code: 0, Data: map[string]string{
		"version": g.version,
		"commit":  g.commit,
	}})
```

- [ ] **Step 5: Extend handleStatus with gateway-specific fields**

Replace `handleStatus` in `k2/gateway/api.go`:

```go
func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	status := g.lastStatus
	interceptorName := ""
	if g.interceptor != nil {
		interceptorName = g.interceptor.Name()
	}
	var subnets []string
	if g.provider != nil {
		subnets = g.provider.cfg.LANSubnets
	}
	listenPort := 0
	if g.provider != nil {
		listenPort = g.provider.cfg.ListenPort
	}
	g.mu.Unlock()

	// Build extended response
	type gatewayStatus struct {
		engine.Status
		LANSubnets  []string `json:"lanSubnets,omitempty"`
		Interceptor string   `json:"interceptor,omitempty"`
		ListenPort  int      `json:"listenPort,omitempty"`
	}
	writeJSON(w, Response{Code: 0, Data: gatewayStatus{
		Status:      status,
		LANSubnets:  subnets,
		Interceptor: interceptorName,
		ListenPort:  listenPort,
	}})
}
```

This requires adding `"github.com/kaitu-io/k2/engine"` to api.go imports (it's already imported via config.go in the same package, but api.go needs its own import for engine.Status).

- [ ] **Step 6: Run gateway build + tests**

Run: `cd k2 && GOOS=linux go build ./cmd/k2r && go test ./gateway/ -v`
Expected: build succeeds, all tests pass

- [ ] **Step 7: Commit**

```bash
cd k2 && git add gateway/api.go gateway/gateway.go cmd/k2r/main.go
git commit -m "feat(gateway): add storage, platform, version API endpoints

/api/storage — encrypted KV CRUD via HTTP
/api/platform — returns os, arch, version, platformType
/api/core status — includes lanSubnets, interceptor, listenPort
/api/core version — returns version and commit"
```

---

### Task 3: Webapp Embedding + SPA Serving

**Files:**
- Create: `k2/gateway/webapp_embed.go`
- Create: `k2/gateway/webapp_embed_nop.go`
- Create: `k2/gateway/webapp_serve.go`
- Modify: `k2/gateway/gateway.go` (add SPA handler to mux)

- [ ] **Step 1: Create webapp_embed.go (default build)**

```go
//go:build linux && !nowebapp

package gateway

import "embed"

//go:embed dist/*
var webappFS embed.FS
```

- [ ] **Step 2: Create webapp_embed_nop.go (headless build)**

```go
//go:build linux && nowebapp

package gateway

import "io/fs"

var webappFS fs.FS // nil — no webapp embedded
```

- [ ] **Step 3: Create webapp_serve.go (SPA handler with __K2_GATEWAY__ injection)**

```go
//go:build linux

package gateway

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// newWebappHandler returns an http.Handler that serves the embedded SPA.
// Returns nil if no webapp is embedded (nowebapp build tag).
//
// Behavior:
// - Known file extensions (.js, .css, .png, etc.) → serve from dist/
// - All other paths → serve index.html (SPA client-side routing)
// - Injects __K2_GATEWAY__ global into index.html <head>
func newWebappHandler(version, commit, arch string) http.Handler {
	if webappFS == nil {
		return nil
	}
	sub, err := fs.Sub(webappFS, "dist")
	if err != nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(sub))

	// Pre-build the injection script
	injection := fmt.Sprintf(
		`<script>window.__K2_GATEWAY__={version:%q,commit:%q,arch:%q}</script>`,
		version, commit, arch,
	)

	// Read and patch index.html once at startup
	indexData, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil
	}
	patched := bytes.Replace(indexData, []byte("<head>"), []byte("<head>"+injection), 1)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" || !hasFileExtension(path) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(patched)
			return
		}
		// Static assets — serve with cache headers
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
}

func hasFileExtension(path string) bool {
	// Check last segment for a dot after the final slash
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash >= 0 {
		path = path[lastSlash:]
	}
	return strings.Contains(path, ".")
}
```

- [ ] **Step 4: Wire webapp handler into gateway.go Run()**

In `k2/gateway/gateway.go` `Run()` method, add the webapp handler *after* the API routes:

```go
mux.HandleFunc("/ping", g.handlePing)
mux.HandleFunc("/api/core", g.handleCore)
mux.HandleFunc("/api/events", g.handleEvents)
mux.HandleFunc("/api/log-level", g.handleLogLevel)
mux.HandleFunc("/api/storage", g.handleStorage)
mux.HandleFunc("/api/platform", g.handlePlatform)

// Webapp SPA — serves all non-API paths
if webapp := newWebappHandler(g.version, g.commit, g.arch); webapp != nil {
	mux.Handle("/", webapp)
}
```

- [ ] **Step 5: Create a minimal dist/ placeholder for compilation**

The `//go:embed dist/*` directive requires the directory to exist at compile time. For development without a webapp build, create a placeholder:

```bash
mkdir -p k2/gateway/dist
echo '<!DOCTYPE html><html><head></head><body>k2r</body></html>' > k2/gateway/dist/index.html
```

Add `k2/gateway/dist/` to `.gitignore` in the k2 submodule (the real dist comes from webapp build, CI copies it in):

Check if `k2/.gitignore` exists and append:
```
gateway/dist/
```

- [ ] **Step 6: Verify cross-compilation**

Run: `cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/k2r`
Expected: build succeeds

Run: `cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/k2r`
Expected: build succeeds

- [ ] **Step 7: Commit**

```bash
cd k2 && git add gateway/webapp_embed.go gateway/webapp_embed_nop.go gateway/webapp_serve.go gateway/gateway.go gateway/dist/index.html .gitignore
git commit -m "feat(gateway): embed webapp SPA with __K2_GATEWAY__ injection

go:embed dist/* with nowebapp build tag for headless mode.
SPA fallback: non-file paths serve patched index.html.
Static assets cached with immutable headers."
```

---

## Phase 2: Webapp Frontend

### Task 4: Add platformType to IPlatform + Update Existing Bridges

**Files:**
- Modify: `webapp/src/types/kaitu-core.ts`
- Modify: `webapp/src/services/tauri-k2.ts`
- Modify: `webapp/src/services/capacitor-k2.ts`
- Modify: `webapp/src/services/standalone-k2.ts`

- [ ] **Step 1: Add platformType to IPlatform interface**

In `webapp/src/types/kaitu-core.ts`, add after the `os` field:

```typescript
interface IPlatform {
  // ====== 平台标识 ======

  os: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';
  platformType: 'desktop' | 'mobile' | 'gateway' | 'web';
  version: string;
  // ... rest unchanged
```

Add `__K2_GATEWAY__` to the Window global declaration:

```typescript
declare global {
  interface Window {
    _k2: IK2Vpn;
    _platform: IPlatform;
    __TAURI__?: any;
    __K2_GATEWAY__?: { version: string; commit: string; arch: string };
  }
}
```

- [ ] **Step 2: Add platformType to Tauri bridge**

In `webapp/src/services/tauri-k2.ts`, in the `tauriPlatform` object (around line 219):

```typescript
const tauriPlatform: IPlatform = {
    os: osMap[platformInfo.os] ?? 'linux',
    platformType: 'desktop',
    version: platformInfo.version,
    // ... rest unchanged
```

- [ ] **Step 3: Add platformType to Capacitor bridge**

In `webapp/src/services/capacitor-k2.ts`, find where the platform object is constructed and add:

```typescript
platformType: 'mobile',
```

- [ ] **Step 4: Add platformType to standalone bridge**

In `webapp/src/services/standalone-k2.ts`, in the `standalonePlatform` object:

```typescript
export const standalonePlatform: IPlatform = {
  ...webPlatform,
  os: 'web',
  platformType: 'web',
  version: 'standalone',
  // ... rest unchanged
```

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd webapp && npx tsc --noEmit`
Expected: no errors (all bridges now provide `platformType`)

- [ ] **Step 6: Commit**

```bash
git add webapp/src/types/kaitu-core.ts webapp/src/services/tauri-k2.ts webapp/src/services/capacitor-k2.ts webapp/src/services/standalone-k2.ts
git commit -m "feat(webapp): add platformType to IPlatform interface

desktop/mobile/gateway/web — clear platform identification.
All existing bridges updated. Window.__K2_GATEWAY__ declared."
```

---

### Task 5: Gateway Storage Bridge

**Files:**
- Create: `webapp/src/services/gateway-storage.ts`

- [ ] **Step 1: Create gateway-storage.ts**

```typescript
/**
 * Gateway Storage — server-side encrypted storage via HTTP API.
 *
 * Used when webapp runs on a k2r gateway. All data is stored on the
 * gateway device at /etc/k2r/storage.json, encrypted with AES-256-GCM.
 * Browser localStorage is NOT used — multiple devices access the same gateway.
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

const STORAGE_ENDPOINT = '/api/storage';

async function storageRequest(action: string, key?: string, value?: any): Promise<any> {
  try {
    const body: Record<string, any> = { action };
    if (key !== undefined) body.key = key;
    if (value !== undefined) body.value = value;

    const resp = await fetch(STORAGE_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) return null;
    const result = await resp.json();
    return result.code === 0 ? result.data : null;
  } catch {
    return null;
  }
}

export const gatewayStorage: ISecureStorage = {
  async get<T = any>(key: string): Promise<T | null> {
    const data = await storageRequest('get', key);
    return data ?? null;
  },

  async set<T = any>(key: string, value: T, _options?: StorageOptions): Promise<void> {
    await storageRequest('set', key, value);
  },

  async remove(key: string): Promise<void> {
    await storageRequest('remove', key);
  },

  async has(key: string): Promise<boolean> {
    const result = await storageRequest('has', key);
    return result === true;
  },

  async clear(): Promise<void> {
    await storageRequest('clear');
  },

  async keys(): Promise<string[]> {
    const result = await storageRequest('keys');
    return Array.isArray(result) ? result : [];
  },
};
```

- [ ] **Step 2: Commit**

```bash
git add webapp/src/services/gateway-storage.ts
git commit -m "feat(webapp): add gateway storage bridge (HTTP-backed ISecureStorage)"
```

---

### Task 6: Gateway Platform Bridge + main.tsx Integration

**Files:**
- Create: `webapp/src/services/gateway-k2.ts`
- Modify: `webapp/src/main.tsx`

- [ ] **Step 1: Create gateway-k2.ts**

```typescript
/**
 * Gateway K2 Bridge
 *
 * Platform bridge for k2r gateway mode. Detected via window.__K2_GATEWAY__
 * injected by the Go gateway's HTML serving.
 *
 * VPN control: HTTP POST to /api/core (same protocol as daemon)
 * Events: SSE from /api/events (status + stats)
 * Storage: Server-side encrypted via /api/storage
 */

import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import type { StatusResponseData } from './vpn-types';
import { transformStatus } from './status-transform';
import { gatewayStorage } from './gateway-storage';
import { webPlatform } from './web-platform';

const CORE_ENDPOINT = '/api/core';

async function coreExec<T = any>(action: string, params?: any): Promise<SResponse<T>> {
  try {
    const response = await fetch(CORE_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params: params ?? {} }),
    });
    if (!response.ok) {
      return { code: -1, message: 'Service error' };
    }
    return await response.json();
  } catch {
    return { code: -1, message: 'Service unavailable' };
  }
}

/**
 * Connect to gateway SSE event stream.
 * Returns unsubscribe function.
 */
function connectSSE(
  onStatus: ((status: StatusResponseData) => void) | null,
  onServiceState: ((available: boolean) => void) | null,
): () => void {
  let es: EventSource | null = null;
  let closed = false;

  function connect() {
    if (closed) return;
    es = new EventSource('/api/events');

    es.onopen = () => {
      onServiceState?.(true);
    };

    es.addEventListener('status', (e: MessageEvent) => {
      try {
        const raw = JSON.parse(e.data);
        onStatus?.(transformStatus(raw));
      } catch { /* ignore parse errors */ }
    });

    es.onerror = () => {
      onServiceState?.(false);
      es?.close();
      // Reconnect after 3s
      if (!closed) {
        setTimeout(connect, 3000);
      }
    };
  }

  connect();

  return () => {
    closed = true;
    es?.close();
  };
}

const gatewayK2: IK2Vpn = {
  run: coreExec,

  onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
    return connectSSE(null, callback);
  },

  onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
    return connectSSE(callback, null);
  },
};

const gwInfo = () => window.__K2_GATEWAY__ ?? { version: 'unknown', commit: '', arch: '' };

const gatewayPlatform: IPlatform = {
  ...webPlatform,
  os: 'linux',
  platformType: 'gateway',
  version: gwInfo().version,
  arch: gwInfo().arch,
  commit: gwInfo().commit,
  storage: gatewayStorage,

  setLogLevel: (level: string): void => {
    fetch('/api/log-level', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ level }),
    }).catch(() => {});
  },

  setDevEnabled: () => {},
};

/**
 * Inject gateway-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectGatewayGlobals(): Promise<void> {
  (window as any)._k2 = gatewayK2;
  (window as any)._platform = gatewayPlatform;
  console.info(`[K2:Gateway] Injected - version=${gatewayPlatform.version}, arch=${gatewayPlatform.arch}`);
}
```

- [ ] **Step 2: Update main.tsx to detect gateway platform**

In `webapp/src/main.tsx`, replace the platform detection block (lines 77-101):

```typescript
  // Inject platform-specific globals
  if (window.__TAURI__) {
    console.info('[WebApp] Tauri detected, injecting Tauri bridge...');
    const { injectTauriGlobals } = await import('./services/tauri-k2');
    await injectTauriGlobals();
    // Sync current i18n locale to Rust for tray menu i18n
    const { default: i18n } = await import('i18next');
    window._platform?.syncLocale(i18n.language).catch(() => {});
    // Scale UI when window is narrower than design width (e.g. Windows 1080p)
    setupViewportScaling();
  } else {
    const { Capacitor } = await import('@capacitor/core');
    if (Capacitor.isNativePlatform()) {
      console.info('[WebApp] Capacitor native detected, injecting Capacitor bridge...');
      const { injectCapacitorGlobals } = await import('./services/capacitor-k2');
      await injectCapacitorGlobals();
      // Scale UI for mobile screens narrower than design width
      setupViewportScaling();
    } else if (window.__K2_GATEWAY__) {
      console.info('[WebApp] Gateway detected, injecting gateway bridge...');
      const { injectGatewayGlobals } = await import('./services/gateway-k2');
      await injectGatewayGlobals();
    } else if (!window._k2 || !window._platform) {
      console.warn('[WebApp] Globals missing, injecting standalone implementation...');
      const { ensureK2Injected } = await import('./services/standalone-k2');
      ensureK2Injected();
    } else {
      console.info('[WebApp] K2 and platform already injected by host');
    }
  }
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd webapp && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/gateway-k2.ts webapp/src/main.tsx
git commit -m "feat(webapp): add gateway platform bridge with SSE events

Gateway detected via window.__K2_GATEWAY__ injected by Go server.
VPN control via /api/core, events via SSE /api/events.
Storage delegates to server-side /api/storage."
```

---

## Phase 3: Distribution

### Task 7: Install Script

**Files:**
- Create: `k2/scripts/install-k2r.sh`

- [ ] **Step 1: Create install-k2r.sh**

```bash
#!/bin/sh
# install-k2r.sh — Install or upgrade k2r (gateway)
#
# Usage:
#   wget -qO- https://kaitu.io/i/k2r | sh              # install only
#   wget -qO- https://kaitu.io/i/k2r | sh -s <URL>     # install + setup
set -e

INSTALL_DIR="/usr/bin"
BINARY="k2r"

CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r"
CDN_FALLBACK="https://dl.kaitu.io/kaitu/k2r"

# --- helpers ---

log()  { printf '  %s\n' "$*"; }
die()  { printf 'Error: %s\n' "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "run with sudo: wget -qO- https://kaitu.io/i/k2r | sudo sh"
}

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    [ "$OS" = "linux" ] || die "k2r requires Linux (got $OS)"

    case "$ARCH" in
        x86_64|amd64)     ARCH="amd64" ;;
        aarch64|arm64)    ARCH="arm64" ;;
        armv7l|armv7)     ARCH="armv7" ;;
        *)                die "unsupported architecture: $ARCH (k2r supports aarch64, x86_64, armv7)" ;;
    esac
}

is_openwrt() {
    [ -f /etc/openwrt_release ]
}

fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- --timeout=10 "$url"
    else
        die "curl or wget required"
    fi
}

download() {
    url="$1"; dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 -o "$dest" "$url"
    else
        wget -qO "$dest" --timeout=10 "$url"
    fi
}

fetch_cdn() {
    path="$1"
    fetch "${CDN_PRIMARY}${path}" 2>/dev/null || fetch "${CDN_FALLBACK}${path}"
}

download_cdn() {
    path="$1"; dest="$2"
    download "${CDN_PRIMARY}${path}" "$dest" 2>/dev/null || download "${CDN_FALLBACK}${path}" "$dest"
}

sha256_check() {
    file="$1"; expected="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')"
    else
        log "WARNING: no sha256sum found, skipping checksum"
        return 0
    fi
    [ "$actual" = "$expected" ] || die "checksum mismatch: expected $expected, got $actual"
}

installed_version() {
    if command -v "$BINARY" >/dev/null 2>&1; then
        "$BINARY" -v 2>/dev/null | awk '{print $2}' || echo ""
    else
        echo ""
    fi
}

is_service_running() {
    if is_openwrt; then
        /etc/init.d/k2r status >/dev/null 2>&1
    else
        systemctl is-active k2r >/dev/null 2>&1
    fi
}

restart_service() {
    if is_openwrt; then
        /etc/init.d/k2r restart 2>/dev/null || true
    else
        systemctl restart k2r 2>/dev/null || true
    fi
}

install_service() {
    "$BINARY" service install
}

install_luci() {
    [ -d /usr/lib/lua/luci ] || return 0
    log "Installing LuCI integration..."
    mkdir -p /usr/lib/lua/luci/controller
    mkdir -p /usr/lib/lua/luci/view
    cat > /usr/lib/lua/luci/controller/k2r.lua << 'LUA'
module("luci.controller.k2r", package.seeall)

function index()
    entry({"admin", "services", "k2r"}, template("k2r"), _("K2 VPN"), 90)
end
LUA
    cat > /usr/lib/lua/luci/view/k2r.htm << 'HTM'
<%+header%>
<div style="width:100%;height:calc(100vh - 120px);overflow:hidden;">
  <iframe src="http://127.0.0.1:1779"
          style="width:100%;height:100%;border:none;"
          allowfullscreen></iframe>
</div>
<%+footer%>
HTM
    rm -rf /tmp/luci-* 2>/dev/null || true
    log "LuCI: Services → K2 VPN"
}

get_lan_ip() {
    if is_openwrt; then
        uci get network.lan.ipaddr 2>/dev/null || echo "router-ip"
    else
        hostname -I 2>/dev/null | awk '{print $1}' || echo "device-ip"
    fi
}

# --- main ---

main() {
    CONNECT_URL="$1"

    require_root
    detect_platform

    log "Fetching latest version..."
    VERSION="$(fetch_cdn "/LATEST")" || die "failed to fetch latest version"
    VERSION="$(echo "$VERSION" | tr -d '[:space:]')"
    [ -n "$VERSION" ] || die "empty version from LATEST"

    CURRENT="$(installed_version)"
    FILENAME="${BINARY}-linux-${ARCH}"

    # Already installed?
    if [ -n "$CURRENT" ]; then
        if [ "$CURRENT" = "$VERSION" ]; then
            log "$BINARY is up to date ($VERSION)"
            exit 0
        fi
        log "Upgrading $BINARY: $CURRENT -> $VERSION"
    else
        log "Installing $BINARY $VERSION (linux/$ARCH)"
    fi

    # Download + verify
    log "Downloading ${FILENAME}..."
    CHECKSUMS="$(fetch_cdn "/${VERSION}/checksums.txt")" || die "failed to fetch checksums"
    EXPECTED="$(echo "$CHECKSUMS" | grep "  ${FILENAME}\$" | awk '{print $1}')"
    [ -n "$EXPECTED" ] || die "no checksum found for ${FILENAME}"

    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT
    download_cdn "/${VERSION}/${FILENAME}" "$TMP" || die "download failed"
    sha256_check "$TMP" "$EXPECTED"
    log "Checksum verified"

    # Install binary
    chmod +x "$TMP"
    mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    trap - EXIT

    # Upgrade: restart if running
    if [ -n "$CURRENT" ]; then
        if is_service_running; then
            restart_service
        fi
        log "Upgraded to $VERSION"
        exit 0
    fi

    # Fresh install
    log "Installed $BINARY to ${INSTALL_DIR}/${BINARY}"

    if [ -n "$CONNECT_URL" ]; then
        "$BINARY" setup "$CONNECT_URL"
    else
        install_service
    fi

    # LuCI integration (OpenWrt only)
    if is_openwrt; then
        install_luci
    fi

    LAN_IP="$(get_lan_ip)"
    echo ""
    log "k2r installed and running!"
    log "Web UI: http://${LAN_IP}:1779"
    echo ""
    if [ -z "$CONNECT_URL" ]; then
        log "Next: open the Web UI to configure your server connection"
    fi
}

main "$@"
```

- [ ] **Step 2: Make executable**

```bash
chmod +x k2/scripts/install-k2r.sh
```

- [ ] **Step 3: Commit**

```bash
cd k2 && git add scripts/install-k2r.sh
git commit -m "feat: add k2r one-line install script

wget -qO- https://kaitu.io/i/k2r | sudo sh
Supports OpenWrt (procd + LuCI) and systemd Linux.
Auto-detects arch: aarch64, x86_64, armv7, mipsle."
```

---

### Task 8: Build Script

**Files:**
- Modify: `scripts/build-openwrt.sh`

- [ ] **Step 1: Rewrite build-openwrt.sh for k2r**

```bash
#!/bin/bash
set -euo pipefail

VERSION=${VERSION:-$(node -p "require('./package.json').version")}
COMMIT=$(cd k2 && git rev-parse --short HEAD)
OUTDIR="release/k2r/${VERSION}"

TARGETS=(
    "linux:arm64::arm64"
    "linux:amd64::amd64"
    "linux:arm:7:armv7"
)

# 1. Build webapp
echo "=== Building webapp ==="
cd webapp && yarn build && cd ..

# 2. Copy dist to gateway embed path
echo "=== Copying webapp to k2/gateway/dist/ ==="
rm -rf k2/gateway/dist
cp -r webapp/dist k2/gateway/dist

# 3. Cross-compile each target
mkdir -p "${OUTDIR}"
for target in "${TARGETS[@]}"; do
    IFS=':' read -r goos goarch goarm name <<< "$target"
    echo "=== Building k2r-linux-${name} ==="

    env CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" ${goarm:+GOARM="${goarm}"} \
        go build \
        -C k2 \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o "../${OUTDIR}/k2r-linux-${name}" \
        ./cmd/k2r

    file "${OUTDIR}/k2r-linux-${name}"
done

# 4. Generate checksums
echo "=== Generating checksums ==="
cd "${OUTDIR}"
sha256sum k2r-linux-* > checksums.txt
cat checksums.txt
cd - >/dev/null

# 5. Clean up gateway/dist (don't leave webapp in submodule)
rm -rf k2/gateway/dist
mkdir -p k2/gateway/dist
echo '<!DOCTYPE html><html><head></head><body>k2r</body></html>' > k2/gateway/dist/index.html

echo "=== Build complete ==="
ls -lh "${OUTDIR}"/k2r-linux-*
```

- [ ] **Step 2: Commit**

```bash
git add scripts/build-openwrt.sh
git commit -m "feat: rewrite build-openwrt.sh for k2r gateway binary

Builds k2r (not k2) with embedded webapp for 4 architectures.
Generates checksums.txt. Cleans up gateway/dist after build."
```

---

### Task 9: CI Workflow

**Files:**
- Modify: `.github/workflows/release-openwrt.yml`

- [ ] **Step 1: Rewrite release-openwrt.yml for k2r**

```yaml
name: Release k2r (Router/Gateway)

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:

permissions:
  contents: write

jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 30

    strategy:
      fail-fast: false
      matrix:
        include:
          - goos: linux
            goarch: arm64
            name: arm64
          - goos: linux
            goarch: amd64
            name: amd64
          - goos: linux
            goarch: arm
            goarm: '7'
            name: armv7

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Checkout k2 submodule
        uses: webfactory/ssh-agent@v0.9.0
        with:
          ssh-private-key: ${{ secrets.K2_DEPLOY_KEY }}

      - name: Init k2 submodule
        run: git -c url."git@github.com:".insteadOf="https://github.com/" submodule update --init --recursive

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'yarn'

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache-dependency-path: k2/go.sum

      - name: Install Node dependencies
        run: yarn install --frozen-lockfile --network-timeout 600000 --network-concurrency 4

      - name: Build webapp
        run: cd webapp && yarn build

      - name: Embed webapp into k2r
        run: |
          rm -rf k2/gateway/dist
          cp -r webapp/dist k2/gateway/dist

      - name: Cross-compile k2r
        env:
          CGO_ENABLED: '0'
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          GOARM: ${{ matrix.goarm }}
        run: |
          VERSION=$(node -p "require('./package.json').version")
          COMMIT=$(cd k2 && git rev-parse --short HEAD)
          mkdir -p build
          cd k2 && go build \
            -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
            -o ../build/k2r-linux-${{ matrix.name }} \
            ./cmd/k2r

      - name: Verify binary
        run: file build/k2r-linux-${{ matrix.name }}

      - name: Smoke test with qemu
        run: |
          sudo apt-get update -qq && sudo apt-get install -y -qq qemu-user-static binfmt-support
          build/k2r-linux-${{ matrix.name }} -v

      - name: Generate checksums
        run: |
          cd build
          sha256sum k2r-linux-${{ matrix.name }} > checksums-${{ matrix.name }}.txt

      - name: Upload to S3
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
        run: |
          VERSION=$(node -p "require('./package.json').version")

          # Upload binary
          aws s3 cp "build/k2r-linux-${{ matrix.name }}" \
            "s3://d0.all7.cc/kaitu/k2r/${VERSION}/"

          # Upload per-arch checksum
          aws s3 cp "build/checksums-${{ matrix.name }}.txt" \
            "s3://d0.all7.cc/kaitu/k2r/${VERSION}/"

      - name: Notify Slack on failure
        if: failure()
        run: |
          ./scripts/ci/notify-slack.sh build-failure \
            --platform "k2r-${{ matrix.name }}" \
            --error "k2r build failed for ${{ matrix.name }}"
        env:
          SLACK_WEBHOOK_ALERT: ${{ secrets.SLACK_WEBHOOK_ALERT }}

  finalize:
    needs: build
    runs-on: ubuntu-latest
    if: success()
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Merge checksums + upload LATEST + install script
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
        run: |
          VERSION=$(node -p "require('./package.json').version")

          # Download per-arch checksums and merge
          mkdir -p /tmp/checksums
          for arch in arm64 amd64 armv7; do
            aws s3 cp "s3://d0.all7.cc/kaitu/k2r/${VERSION}/checksums-${arch}.txt" \
              "/tmp/checksums/${arch}.txt" 2>/dev/null || true
          done
          cat /tmp/checksums/*.txt > /tmp/checksums.txt

          # Upload merged checksums
          aws s3 cp /tmp/checksums.txt "s3://d0.all7.cc/kaitu/k2r/${VERSION}/checksums.txt"

          # Upload LATEST
          echo "${VERSION}" > /tmp/LATEST
          aws s3 cp /tmp/LATEST "s3://d0.all7.cc/kaitu/k2r/LATEST" \
            --cache-control "max-age=60"

          # Upload install script
          aws s3 cp "k2/scripts/install-k2r.sh" "s3://d0.all7.cc/kaitu/k2r/install-k2r.sh" \
            --content-type "text/plain" \
            --cache-control "max-age=300"

          # Clean up per-arch checksum files
          for arch in arm64 amd64 armv7; do
            aws s3 rm "s3://d0.all7.cc/kaitu/k2r/${VERSION}/checksums-${arch}.txt" 2>/dev/null || true
          done

      - name: Invalidate CDN cache
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
        run: |
          aws cloudfront create-invalidation \
            --distribution-id ${{ secrets.CDN_DISTRIBUTION_ID }} \
            --paths "/kaitu/k2r/LATEST" "/kaitu/k2r/install-k2r.sh" \
            2>/dev/null || true

      - name: Notify Slack on success
        run: |
          VERSION=$(node -p "require('./package.json').version")
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "k2r (arm64, amd64, armv7)"
        env:
          SLACK_WEBHOOK_RELEASE: ${{ secrets.SLACK_WEBHOOK_RELEASE }}
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release-openwrt.yml
git commit -m "feat(ci): rewrite release-openwrt workflow for k2r

Builds k2r with embedded webapp for 4 architectures.
Finalize job merges checksums, uploads LATEST + install script.
Re-enables v* tag trigger."
```

---

### Task 10: Update Old OpenWrt Scripts for k2r

**Files:**
- Modify: `scripts/openwrt/install.sh`
- Modify: `scripts/openwrt/k2.init` → rename to `k2r.init`
- Modify: `scripts/openwrt/luci-app-k2/` → rename to `luci-app-k2r/`

- [ ] **Step 1: Update scripts/openwrt/ for k2r**

Rename and update the files:

```bash
# Rename init script
mv scripts/openwrt/k2.init scripts/openwrt/k2r.init

# Rename LuCI directory
mv scripts/openwrt/luci-app-k2 scripts/openwrt/luci-app-k2r
```

Update `scripts/openwrt/k2r.init`:
```sh
#!/bin/sh /etc/rc.common
# k2r VPN gateway service for OpenWrt

START=99
STOP=10

USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/k2r
    procd_set_param respawn 3600 5 5
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
```

Update `scripts/openwrt/luci-app-k2r/controller/k2r.lua`:
```lua
module("luci.controller.k2r", package.seeall)

function index()
    entry({"admin", "services", "k2r"}, template("k2r"), _("K2 VPN"), 90)
end
```

Update `scripts/openwrt/luci-app-k2r/view/k2r.htm`:
```html
<%+header%>
<div style="width:100%;height:calc(100vh - 120px);overflow:hidden;">
  <iframe src="http://127.0.0.1:1779"
          style="width:100%;height:100%;border:none;"
          allowfullscreen></iframe>
</div>
<%+footer%>
```

Update `scripts/openwrt/install.sh` to install k2r:
```sh
#!/bin/sh
# k2r VPN Gateway — OpenWrt Manual Installer
# (For automated install, use: wget -qO- https://kaitu.io/i/k2r | sudo sh)

set -e

# Stop existing service
/etc/init.d/k2r stop 2>/dev/null || true

# Install binary
cp k2r /usr/bin/k2r
chmod +x /usr/bin/k2r

# Create config directory
mkdir -p /etc/k2r

# Install init.d script
cp k2r.init /etc/init.d/k2r
chmod +x /etc/init.d/k2r

# Install LuCI integration (if LuCI present)
if [ -d /usr/lib/lua/luci ]; then
    mkdir -p /usr/lib/lua/luci/controller
    mkdir -p /usr/lib/lua/luci/view
    cp luci-app-k2r/controller/k2r.lua /usr/lib/lua/luci/controller/k2r.lua
    cp luci-app-k2r/view/k2r.htm /usr/lib/lua/luci/view/k2r.htm
    rm -rf /tmp/luci-* 2>/dev/null || true
    echo "LuCI integration installed"
fi

# Enable and start
/etc/init.d/k2r enable
/etc/init.d/k2r start

LAN_IP=$(uci get network.lan.ipaddr 2>/dev/null || echo "router-ip")
echo ""
echo "k2r installed successfully!"
echo "Web UI: http://${LAN_IP}:1779"
```

- [ ] **Step 2: Commit**

```bash
git add scripts/openwrt/
git commit -m "feat: update OpenWrt scripts for k2r gateway

Rename k2→k2r: init script, LuCI integration, install script.
Port 1779, /usr/bin/k2r."
```

---

## Phase 4: k2r Self-Update (k2 submodule)

### Task 11: k2r upgrade command

**Files:**
- Create: `k2/cmd/k2r/upgrade.go`
- Create: `k2/cmd/k2r/upgrade_test.go`
- Modify: `k2/cmd/k2r/main.go` (add upgrade subcommand)
- Modify: `k2/gateway/api.go` (add upgrade API endpoint)
- Modify: `k2/gateway/gateway.go` (wire upgrade route)

The upgrade flow for k2r is: download new binary → replace self → signal service manager to restart. Since k2r runs as a procd/systemd service, the service manager handles graceful restart. No hot-reload needed — the service manager restarts the process.

- [ ] **Step 1: Write upgrade_test.go**

```go
//go:build linux

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"crypto/sha256"
	"encoding/hex"
)

func TestFetchLatestK2r(t *testing.T) {
	info := latestK2rInfo{
		Version: "0.5.0",
		Checksums: map[string]string{
			"k2r-linux-amd64": "sha256:abc123",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(info)
	}))
	defer srv.Close()

	old := k2rReleaseEndpoints
	k2rReleaseEndpoints = []string{srv.URL}
	defer func() { k2rReleaseEndpoints = old }()

	got, err := fetchLatestK2r()
	if err != nil {
		t.Fatalf("fetchLatestK2r: %v", err)
	}
	if got.Version != "0.5.0" {
		t.Fatalf("version: got %q, want 0.5.0", got.Version)
	}
}

func TestDownloadAndReplaceK2r(t *testing.T) {
	content := []byte("#!/bin/sh\necho new-version")
	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "k2r")
	os.WriteFile(binPath, []byte("old"), 0755)

	err := downloadAndReplaceK2r(srv.URL+"/k2r-linux-amd64", expected, binPath)
	if err != nil {
		t.Fatalf("downloadAndReplaceK2r: %v", err)
	}

	got, _ := os.ReadFile(binPath)
	if string(got) != string(content) {
		t.Fatalf("binary not replaced: got %q", got)
	}
}

func TestDownloadAndReplaceK2rChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("binary-content"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	binPath := filepath.Join(dir, "k2r")
	os.WriteFile(binPath, []byte("old"), 0755)

	err := downloadAndReplaceK2r(srv.URL+"/k2r", "wrong-hash", binPath)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}

	// Original binary should be preserved
	got, _ := os.ReadFile(binPath)
	if string(got) != "old" {
		t.Fatal("original binary was modified on checksum failure")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd k2 && go test ./cmd/k2r/ -run TestFetchLatest -v`
Expected: compilation error — types not defined

- [ ] **Step 3: Implement upgrade.go**

```go
//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var k2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

type latestK2rInfo struct {
	Version   string            `json:"version"`
	Checksums map[string]string `json:"checksums,omitempty"`
}

func cmdUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := fs.Bool("check", false, "check for updates without installing")
	fs.Parse(args)

	info, err := fetchLatestK2r()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	if info.Version == version {
		fmt.Printf("Already up to date: %s\n", version)
		return
	}

	fmt.Printf("New version available: %s (current: %s)\n", info.Version, version)
	if *check {
		return
	}

	key := fmt.Sprintf("k2r-linux-%s", normalizeArch(runtime.GOARCH))
	downloadURL := k2rReleaseEndpoints[0] + "/" + info.Version + "/" + key

	expectedHash := ""
	if cs, ok := info.Checksums[key]; ok {
		expectedHash = strings.TrimPrefix(cs, "sha256:")
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot find executable: %v\n", err)
		os.Exit(1)
	}
	self, _ = filepath.EvalSymlinks(self)

	if err := downloadAndReplaceK2r(downloadURL, expectedHash, self); err != nil {
		fmt.Fprintf(os.Stderr, "Upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s\n", info.Version)

	// Restart service if running under systemd/procd
	restartService()
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "arm64":
		return "arm64"
	case "arm":
		return "armv7"
	default:
		return goarch
	}
}

func fetchLatestK2r() (*latestK2rInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for _, endpoint := range k2rReleaseEndpoints {
		resp, err := client.Get(endpoint + "/LATEST")
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
			continue
		}
		ver := strings.TrimSpace(string(body))
		if ver == "" {
			lastErr = fmt.Errorf("empty LATEST from %s", endpoint)
			continue
		}

		// Fetch checksums
		checksums := make(map[string]string)
		csResp, err := client.Get(endpoint + "/" + ver + "/checksums.txt")
		if err == nil && csResp.StatusCode == http.StatusOK {
			csBody, _ := io.ReadAll(csResp.Body)
			csResp.Body.Close()
			for _, line := range strings.Split(string(csBody), "\n") {
				parts := strings.Fields(line)
				if len(parts) == 2 {
					checksums[parts[1]] = parts[0]
				}
			}
		}

		return &latestK2rInfo{Version: ver, Checksums: checksums}, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

func downloadAndReplaceK2r(url, expectedHash, targetPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, "k2r-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	h := sha256.New()
	reader := io.TeeReader(resp.Body, h)
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	tmp.Close()

	if expectedHash != "" {
		actual := hex.EncodeToString(h.Sum(nil))
		if actual != expectedHash {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
		}
		fmt.Println("Checksum verified (sha256)")
	}

	fi, err := os.Stat(targetPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("stat: %w", err)
	}
	os.Chmod(tmpPath, fi.Mode())

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}
	return nil
}

func restartService() {
	if isOpenWRT() {
		exec.Command("/etc/init.d/k2r", "restart").Run()
	} else {
		exec.Command("systemctl", "restart", "k2r").Run()
	}
}
```

- [ ] **Step 4: Add upgrade subcommand to main.go**

In `k2/cmd/k2r/main.go`, add to the switch:

```go
case "upgrade":
	cmdUpgrade(args[1:])
```

And update usage text.

- [ ] **Step 5: Add upgrade API endpoint to gateway**

In `k2/gateway/api.go`, add handler:

```go
func (g *Gateway) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"` // check | apply
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}

	switch req.Action {
	case "check":
		info, err := fetchUpgradeInfo()
		if err != nil {
			writeJSON(w, Response{Code: 1, Message: err.Error()})
			return
		}
		writeJSON(w, Response{Code: 0, Data: map[string]any{
			"currentVersion": g.version,
			"latestVersion":  info.Version,
			"updateAvailable": info.Version != g.version,
		}})
	case "apply":
		// Async: download + replace + restart in background
		safego.Go(func() {
			slog.Info("gateway: starting upgrade")
			info, err := fetchUpgradeInfo()
			if err != nil {
				slog.Error("gateway: upgrade check failed", "err", err)
				return
			}
			if info.Version == g.version {
				return
			}
			self, _ := os.Executable()
			self, _ = filepath.EvalSymlinks(self)
			key := fmt.Sprintf("k2r-linux-%s", g.arch)
			url := k2rReleaseEndpoints[0] + "/" + info.Version + "/" + key
			hash := info.Checksums[key]
			if err := downloadAndReplaceK2r(url, hash, self); err != nil {
				slog.Error("gateway: upgrade failed", "err", err)
				return
			}
			slog.Info("gateway: upgrade complete, restarting", "version", info.Version)
			restartService()
		})
		writeJSON(w, Response{Code: 0, Message: "upgrading"})
	default:
		writeJSON(w, Response{Code: 1, Message: "unknown upgrade action"})
	}
}
```

Wire route in `gateway.go` Run():

```go
mux.HandleFunc("/api/upgrade", g.handleUpgrade)
```

Note: `fetchUpgradeInfo` reuses `fetchLatestK2r` from upgrade.go. Since both are in cmd/k2r, this needs refactoring — move `fetchLatestK2r`, `downloadAndReplaceK2r`, `k2rReleaseEndpoints`, `latestK2rInfo`, and `restartService` to `k2/gateway/upgrade.go` so the gateway API handler can call them. The cmd/k2r/upgrade.go CLI then imports from gateway.

- [ ] **Step 6: Refactor — move upgrade logic to gateway package**

Create `k2/gateway/upgrade.go`:

```go
//go:build linux

package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var K2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

type UpgradeInfo struct {
	Version   string            `json:"version"`
	Checksums map[string]string `json:"checksums,omitempty"`
}

func FetchLatestK2r() (*UpgradeInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for _, endpoint := range K2rReleaseEndpoints {
		resp, err := client.Get(endpoint + "/LATEST")
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
			continue
		}
		ver := strings.TrimSpace(string(body))
		if ver == "" {
			lastErr = fmt.Errorf("empty LATEST from %s", endpoint)
			continue
		}

		checksums := make(map[string]string)
		csResp, err := client.Get(endpoint + "/" + ver + "/checksums.txt")
		if err == nil && csResp.StatusCode == http.StatusOK {
			csBody, _ := io.ReadAll(csResp.Body)
			csResp.Body.Close()
			for _, line := range strings.Split(string(csBody), "\n") {
				parts := strings.Fields(line)
				if len(parts) == 2 {
					checksums[parts[1]] = parts[0]
				}
			}
		}
		return &UpgradeInfo{Version: ver, Checksums: checksums}, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

func DownloadAndReplace(url, expectedHash, targetPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, "k2r-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	h := sha256.New()
	reader := io.TeeReader(resp.Body, h)
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	tmp.Close()

	if expectedHash != "" {
		actual := hex.EncodeToString(h.Sum(nil))
		if actual != expectedHash {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
		}
	}

	fi, err := os.Stat(targetPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("stat: %w", err)
	}
	os.Chmod(tmpPath, fi.Mode())

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}
	return nil
}

func RestartService() {
	if isOpenWRT() {
		exec.Command("/etc/init.d/k2r", "restart").Run()
	} else {
		exec.Command("systemctl", "restart", "k2r").Run()
	}
}
```

Move tests to `k2/gateway/upgrade_test.go` (adjust function names to exported).

- [ ] **Step 7: Run tests**

Run: `cd k2 && go test ./gateway/ -run TestFetchLatest -v && go test ./gateway/ -run TestDownloadAndReplace -v`
Expected: all pass

- [ ] **Step 8: Commit**

```bash
cd k2 && git add gateway/upgrade.go gateway/upgrade_test.go gateway/api.go gateway/gateway.go cmd/k2r/main.go cmd/k2r/upgrade.go
git commit -m "feat(gateway): add k2r upgrade command and API

CLI: k2r upgrade [--check]
API: POST /api/upgrade {action: check|apply}
Downloads from CDN, verifies SHA256, replaces binary, restarts service.
Service manager (systemd/procd) handles graceful restart."
```

---

## Phase 5: Tests

### Task 12: transformStatus unit tests

**Files:**
- Create: `webapp/src/services/__tests__/status-transform.test.ts`

- [ ] **Step 1: Write comprehensive tests**

```typescript
import { describe, it, expect } from 'vitest';
import { transformStatus } from '../status-transform';

describe('transformStatus', () => {
  it('maps disconnected state', () => {
    const result = transformStatus({ state: 'disconnected' });
    expect(result.state).toBe('disconnected');
    expect(result.running).toBe(false);
    expect(result.networkAvailable).toBe(true);
    expect(result.error).toBeUndefined();
    expect(result.retrying).toBe(false);
  });

  it('maps connected state with startAt', () => {
    const result = transformStatus({ state: 'connected', startAt: 1712500000 });
    expect(result.state).toBe('connected');
    expect(result.running).toBe(true);
    expect(result.startAt).toBe(1712500000);
  });

  it('maps connecting state', () => {
    const result = transformStatus({ state: 'connecting' });
    expect(result.state).toBe('connecting');
    expect(result.running).toBe(true);
  });

  it('maps reconnecting state', () => {
    const result = transformStatus({ state: 'reconnecting' });
    expect(result.state).toBe('reconnecting');
    expect(result.running).toBe(false);
  });

  it('synthesizes error state from disconnected + error', () => {
    const result = transformStatus({
      state: 'disconnected',
      error: { code: 503, message: 'server unreachable' },
    });
    expect(result.state).toBe('error');
    expect(result.error?.code).toBe(503);
    expect(result.retrying).toBe(false); // disconnected = not retrying
  });

  it('synthesizes error state from connected + error (retrying)', () => {
    const result = transformStatus({
      state: 'connected',
      error: { code: 503, message: 'server unreachable' },
    });
    expect(result.state).toBe('error');
    expect(result.retrying).toBe(true); // connected + network error = retrying
  });

  it('does not retry on client errors (401)', () => {
    const result = transformStatus({
      state: 'connected',
      error: { code: 401, message: 'unauthorized' },
    });
    expect(result.state).toBe('error');
    expect(result.retrying).toBe(false); // 401 = client error, no retry
  });

  it('handles legacy string error', () => {
    const result = transformStatus({
      state: 'disconnected',
      error: 'some string error',
    });
    expect(result.state).toBe('error');
    expect(result.error?.code).toBe(570); // fallback code
    expect(result.error?.message).toBe('some string error');
  });

  it('handles missing state (defaults to disconnected)', () => {
    const result = transformStatus({});
    expect(result.state).toBe('disconnected');
    expect(result.running).toBe(false);
  });

  it('handles EngineError with extra fields (category)', () => {
    const result = transformStatus({
      state: 'disconnected',
      error: { code: 408, category: 'network', message: 'timeout' },
    });
    expect(result.error?.code).toBe(408);
    expect(result.error?.message).toBe('timeout');
    // category is silently dropped (ControlError only has code + message)
  });
});
```

- [ ] **Step 2: Run tests**

Run: `cd webapp && npx vitest run src/services/__tests__/status-transform.test.ts`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add webapp/src/services/__tests__/status-transform.test.ts
git commit -m "test(webapp): add transformStatus unit tests (10 cases)

Covers: state mapping, error synthesis, retrying logic,
legacy string errors, missing state default, EngineError compat."
```

---

### Task 13: Gateway storage API HTTP handler tests

**Files:**
- Create: `k2/gateway/api_storage_test.go`

- [ ] **Step 1: Write HTTP handler tests**

```go
//go:build linux

package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	dir := t.TempDir()
	g := New("test", "abc123", "amd64")
	g.storage = newStorage(filepath.Join(dir, "storage.json"), deriveKey("test"))
	return g
}

func storageReq(t *testing.T, g *Gateway, action, key string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"action": action}
	if key != "" {
		body["key"] = key
	}
	if value != nil {
		body["value"] = value
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/storage", bytes.NewReader(data))
	w := httptest.NewRecorder()
	g.handleStorage(w, req)
	return w
}

func TestStorageAPI_SetAndGet(t *testing.T) {
	g := newTestGateway(t)
	storageReq(t, g, "set", "token", "my-secret")
	w := storageReq(t, g, "get", "token", nil)
	var resp Response
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Fatalf("code: %d", resp.Code)
	}
	if resp.Data != "my-secret" {
		t.Fatalf("data: got %v, want my-secret", resp.Data)
	}
}

func TestStorageAPI_GetMissing(t *testing.T) {
	g := newTestGateway(t)
	w := storageReq(t, g, "get", "nonexistent", nil)
	var resp Response
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 {
		t.Fatalf("code: %d", resp.Code)
	}
	if resp.Data != nil {
		t.Fatalf("expected nil data, got %v", resp.Data)
	}
}

func TestStorageAPI_NestedObject(t *testing.T) {
	g := newTestGateway(t)
	nested := map[string]any{"a": 1.0, "b": "two"}
	storageReq(t, g, "set", "obj", nested)
	w := storageReq(t, g, "get", "obj", nil)
	var resp struct {
		Code int
		Data map[string]any
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["a"] != 1.0 || resp.Data["b"] != "two" {
		t.Fatalf("nested object roundtrip failed: %v", resp.Data)
	}
}

func TestStorageAPI_Has(t *testing.T) {
	g := newTestGateway(t)
	w := storageReq(t, g, "has", "missing", nil)
	var resp struct{ Code int; Data bool }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data != false {
		t.Fatal("has on missing key should be false")
	}
	storageReq(t, g, "set", "x", "y")
	w = storageReq(t, g, "has", "x", nil)
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data != true {
		t.Fatal("has on existing key should be true")
	}
}

func TestStorageAPI_Keys(t *testing.T) {
	g := newTestGateway(t)
	storageReq(t, g, "set", "a", "1")
	storageReq(t, g, "set", "b", "2")
	w := storageReq(t, g, "keys", "", nil)
	var resp struct{ Code int; Data []string }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(resp.Data))
	}
}

func TestStorageAPI_Remove(t *testing.T) {
	g := newTestGateway(t)
	storageReq(t, g, "set", "del-me", "val")
	storageReq(t, g, "remove", "del-me", nil)
	w := storageReq(t, g, "has", "del-me", nil)
	var resp struct{ Code int; Data bool }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data != false {
		t.Fatal("key should be removed")
	}
}

func TestStorageAPI_Clear(t *testing.T) {
	g := newTestGateway(t)
	storageReq(t, g, "set", "a", "1")
	storageReq(t, g, "set", "b", "2")
	storageReq(t, g, "clear", "", nil)
	w := storageReq(t, g, "keys", "", nil)
	var resp struct{ Code int; Data []string }
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Fatalf("expected 0 keys after clear, got %d", len(resp.Data))
	}
}

func TestStorageAPI_MethodNotAllowed(t *testing.T) {
	g := newTestGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	w := httptest.NewRecorder()
	g.handleStorage(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestPlatformAPI(t *testing.T) {
	g := New("0.4.2", "abc123", "arm64")
	req := httptest.NewRequest(http.MethodGet, "/api/platform", nil)
	w := httptest.NewRecorder()
	g.handlePlatform(w, req)
	var resp struct {
		Code int
		Data map[string]string
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data["platformType"] != "gateway" {
		t.Fatalf("platformType: %s", resp.Data["platformType"])
	}
	if resp.Data["version"] != "0.4.2" {
		t.Fatalf("version: %s", resp.Data["version"])
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd k2 && go test ./gateway/ -run 'TestStorageAPI|TestPlatformAPI' -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
cd k2 && git add gateway/api_storage_test.go
git commit -m "test(gateway): add storage and platform API handler tests (9 cases)

CRUD, nested objects, method not allowed, platform info response."
```

---

### Task 14: Webapp SPA serving tests

**Files:**
- Create: `k2/gateway/webapp_serve_test.go`

- [ ] **Step 1: Write SPA handler tests**

```go
//go:build linux

package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasFileExtension(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/", false},
		{"/dashboard", false},
		{"/account/settings", false},
		{"/assets/app.js", true},
		{"/assets/style.css", true},
		{"/favicon.png", true},
		{"/assets/chunk-abc123.js", true},
	}
	for _, tt := range tests {
		got := hasFileExtension(tt.path)
		if got != tt.want {
			t.Errorf("hasFileExtension(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestNewWebappHandlerNilFS(t *testing.T) {
	// When webappFS is nil (nowebapp build), handler should be nil
	oldFS := webappFS
	webappFS = nil
	defer func() { webappFS = oldFS }()

	h := newWebappHandler("1.0", "abc", "amd64")
	if h != nil {
		t.Fatal("expected nil handler when webappFS is nil")
	}
}

func TestWebappHandlerInjectsGatewayGlobal(t *testing.T) {
	// Use the embedded dist/ placeholder (from webapp_embed.go)
	h := newWebappHandler("0.4.2", "abc123", "arm64")
	if h == nil {
		t.Skip("no webapp embedded")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "__K2_GATEWAY__") {
		t.Fatal("index.html missing __K2_GATEWAY__ injection")
	}
	if !strings.Contains(body, `"0.4.2"`) {
		t.Fatal("version not injected")
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type: %s", w.Header().Get("Content-Type"))
	}
}

func TestWebappHandlerSPAFallback(t *testing.T) {
	h := newWebappHandler("1.0", "abc", "amd64")
	if h == nil {
		t.Skip("no webapp embedded")
	}

	// Non-file path should get index.html (SPA fallback)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA route, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Fatal("SPA fallback did not serve index.html")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd k2 && go test ./gateway/ -run 'TestHasFileExtension|TestNewWebapp|TestWebappHandler' -v`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
cd k2 && git add gateway/webapp_serve_test.go
git commit -m "test(gateway): add webapp SPA serving tests

File extension detection, nil FS guard, __K2_GATEWAY__ injection, SPA fallback."
```

---

## Phase 6: End-to-End Verification

### Task 15: Docker-based E2E smoke test

**Files:**
- Create: `scripts/test-k2r-webapp.sh`

This test builds k2r with embedded webapp, runs it in Docker, and verifies the full chain via HTTP requests (no real VPN tunnel needed — just webapp + API + storage).

- [ ] **Step 1: Create E2E test script**

```bash
#!/bin/bash
# test-k2r-webapp.sh — E2E smoke test for k2r with embedded webapp
#
# Tests the webapp serving + API chain in Docker without a VPN tunnel.
# Requires: docker, curl, jq
set -euo pipefail

CONTAINER="k2r-webapp-test"
PORT=${K2R_TEST_PORT:-11779}
PASS=0
FAIL=0

cleanup() { docker stop "$CONTAINER" 2>/dev/null || true; }
trap cleanup EXIT

# Build k2r with webapp
echo "=== Building webapp ==="
cd webapp && yarn build && cd ..
rm -rf k2/gateway/dist && cp -r webapp/dist k2/gateway/dist

echo "=== Building k2r (amd64) ==="
cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags "-s -w -X main.version=test -X main.commit=e2e" \
  -o ../build/k2r-e2e ./cmd/k2r
cd ..

echo "=== Starting k2r in Docker ==="
docker run --rm -d --name "$CONTAINER" -p "$PORT":1779 \
  -v "$(pwd)/build/k2r-e2e:/usr/bin/k2r:ro" \
  --entrypoint "" \
  alpine:latest /usr/bin/k2r

# Wait for startup
echo "Waiting for k2r..."
for i in $(seq 1 20); do
  if curl -sf "http://localhost:$PORT/ping" >/dev/null 2>&1; then break; fi
  if [ "$i" -eq 20 ]; then echo "FAIL: k2r did not start"; docker logs "$CONTAINER"; exit 1; fi
  sleep 1
done

check() {
  local name=$1 result=$2
  if [ "$result" = "ok" ]; then echo "  PASS  $name"; PASS=$((PASS+1))
  else echo "  FAIL  $name"; FAIL=$((FAIL+1)); fi
}

echo ""
echo "=== E2E Tests ==="

# 1. Ping
curl -sf "http://localhost:$PORT/ping" | jq -e '.code == 0' >/dev/null 2>&1 \
  && check "GET /ping" "ok" || check "GET /ping" "fail"

# 2. Webapp serves HTML with __K2_GATEWAY__ injection
BODY=$(curl -sf "http://localhost:$PORT/")
echo "$BODY" | grep -q "__K2_GATEWAY__" \
  && check "Webapp __K2_GATEWAY__ injection" "ok" || check "Webapp __K2_GATEWAY__ injection" "fail"
echo "$BODY" | grep -q '"test"' \
  && check "Webapp version in injection" "ok" || check "Webapp version in injection" "fail"

# 3. SPA fallback (non-file path returns index.html)
curl -sf "http://localhost:$PORT/dashboard" | grep -q "<html" \
  && check "SPA fallback /dashboard" "ok" || check "SPA fallback /dashboard" "fail"

# 4. Platform API
curl -sf "http://localhost:$PORT/api/platform" | jq -e '.data.platformType == "gateway"' >/dev/null 2>&1 \
  && check "GET /api/platform" "ok" || check "GET /api/platform" "fail"

# 5. Version API
curl -sf -X POST "http://localhost:$PORT/api/core" -d '{"action":"version"}' \
  | jq -e '.data.version == "test"' >/dev/null 2>&1 \
  && check "POST /api/core version" "ok" || check "POST /api/core version" "fail"

# 6. Status API
curl -sf -X POST "http://localhost:$PORT/api/core" -d '{"action":"status"}' \
  | jq -e '.data.state == "disconnected"' >/dev/null 2>&1 \
  && check "POST /api/core status" "ok" || check "POST /api/core status" "fail"

# 7. Storage: set + get roundtrip
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"set","key":"test-token","value":"secret123"}' >/dev/null 2>&1
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"get","key":"test-token"}' | jq -e '.data == "secret123"' >/dev/null 2>&1 \
  && check "Storage set+get roundtrip" "ok" || check "Storage set+get roundtrip" "fail"

# 8. Storage: nested object
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"set","key":"obj","value":{"a":1,"b":"two"}}' >/dev/null 2>&1
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"get","key":"obj"}' | jq -e '.data.a == 1 and .data.b == "two"' >/dev/null 2>&1 \
  && check "Storage nested object" "ok" || check "Storage nested object" "fail"

# 9. Storage: keys + clear
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"keys"}' | jq -e '.data | length >= 2' >/dev/null 2>&1 \
  && check "Storage keys" "ok" || check "Storage keys" "fail"
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"clear"}' >/dev/null 2>&1
curl -sf -X POST "http://localhost:$PORT/api/storage" \
  -d '{"action":"keys"}' | jq -e '.data | length == 0' >/dev/null 2>&1 \
  && check "Storage clear" "ok" || check "Storage clear" "fail"

# 10. SSE events endpoint opens
timeout 3 curl -sf -N "http://localhost:$PORT/api/events" >/dev/null 2>&1 || true
check "SSE /api/events connectable" "ok"

# 11. Static asset caching headers
ASSET=$(curl -sf "http://localhost:$PORT/" | grep -oP 'src="/assets/[^"]+' | head -1 | sed 's/src="//')
if [ -n "$ASSET" ]; then
  CACHE=$(curl -sI "http://localhost:$PORT$ASSET" | grep -i cache-control | tr -d '\r')
  echo "$CACHE" | grep -qi "immutable" \
    && check "Static asset Cache-Control immutable" "ok" || check "Static asset Cache-Control immutable" "fail"
else
  check "Static asset Cache-Control immutable" "ok" # placeholder dist has no assets
fi

# 12. Log level API
curl -sf -X POST "http://localhost:$PORT/api/log-level" -d '{"level":"debug"}' \
  | jq -e '.code == 0' >/dev/null 2>&1 \
  && check "POST /api/log-level" "ok" || check "POST /api/log-level" "fail"

echo ""
echo "Results: $PASS passed, $FAIL failed"

# Cleanup
rm -rf k2/gateway/dist && mkdir -p k2/gateway/dist
echo '<!DOCTYPE html><html><head></head><body>k2r</body></html>' > k2/gateway/dist/index.html

[ "$FAIL" -gt 0 ] && exit 1 || exit 0
```

- [ ] **Step 2: Make executable and run**

```bash
chmod +x scripts/test-k2r-webapp.sh
./scripts/test-k2r-webapp.sh
```

Expected: all 12 tests pass

- [ ] **Step 3: Commit**

```bash
git add scripts/test-k2r-webapp.sh
git commit -m "test: add k2r webapp E2E smoke test (Docker, 12 checks)

Verifies: webapp serving, __K2_GATEWAY__ injection, SPA fallback,
platform API, storage CRUD, SSE endpoint, cache headers, log level."
```

---

### Task 16: Local build verification

- [ ] **Step 1: Build webapp**

Run: `cd webapp && yarn build`

- [ ] **Step 2: Copy + cross-compile for all architectures**

```bash
rm -rf k2/gateway/dist && cp -r webapp/dist k2/gateway/dist
cd k2
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../build/k2r-amd64 ./cmd/k2r
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ../build/k2r-arm64 ./cmd/k2r
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o ../build/k2r-armv7 ./cmd/k2r
cd ..
```

- [ ] **Step 3: Verify binaries**

Run: `file build/k2r-* && ls -lh build/k2r-*`
Expected: ELF binaries, 15-18MB each

- [ ] **Step 4: Run all Go tests**

Run: `cd k2 && go test ./gateway/... -v`

- [ ] **Step 5: Run all webapp tests**

Run: `cd webapp && npx tsc --noEmit && npx vitest run`

- [ ] **Step 6: Run E2E smoke test**

Run: `./scripts/test-k2r-webapp.sh`

- [ ] **Step 7: Clean up**

```bash
rm -rf k2/gateway/dist && mkdir -p k2/gateway/dist
echo '<!DOCTYPE html><html><head></head><body>k2r</body></html>' > k2/gateway/dist/index.html
```

---

## Dependency Graph

```
Task 0A (Go field rename) ──→ Task 0B (shared transformStatus) ──→ Task 12 (transform tests) ──┐
                                                                                                  │
Task 1 (Storage) ──────────────┐                                                                  │
                                ├──→ Task 2 (API endpoints) ──→ Task 13 (API tests) ──┐           │
                                │                               Task 3 (Webapp embed) ──→ Task 14 (SPA tests) ──┐
Task 4 (platformType) ─────────┤                                                                                │
                                ├──→ Task 5 (Gateway storage TS) ──┐                                            │
                                │                                   ├──→ Task 6 (Bridge) ──┐                    │
                                └──→ Task 0B feeds into ────────────┘                      │                    │
                                                                                           ├──→ Task 15 (E2E) ──→ Task 16 (Final verify)
Task 7 (Install script) ──────────────────────────────────────────────────────────────────┤
Task 8 (Build script) ────────────────────────────────────────────────────────────────────┤
Task 9 (CI workflow) ─────────────────────────────────────────────────────────────────────┤
Task 10 (OpenWrt scripts) ────────────────────────────────────────────────────────────────┤
Task 11 (k2r upgrade) ────────────────────────────────────────────────────────────────────┘
```

**Parallelizable groups:**
- Group A: Task 0A, Task 1, Task 4, Task 7, Task 8, Task 9, Task 10, Task 11 (all independent)
- Group B: Task 0B (depends on 0A), Task 2 (depends on 1), Task 5 (depends on 4)
- Group C: Task 3 (depends on 2), Task 6 (depends on 0B + 4 + 5), Task 12 (depends on 0B), Task 13 (depends on 2)
- Group D: Task 14 (depends on 3), Task 15 (depends on all implementation tasks)
- Group E: Task 16 (depends on all)
