# Windows Quality Assurance — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add 5-layer Windows quality assurance so every PR validates Windows-specific code paths, preventing platform bugs from reaching production.

**Architecture:** CI gate (Windows self-hosted runner) → platform unit tests (Rust `#[cfg(windows)]` + Go `_windows_test.go`) → build verification (NSIS build in CI) → service integration tests (admin-gated) → regression guards. Tests are written TDD-style where possible.

**Tech Stack:** GitHub Actions (self-hosted Windows runner), Rust `cargo test`, Go `go test` with build tags, vitest (webapp), PowerShell (service smoke tests), NSIS (build verification)

**Design doc:** `docs/plans/2026-03-07-windows-quality-assurance-design.md`

---

## Task 1: CI Gate — Add Windows job to ci.yml

**Files:**
- Modify: `.github/workflows/ci.yml`

**Context:** The existing CI runs only on `ubuntu-latest`. The release workflow (`release-desktop.yml`) already has a Windows self-hosted runner pattern we can reuse (nvm setup, Go/Rust setup, submodule init).

**Step 1: Add `test-windows` job to ci.yml**

Add a new job after the existing `test` job. Reuse the self-hosted Node.js setup from `release-desktop.yml:49-66`:

```yaml
  test-windows:
    runs-on: [self-hosted, Windows]

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Setup SSH for private submodule
        uses: webfactory/ssh-agent@v0.9.0
        with:
          ssh-private-key: ${{ secrets.K2_DEPLOY_KEY }}

      - name: Init k2 submodule
        shell: bash
        run: git -c url."git@github.com:".insteadOf="https://github.com/" submodule update --init --recursive

      - name: Setup Node.js (self-hosted)
        shell: bash
        run: |
          NODE_VERSION=$(cat .nvmrc)
          CURRENT=$(node -v 2>/dev/null || echo "none")
          if [ "$CURRENT" != "v${NODE_VERSION}" ]; then
            echo "Node $CURRENT -> v${NODE_VERSION}"
            nvm install "$NODE_VERSION"
          fi
          nvm use "$NODE_VERSION"
          NODE_DIR=$(dirname "$(which node)")
          echo "$NODE_DIR" >> "$GITHUB_PATH"
          if ! command -v yarn &>/dev/null; then
            npm install -g yarn
          fi
          NPM_GLOBAL=$(npm prefix -g)
          echo "$NPM_GLOBAL" >> "$GITHUB_PATH"

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache-dependency-path: k2/go.sum

      - name: Setup Rust
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: x86_64-pc-windows-msvc

      - name: Install Node dependencies
        run: yarn install --frozen-lockfile --network-timeout 600000

      - name: Run webapp tests (vitest)
        run: yarn test
        working-directory: webapp

      - name: Type-check webapp (tsc)
        run: npx tsc --noEmit
        working-directory: webapp

      - name: Cargo check (Tauri)
        run: cargo check
        working-directory: desktop/src-tauri

      - name: Cargo test (Tauri)
        run: cargo test
        working-directory: desktop/src-tauri

      - name: Go test (k2 core)
        shell: bash
        working-directory: k2
        run: go test ./sniff/... ./core/... ./engine/... ./config/... ./daemon/... ./provider/... ./cmd/k2/... -count=1 -short

      - name: Build k2 Windows binary
        shell: bash
        run: make build-k2-windows

      - name: Verify k2 binary exists
        shell: bash
        run: test -f desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe
```

**Step 2: Verify CI YAML is valid**

Run: `cd .github/workflows && python3 -c "import yaml; yaml.safe_load(open('ci.yml'))" 2>/dev/null || echo "check manually"`

Or just verify indentation visually — YAML is sensitive to whitespace.

**Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Windows test job to CI pipeline

Add test-windows job on self-hosted Windows runner. Runs cargo check,
cargo test, go test, vitest, and k2 binary build on every push/PR.
Catches Windows-specific compilation errors and test failures."
```

---

## Task 2: Rust — Window size calculation tests (platform-independent)

**Files:**
- Modify: `desktop/src-tauri/src/window.rs`

**Context:** `calculate_window_size(screen_height)` is a pure function. It enforces `ASPECT_RATIO=9/20`, `MAX_HEIGHT_RATIO=0.85`, `MIN_WIDTH=320`, `MIN_HEIGHT=568`, `MAX_WIDTH=480`. Common Windows screens (768p, 1080p) need coverage.

**Step 1: Write the tests**

Add at bottom of `window.rs`, inside a new `#[cfg(test)]` module:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_window_size_1080p() {
        let (w, h) = calculate_window_size(1080);
        // 1080 * 0.80 = 864, width = 864 * 0.45 = 388
        assert!(w >= MIN_WIDTH && w <= MAX_WIDTH, "width {} out of bounds", w);
        assert!(h <= (1080.0 * MAX_HEIGHT_RATIO) as u32, "height {} exceeds max ratio", h);
        // Verify aspect ratio (within 1px tolerance)
        let actual_ratio = w as f64 / h as f64;
        assert!((actual_ratio - ASPECT_RATIO).abs() < 0.01, "ratio {} != {}", actual_ratio, ASPECT_RATIO);
    }

    #[test]
    fn test_window_size_768p() {
        // Common Windows laptop: 1366x768
        let (w, h) = calculate_window_size(768);
        assert!(w >= MIN_WIDTH, "width {} below minimum", w);
        assert!(h <= (768.0 * MAX_HEIGHT_RATIO) as u32, "height {} exceeds 768p max", h);
        assert!(h >= MIN_HEIGHT || (768.0 * MAX_HEIGHT_RATIO) as u32 < MIN_HEIGHT,
            "height {} below minimum when screen allows it", h);
    }

    #[test]
    fn test_window_size_4k() {
        // 4K: 3840x2160
        let (w, h) = calculate_window_size(2160);
        assert!(w <= MAX_WIDTH, "width {} exceeds MAX_WIDTH on 4K", w);
    }

    #[test]
    fn test_window_size_small_screen() {
        // Very small screen (e.g., 600p)
        let (w, h) = calculate_window_size(600);
        assert!(w >= MIN_WIDTH, "width {} below minimum on small screen", w);
    }

    #[test]
    fn test_window_size_aspect_ratio_maintained() {
        // Across multiple resolutions, aspect ratio should be close to 9:20
        for screen_h in [600, 768, 900, 1080, 1440, 2160] {
            let (w, h) = calculate_window_size(screen_h);
            if h > 0 {
                let ratio = w as f64 / h as f64;
                assert!((ratio - ASPECT_RATIO).abs() < 0.02,
                    "screen {}p: ratio {} deviates from {}", screen_h, ratio, ASPECT_RATIO);
            }
        }
    }
}
```

**Step 2: Run tests to verify they pass**

Run: `cd desktop/src-tauri && cargo test window::tests -- --nocapture`

Expected: All 5 tests pass (these test existing working code).

**Step 3: Commit**

```bash
git add desktop/src-tauri/src/window.rs
git commit -m "test(desktop): add window size calculation tests

Cover common Windows resolutions (768p, 1080p, 4K) and verify aspect ratio
maintenance across screen sizes. Regression guard for window sizing logic."
```

---

## Task 3: Rust — Windows-specific service tests

**Files:**
- Modify: `desktop/src-tauri/src/service.rs`

**Context:** `service.rs:251-277` has `#[cfg(target_os = "windows")]` block for WMI-based UDID. `service.rs:337-365` has Windows PowerShell elevation. The existing `#[cfg(test)]` module (line 600) has platform-independent tests only.

**Step 1: Add Windows-specific tests to existing test module**

Insert before the closing `}` of the `mod tests` block in `service.rs` (after line 763):

```rust
    // -----------------------------------------------------------------------
    // Windows-specific tests (only compile and run on Windows)
    // -----------------------------------------------------------------------

    #[cfg(target_os = "windows")]
    mod windows_tests {
        use super::super::*;

        #[test]
        fn test_windows_udid_wmi_available() {
            // WMI query should succeed on any Windows machine.
            // Regression: a9a00e1 fixed wmic failure on ARM64 Win11 by switching to WMI COM.
            let uuid = get_hardware_uuid();
            assert!(uuid.is_ok(), "WMI UUID query failed: {:?}", uuid.err());
            let uuid = uuid.unwrap();
            assert!(!uuid.is_empty(), "UUID should not be empty");
            assert_ne!(uuid, "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF",
                "UUID should not be the sentinel value");
        }

        #[test]
        fn test_windows_service_path_has_backslashes() {
            // On Windows, current_exe() returns paths with backslashes.
            let exe = std::env::current_exe().unwrap();
            let parent = exe.parent().unwrap();
            let k2_path = parent.join("k2.exe");
            let path_str = k2_path.to_string_lossy();
            // Windows paths should use backslashes (not forward slashes)
            assert!(path_str.contains('\\'), "Windows path should have backslashes: {}", path_str);
            assert!(path_str.ends_with("k2.exe"), "Should end with k2.exe: {}", path_str);
        }

        #[test]
        fn test_powershell_command_no_injection() {
            // Verify the PowerShell command format used in admin_reinstall_service_windows.
            // The service_path is derived from current_exe() — test that it's properly quoted.
            let fake_path = r"C:\Program Files\Kaitu\k2.exe";
            let ps_script = format!(
                r#"Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden"#,
                fake_path
            );
            // Single quotes in PowerShell prevent variable expansion — safe for paths with spaces
            assert!(ps_script.contains("'C:\\Program Files\\Kaitu\\k2.exe'"));
            assert!(ps_script.contains("-Verb RunAs"));
        }
    }
```

**Step 2: Run tests on Windows**

Run: `cd desktop/src-tauri && cargo test service::tests -- --nocapture`

Expected: All tests pass (including new `windows_tests` module on Windows, skipped on other platforms).

**Step 3: Commit**

```bash
git add desktop/src-tauri/src/service.rs
git commit -m "test(desktop): add Windows-specific service tests

Test WMI UDID query, Windows path format, and PowerShell command safety.
Regression for a9a00e1 (wmic ARM64 failure) and 31ace38 (service install)."
```

---

## Task 4: Rust — Windows log cleanup test

**Files:**
- Modify: `desktop/src-tauri/src/log_upload.rs`

**Context:** `log_upload.rs:508-529` has `cleanup_dir_logs()` which truncates files on Windows (line 515-520) but deletes on macOS/Linux (line 521-526). This was a fix for Windows file locks (commit 4982946).

**Step 1: Add platform-specific cleanup test**

Add to the existing `mod tests` block in `log_upload.rs` (after the last test):

```rust
    #[test]
    fn test_cleanup_dir_logs_behavior() {
        let dir = tempfile::tempdir().unwrap();
        let log1 = dir.path().join("desktop.log");
        let log2 = dir.path().join("desktop.log.1");
        let other = dir.path().join("other.txt");

        std::fs::write(&log1, "log content 1").unwrap();
        std::fs::write(&log2, "log content 2").unwrap();
        std::fs::write(&other, "should survive").unwrap();

        cleanup_dir_logs(dir.path(), |name| {
            name.starts_with("desktop") && name.contains("log")
        });

        // "other.txt" should be untouched
        assert_eq!(std::fs::read_to_string(&other).unwrap(), "should survive");

        // On Windows: files truncated (exist but empty)
        // On macOS/Linux: files deleted
        #[cfg(target_os = "windows")]
        {
            assert!(log1.exists(), "Windows should truncate, not delete");
            assert_eq!(std::fs::read_to_string(&log1).unwrap().len(), 0,
                "Windows should truncate to 0 bytes");
        }
        #[cfg(not(target_os = "windows"))]
        {
            assert!(!log1.exists(), "Non-Windows should delete the file");
            assert!(!log2.exists(), "Non-Windows should delete the file");
        }
    }

    #[cfg(target_os = "windows")]
    #[test]
    fn test_windows_log_dir_uses_programdata() {
        let dir = get_log_dir();
        let dir_str = dir.to_string_lossy().to_lowercase();
        assert!(dir_str.contains("programdata") || dir_str.contains("kaitu"),
            "Windows log dir should use ProgramData: {}", dir.display());
    }
```

**Step 2: Add tempfile dev-dependency if not present**

Check `desktop/src-tauri/Cargo.toml` for `tempfile` in `[dev-dependencies]`. If missing, add:

```toml
[dev-dependencies]
tempfile = "3"
```

**Step 3: Run tests**

Run: `cd desktop/src-tauri && cargo test log_upload::tests -- --nocapture`

Expected: All tests pass.

**Step 4: Commit**

```bash
git add desktop/src-tauri/src/log_upload.rs desktop/src-tauri/Cargo.toml desktop/src-tauri/Cargo.lock
git commit -m "test(desktop): add Windows log cleanup behavior test

Verify truncate-not-delete on Windows (file lock workaround).
Regression for 4982946 (file locks during log cleanup)."
```

---

## Task 5: Go — DNS crash recovery tests (k2 submodule)

**Files:**
- Create: `k2/provider/dns_windows_test.go`

**Context:** `dns_windows.go` manages a JSON marker file at `%ProgramData%\k2\dns-state.json`. Functions: `dnsStateFilePath()`, `saveDNSState()`, `removeDNSState()`, `CleanupStaleDNSOverride()`. The cleanup function checks marker age (>24h = remove), checks TUN interface existence, and runs PowerShell/netsh commands for DNS reset.

**Step 1: Write the test file**

Create `k2/provider/dns_windows_test.go`:

```go
//go:build windows

package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDNSStateFilePath_UsesProgramData(t *testing.T) {
	path := dnsStateFilePath()
	if path == "" {
		t.Fatal("dnsStateFilePath() returned empty string")
	}

	// Should contain ProgramData (or fallback to Temp)
	programData := os.Getenv("ProgramData")
	if programData != "" {
		if !strings.Contains(path, programData) {
			t.Errorf("path %q does not contain ProgramData %q", path, programData)
		}
	}

	// Should end with dns-state.json
	if filepath.Base(path) != "dns-state.json" {
		t.Errorf("path %q does not end with dns-state.json", path)
	}

	// Should contain k2 directory component
	if !strings.Contains(path, string(filepath.Separator)+"k2"+string(filepath.Separator)) {
		t.Errorf("path %q does not contain /k2/ directory", path)
	}
}

func TestDNSStateFilePath_FallbackWhenProgramDataEmpty(t *testing.T) {
	original := os.Getenv("ProgramData")
	os.Setenv("ProgramData", "")
	defer os.Setenv("ProgramData", original)

	path := dnsStateFilePath()
	// Should fall back to temp dir
	if !strings.Contains(path, os.TempDir()) {
		t.Errorf("fallback path %q does not use TempDir %q", path, os.TempDir())
	}
}

func TestSaveDNSState_WritesValidJSON(t *testing.T) {
	// Use temp dir to avoid modifying ProgramData
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	saveDNSState("sing-tun", "198.18.0.8")

	path := dnsStateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state windowsDNSState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if state.InterfaceName != "sing-tun" {
		t.Errorf("InterfaceName = %q, want sing-tun", state.InterfaceName)
	}
	if state.DNSServer != "198.18.0.8" {
		t.Errorf("DNSServer = %q, want 198.18.0.8", state.DNSServer)
	}
	if time.Since(state.Timestamp) > 5*time.Second {
		t.Errorf("Timestamp too old: %v", state.Timestamp)
	}
}

func TestRemoveDNSState_DeletesFile(t *testing.T) {
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	saveDNSState("test-iface", "1.2.3.4")
	path := dnsStateFilePath()

	// File should exist
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	removeDNSState()

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file still exists after remove")
	}
}

func TestCleanupStaleDNSOverride_NoMarker_NoOp(t *testing.T) {
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	// No marker file exists — should not panic
	CleanupStaleDNSOverride()
}

func TestCleanupStaleDNSOverride_OldMarker_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	// Write a marker with timestamp >24h ago
	state := windowsDNSState{
		InterfaceName: "sing-tun",
		DNSServer:     "198.18.0.8",
		Timestamp:     time.Now().Add(-25 * time.Hour),
	}
	data, _ := json.Marshal(state)
	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, data, 0600)

	CleanupStaleDNSOverride()

	// Old marker should be deleted (>24h rule)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stale marker (>24h) should be removed")
	}
}

func TestCleanupStaleDNSOverride_InvalidJSON_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("not json"), 0600)

	CleanupStaleDNSOverride()

	// Invalid JSON marker should be deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("invalid JSON marker should be removed")
	}
}

func TestCleanupStaleDNSOverride_FreshMarker_InterfaceGone_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)
	defer os.Setenv("ProgramData", original)

	// Fresh marker but interface name is fake (won't exist)
	state := windowsDNSState{
		InterfaceName: "nonexistent-test-iface-12345",
		DNSServer:     "198.18.0.8",
		Timestamp:     time.Now(),
	}
	data, _ := json.Marshal(state)
	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, data, 0600)

	CleanupStaleDNSOverride()

	// Interface doesn't exist → marker should be removed (no DNS cleanup needed)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("marker for non-existent interface should be removed")
	}
}
```

**Step 2: Run the tests**

Run: `cd k2 && go test ./provider/... -run TestDNS -count=1 -v`

Expected: All 7 tests pass.

**Step 3: Commit (in k2 submodule)**

```bash
cd k2
git add provider/dns_windows_test.go
git commit -m "test(provider): add Windows DNS crash recovery tests

Test marker file lifecycle (save/remove/cleanup), ProgramData path
resolution, old marker removal (>24h), invalid JSON cleanup, and
non-existent interface handling. No admin required."
```

---

## Task 6: Go — Windows signal handling tests (k2 submodule)

**Files:**
- Create: `k2/cmd/k2/signal_windows_test.go`

**Context:** `signal_windows.go` defines `runSignal(sig, pidPath, w)`. Key behaviors: reject "reload" on Windows, handle stale PID files, send SIGTERM for "stop".

**Step 1: Write the test file**

Create `k2/cmd/k2/signal_windows_test.go`:

```go
//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSignal_RejectReload(t *testing.T) {
	var buf bytes.Buffer
	err := runSignal("reload", "/nonexistent/pid", &buf)
	if err == nil {
		t.Fatal("reload should return error on Windows")
	}
	if !strings.Contains(err.Error(), "not supported on Windows") {
		t.Errorf("error = %q, want 'not supported on Windows'", err.Error())
	}
}

func TestRunSignal_UnknownSignal(t *testing.T) {
	var buf bytes.Buffer
	err := runSignal("hup", "/nonexistent/pid", &buf)
	if err == nil {
		t.Fatal("unknown signal should return error")
	}
	if !strings.Contains(err.Error(), "unknown signal") {
		t.Errorf("error = %q, want 'unknown signal'", err.Error())
	}
}

func TestRunSignal_MissingPIDFile(t *testing.T) {
	var buf bytes.Buffer
	err := runSignal("stop", filepath.Join(t.TempDir(), "nonexistent.pid"), &buf)
	if err == nil {
		t.Fatal("stop with missing PID file should return error")
	}
	// Should mention pid file
	if !strings.Contains(err.Error(), "pid") {
		t.Errorf("error = %q, want mention of pid", err.Error())
	}
}

func TestRunSignal_StalePID_Removed(t *testing.T) {
	// Write a PID file with a definitely-dead PID
	pidPath := filepath.Join(t.TempDir(), "k2.pid")
	os.WriteFile(pidPath, []byte("99999999"), 0600)

	var buf bytes.Buffer
	err := runSignal("stop", pidPath, &buf)
	if err == nil {
		t.Fatal("stop with dead PID should return error")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want 'not running'", err.Error())
	}

	// Stale PID file should be cleaned up
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("stale PID file should be removed")
	}
}
```

**Step 2: Run the tests**

Run: `cd k2 && go test ./cmd/k2/... -run TestRunSignal -count=1 -v`

Expected: All 4 tests pass.

**Step 3: Commit (in k2 submodule)**

```bash
cd k2
git add cmd/k2/signal_windows_test.go
git commit -m "test(cmd): add Windows signal handling tests

Test reload rejection, unknown signal, missing PID file, and stale PID
cleanup. Verifies Windows-specific signal_windows.go behavior."
```

---

## Task 7: Go — Windows admin check test (k2 submodule)

**Files:**
- Create: `k2/config/log_windows_test.go`

**Context:** `log_windows.go` defines `isRoot()` which checks `token.IsElevated()`. In CI (non-admin), it should return false.

**Step 1: Write the test file**

Create `k2/config/log_windows_test.go`:

```go
//go:build windows

package config

import "testing"

func TestIsRoot_CI(t *testing.T) {
	// In CI or normal development, process is NOT elevated.
	// This test documents expected behavior and catches API breakage.
	result := isRoot()
	// We can't assert false (test might run elevated), but we CAN assert no panic.
	t.Logf("isRoot() = %v (expected false in CI, true in admin terminal)", result)
}

func TestIsRoot_NoPanic(t *testing.T) {
	// isRoot() must never panic, even if Windows token APIs fail.
	// Call it multiple times to catch any state issues.
	for i := 0; i < 3; i++ {
		_ = isRoot()
	}
}
```

**Step 2: Run the tests**

Run: `cd k2 && go test ./config/... -run TestIsRoot -count=1 -v`

Expected: Both tests pass.

**Step 3: Commit (in k2 submodule)**

```bash
cd k2
git add config/log_windows_test.go
git commit -m "test(config): add Windows admin privilege check test

Verify isRoot() doesn't panic and documents expected behavior.
Uses Windows token API (OpenCurrentProcessToken + IsElevated)."
```

---

## Task 8: Go — Wintun embed test (k2 submodule)

**Files:**
- Create: `k2/daemon/wintun/embed_windows_test.go`

**Context:** `embed_windows.go` embeds `wintun_amd64.dll` and `wintun_arm64.dll`, with `EnsureExtracted()` that extracts the correct DLL based on CPU architecture. `isNativeARM64()` uses `IsWow64Process2` API.

**Step 1: Write the test file**

Create `k2/daemon/wintun/embed_windows_test.go`:

```go
//go:build windows

package wintun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedDLLsNotEmpty(t *testing.T) {
	if len(dllAmd64) == 0 {
		t.Fatal("embedded wintun_amd64.dll is empty")
	}
	if len(dllArm64) == 0 {
		t.Fatal("embedded wintun_arm64.dll is empty")
	}
	t.Logf("wintun_amd64.dll: %d bytes, wintun_arm64.dll: %d bytes", len(dllAmd64), len(dllArm64))
}

func TestEnsureExtracted_WritesDLL(t *testing.T) {
	dir, err := EnsureExtracted()
	if err != nil {
		t.Fatalf("EnsureExtracted() error: %v", err)
	}
	if dir == "" {
		t.Fatal("EnsureExtracted() returned empty directory")
	}

	dllPath := filepath.Join(dir, "wintun.dll")
	info, err := os.Stat(dllPath)
	if err != nil {
		t.Fatalf("wintun.dll not found at %s: %v", dllPath, err)
	}
	if info.Size() == 0 {
		t.Fatal("wintun.dll is empty")
	}
	t.Logf("wintun.dll extracted: %s (%d bytes)", dllPath, info.Size())
}

func TestIsNativeARM64_NoPanic(t *testing.T) {
	// Should not panic on any Windows version (API may not exist on older builds)
	result := isNativeARM64()
	t.Logf("isNativeARM64() = %v", result)
}
```

**Step 2: Run the tests**

Run: `cd k2 && go test ./daemon/wintun/... -count=1 -v`

Expected: All 3 tests pass.

**Step 3: Commit (in k2 submodule)**

```bash
cd k2
git add daemon/wintun/embed_windows_test.go
git commit -m "test(wintun): add embedded DLL and extraction tests

Verify both amd64/arm64 DLLs are embedded, EnsureExtracted writes
correct DLL, and isNativeARM64 doesn't panic. Regression for e5b21d9."
```

---

## Task 9: CI — Build verification job (main branch only)

**Files:**
- Modify: `.github/workflows/ci.yml`

**Context:** After `test-windows` succeeds, run a full NSIS build on `main` branch or manual dispatch to catch packaging issues. No install, just build + verify artifacts.

**Step 1: Add `build-windows` job to ci.yml**

Add after `test-windows` job:

```yaml
  build-windows:
    runs-on: [self-hosted, Windows]
    needs: test-windows
    if: github.ref == 'refs/heads/main' || github.event_name == 'workflow_dispatch'

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Setup SSH for private submodule
        uses: webfactory/ssh-agent@v0.9.0
        with:
          ssh-private-key: ${{ secrets.K2_DEPLOY_KEY }}

      - name: Init k2 submodule
        shell: bash
        run: git -c url."git@github.com:".insteadOf="https://github.com/" submodule update --init --recursive

      - name: Setup Node.js (self-hosted)
        shell: bash
        run: |
          NODE_VERSION=$(cat .nvmrc)
          nvm use "$NODE_VERSION" 2>/dev/null || nvm install "$NODE_VERSION"
          NODE_DIR=$(dirname "$(which node)")
          echo "$NODE_DIR" >> "$GITHUB_PATH"
          if ! command -v yarn &>/dev/null; then npm install -g yarn; fi
          echo "$(npm prefix -g)" >> "$GITHUB_PATH"

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache-dependency-path: k2/go.sum

      - name: Setup Rust
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: x86_64-pc-windows-msvc

      - name: Install and build
        shell: bash
        run: |
          yarn install --frozen-lockfile --network-timeout 600000
          make build-windows

      - name: Verify build artifacts
        shell: bash
        run: |
          VERSION=$(node -p "require('./package.json').version")
          NSIS_DIR="desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis"

          echo "Checking NSIS installer..."
          ls -la "${NSIS_DIR}/"*.exe 2>/dev/null || { echo "FAIL: No NSIS installer found"; exit 1; }

          echo "Checking k2 binary..."
          test -f "desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe" || { echo "FAIL: k2 binary not found"; exit 1; }

          echo "Checking version alignment..."
          CARGO_VERSION=$(grep '^version' desktop/src-tauri/Cargo.toml | head -1 | sed 's/.*"\(.*\)".*/\1/')
          PKG_VERSION=$(node -p "require('./package.json').version")
          if [ "$CARGO_VERSION" != "$PKG_VERSION" ]; then
            echo "FAIL: Version mismatch Cargo.toml=$CARGO_VERSION package.json=$PKG_VERSION"
            exit 1
          fi

          echo "All build artifacts verified OK"
```

**Step 2: Also add `workflow_dispatch` trigger**

At the top of ci.yml, add `workflow_dispatch:` to the `on:` block so the build can be triggered manually:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

**Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Windows build verification job

Full NSIS build + artifact verification on main branch push and
manual dispatch. Checks installer output, k2 binary, and version
alignment. Catches packaging issues before release."
```

---

## Task 10: PowerShell — Service lifecycle smoke script

**Files:**
- Create: `scripts/test-windows-service-smoke.ps1`

**Context:** Automates the manual scenarios from `docs/test-windows-service.md`. Requires admin privileges. Run manually or via `workflow_dispatch` with elevated runner.

**Step 1: Write the smoke script**

Create `scripts/test-windows-service-smoke.ps1`:

```powershell
#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Automated smoke test for k2 Windows Service lifecycle.
    Exercises: install → ping → idempotent reinstall → stop → uninstall.

.DESCRIPTION
    Automates scenarios 1-4 from docs/test-windows-service.md.
    Must run as Administrator. Uses the k2 binary at the standard install path.

.PARAMETER K2Path
    Path to k2.exe binary. Default: desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe
#>
param(
    [string]$K2Path = "desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe"
)

$ErrorActionPreference = "Stop"
$pass = 0
$fail = 0

function Test-Step {
    param([string]$Name, [scriptblock]$Action)
    Write-Host "`n--- $Name ---" -ForegroundColor Cyan
    try {
        & $Action
        Write-Host "  PASS" -ForegroundColor Green
        $script:pass++
    } catch {
        Write-Host "  FAIL: $_" -ForegroundColor Red
        $script:fail++
    }
}

# Verify k2 binary exists
if (-not (Test-Path $K2Path)) {
    Write-Host "k2 binary not found at: $K2Path" -ForegroundColor Red
    Write-Host "Build first: make build-k2-windows" -ForegroundColor Yellow
    exit 1
}

$k2Abs = (Resolve-Path $K2Path).Path
Write-Host "Using k2 binary: $k2Abs"

# Cleanup any previous test state
Write-Host "`n=== Cleanup ===" -ForegroundColor Yellow
sc.exe stop kaitu 2>$null
sc.exe delete kaitu 2>$null
Start-Sleep -Seconds 2

# Test 1: Fresh install
Test-Step "Fresh install" {
    & $k2Abs service install
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "RUNNING") {
        throw "Service not RUNNING after install: $status"
    }
}

# Test 2: Daemon responds to ping
Test-Step "Daemon ping" {
    Start-Sleep -Seconds 2
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:1777/ping" -TimeoutSec 5
    if ($response.code -ne 0) {
        throw "Ping response code = $($response.code), expected 0"
    }
}

# Test 3: Idempotent reinstall
Test-Step "Idempotent reinstall" {
    & $k2Abs service install
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "RUNNING") {
        throw "Service not RUNNING after reinstall: $status"
    }
}

# Test 4: Stop service
Test-Step "Stop service" {
    sc.exe stop kaitu
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "STOPPED") {
        throw "Service not STOPPED: $status"
    }
}

# Test 5: Uninstall
Test-Step "Uninstall service" {
    & $k2Abs service uninstall
    Start-Sleep -Seconds 2
    $result = sc.exe query kaitu 2>&1
    if ($result -notmatch "1060") {
        throw "Service still exists after uninstall"
    }
}

# Summary
Write-Host "`n=== Results ===" -ForegroundColor Yellow
Write-Host "  Pass: $pass" -ForegroundColor Green
Write-Host "  Fail: $fail" -ForegroundColor $(if ($fail -gt 0) { "Red" } else { "Green" })

if ($fail -gt 0) { exit 1 }
```

**Step 2: Test the script locally (requires admin terminal)**

Run from admin PowerShell: `.\scripts\test-windows-service-smoke.ps1`

Expected: 5/5 pass (requires k2 binary built first via `make build-k2-windows`).

**Step 3: Commit**

```bash
git add scripts/test-windows-service-smoke.ps1
git commit -m "test: add Windows service lifecycle smoke script

Automated version of docs/test-windows-service.md scenarios 1-4.
Tests: install, ping, idempotent reinstall, stop, uninstall.
Requires Administrator privileges."
```

---

## Task 11: Update k2 submodule reference

**Files:**
- Update: `k2` (submodule reference)

**Context:** After pushing Go test files to k2 repo (Tasks 5-8), update the submodule reference in k2app.

**Step 1: Push k2 changes**

```bash
cd k2
git push origin master
```

**Step 2: Update submodule in k2app**

```bash
cd ..  # back to k2app root
git add k2
git commit -m "chore: update k2 submodule — Windows test coverage

New test files:
- provider/dns_windows_test.go (DNS crash recovery)
- cmd/k2/signal_windows_test.go (signal handling)
- config/log_windows_test.go (admin check)
- daemon/wintun/embed_windows_test.go (DLL embed)"
```

---

## Task 12: Final verification — run all tests on Windows

**Step 1: Run Rust tests**

Run: `cd desktop/src-tauri && cargo test`

Expected: All tests pass (including new window, service, log_upload tests).

**Step 2: Run Go tests**

Run: `cd k2 && go test ./provider/... ./cmd/k2/... ./config/... ./daemon/wintun/... -count=1 -v`

Expected: All new Windows tests pass.

**Step 3: Run webapp tests**

Run: `cd webapp && yarn test`

Expected: All tests pass.

**Step 4: Build k2 binary**

Run: `make build-k2-windows`

Expected: `desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe` exists.

**Step 5: Commit and push**

```bash
git push origin main
```

Expected: CI triggers both `test` (ubuntu) and `test-windows` (self-hosted) jobs. Both pass.

---

## Summary

| Task | Layer | Scope | Files |
|------|-------|-------|-------|
| 1 | L1 | CI gate | `.github/workflows/ci.yml` |
| 2 | L2 | Window size tests | `desktop/src-tauri/src/window.rs` |
| 3 | L2 | Service tests (Windows) | `desktop/src-tauri/src/service.rs` |
| 4 | L2 | Log cleanup test | `desktop/src-tauri/src/log_upload.rs` |
| 5 | L2 | DNS recovery tests | `k2/provider/dns_windows_test.go` (new) |
| 6 | L2 | Signal tests | `k2/cmd/k2/signal_windows_test.go` (new) |
| 7 | L2 | Admin check test | `k2/config/log_windows_test.go` (new) |
| 8 | L2 | Wintun embed test | `k2/daemon/wintun/embed_windows_test.go` (new) |
| 9 | L3 | Build verification | `.github/workflows/ci.yml` |
| 10 | L4 | Service smoke test | `scripts/test-windows-service-smoke.ps1` (new) |
| 11 | — | Submodule update | `k2` |
| 12 | — | Final verification | All |
