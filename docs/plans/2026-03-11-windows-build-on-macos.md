# Windows Build on macOS Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move the entire Windows desktop build + Authenticode signing pipeline from Windows VM to macOS, using cargo-xwin for Rust cross-compilation and osslsigncode + SimplySign PKCS#11 for code signing.

**Architecture:** Cross-compile Tauri (Rust) via `cargo-xwin --target x86_64-pc-windows-msvc`, cross-compile k2 (Go) via `GOOS=windows`, sign with `osslsigncode` using SimplySign Desktop's PKCS#11 module, run on macOS self-hosted GitHub Actions runner (LaunchAgent with GUI session access).

**Tech Stack:** cargo-xwin, osslsigncode, NSIS (brew), SimplySign Desktop PKCS#11, GitHub Actions self-hosted runner

**Branch:** `fix/ci-gates` (rebase onto main first)

**Verification principle:** Every phase ends with a `/using-superpowers` review at 10/10 confidence before proceeding.

---

## Phase 1: Local `make build-windows` on macOS

### Task 1.1: Install cargo-xwin

**Step 1: Install cargo-xwin**

Run:
```bash
cargo install --locked cargo-xwin
```
Expected: Binary installed at `~/.cargo/bin/cargo-xwin`

**Step 2: Verify toolchain**

Run:
```bash
cargo-xwin --version
rustup target list --installed | grep x86_64-pc-windows-msvc
which makensis
which osslsigncode
```
Expected: All four commands succeed. `x86_64-pc-windows-msvc` already installed (confirmed), `makensis` at `/opt/homebrew/bin/makensis` (confirmed), `osslsigncode` at `/opt/homebrew/bin/osslsigncode` (confirmed).

### Task 1.2: Create cross-platform signing wrapper

**Files:**
- Create: `desktop/src-tauri/windows-sign.sh`
- Modify: `desktop/src-tauri/tauri.conf.json:52`

**Step 1: Create `desktop/src-tauri/windows-sign.sh`**

This is the Tauri signCommand entry point. It detects OS and dispatches to the right signer.

```bash
#!/bin/bash
set -e

# Tauri signCommand wrapper — called for every .exe/.dll during Windows bundle.
# macOS: osslsigncode + SimplySign PKCS#11
# Windows: signtool.exe from Windows SDK

if [ "${SKIP_WINDOWS_SIGNING:-false}" = "true" ]; then
    echo "SKIP_WINDOWS_SIGNING=true, skipping: $(basename "$1")"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

case "$(uname -s)" in
    Darwin)
        exec bash "$REPO_ROOT/scripts/ci/macos/windows-sign.sh" "$1"
        ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
        # Windows: use signtool via PowerShell
        exec powershell -NoProfile -File "$REPO_ROOT/scripts/ci/windows/sign-binary.ps1" "$1"
        ;;
    *)
        echo "ERROR: Unsupported OS for Windows signing: $(uname -s)" >&2
        exit 1
        ;;
esac
```

**Step 2: Update tauri.conf.json signCommand**

Change line 52 from:
```json
"signCommand": "powershell -NoProfile -File ../../scripts/ci/windows/sign-binary.ps1 %1"
```
to:
```json
"signCommand": "bash windows-sign.sh %1"
```

Note: Tauri runs signCommand from `src-tauri/` working directory.

**Step 3: chmod +x**

Run:
```bash
chmod +x desktop/src-tauri/windows-sign.sh
```

### Task 1.3: Modify Makefile for macOS cross-compilation

**Files:**
- Modify: `Makefile:53-63`

**Step 1: Replace build-windows target**

Replace lines 53-63 with:

```makefile
build-windows: pre-build build-webapp build-k2-windows
	@# Detect cross-compilation (macOS/Linux building for Windows)
	@if [ "$$(uname -s)" = "Darwin" ] || [ "$$(uname -s)" = "Linux" ]; then \
		echo "--- Cross-compiling Windows from $$(uname -s) via cargo-xwin ---"; \
		command -v cargo-xwin >/dev/null 2>&1 || { echo "ERROR: cargo-xwin not found. Install: cargo install --locked cargo-xwin"; exit 1; }; \
		cd desktop && yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc $(TAURI_FEATURES_ARG); \
	else \
		cd desktop && yarn tauri build --target x86_64-pc-windows-msvc $(TAURI_FEATURES_ARG); \
	fi
	@echo "--- Collecting artifacts ---"
	@mkdir -p release/$(VERSION)
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe release/$(VERSION)/
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe.sig release/$(VERSION)/ 2>/dev/null || true
	@echo "=== Build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/
```

### Task 1.4: Ensure SimpliSign is logged in

**Step 1: Verify SimpliSign PKCS#11 token is available**

Run:
```bash
pkcs11-tool --module /usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib --list-slots
```

Expected: Shows token with label "wordgate". If not, run:
```bash
SIMPLISIGN_TOTP_URI='otpauth://totp/...' bash scripts/ci/macos/simplisign-login.sh
```

### Task 1.5: Run full local build

**Step 1: Build**

Run:
```bash
make build-windows
```

Expected: Full pipeline completes:
1. webapp builds (yarn vite build)
2. k2 cross-compiles (GOOS=windows go build)
3. Tauri cross-compiles via cargo-xwin
4. Each .exe/.dll signed via osslsigncode (windows-sign.sh dispatches to macos/windows-sign.sh)
5. NSIS installer generated
6. Artifacts in `release/{VERSION}/`

**Step 2: Verify signing**

Run:
```bash
VERSION=$(node -p "require('./package.json').version")
osslsigncode verify release/$VERSION/Kaitu_${VERSION}_x64-setup.exe
```

Expected: `Signature verification: ok`

### Task 1.6: Phase 1 Review

Run `/using-superpowers` to review all changes. Confidence must be 10/10 before proceeding.

**Checklist:**
- [ ] `make build-windows` produces signed .exe on macOS
- [ ] `osslsigncode verify` passes on output
- [ ] signCommand wrapper handles both macOS and Windows
- [ ] No regressions to `make build-macos`

**Commit:**
```bash
git add desktop/src-tauri/windows-sign.sh desktop/src-tauri/tauri.conf.json Makefile scripts/ci/macos/windows-sign.sh scripts/ci/macos/simplisign-login.sh
git commit -m "feat: Windows cross-build from macOS via cargo-xwin + osslsigncode"
```

---

## Phase 2: GitHub Actions Runner + CI Workflow

### Task 2.1: Register macOS runner for k2app

The existing macOS runner (`~/actions-runner/`) is registered to `kaitu-io/kaitu`, not `kaitu-io/k2app`. Need a second runner instance.

**Step 1: Create runner directory**

Run:
```bash
mkdir -p ~/actions-runner-k2app
```

**Step 2: Get registration token**

Run:
```bash
gh api -X POST /repos/kaitu-io/k2app/actions/runners/registration-token --jq '.token'
```

**Step 3: Download and configure runner**

Run:
```bash
cd ~/actions-runner-k2app
# Use same runner binary version as existing
RUNNER_VERSION=$(ls -d ~/actions-runner/bin.* | sort -V | tail -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
curl -o actions-runner.tar.gz -L "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
tar xzf actions-runner.tar.gz
rm actions-runner.tar.gz
```

**Step 4: Configure**

Run:
```bash
cd ~/actions-runner-k2app
./config.sh --url https://github.com/kaitu-io/k2app \
  --token <TOKEN_FROM_STEP_2> \
  --name macOS-runner-k2app \
  --labels self-hosted,macOS,ARM64 \
  --work _work
```

**Step 5: Install as LaunchAgent**

Run:
```bash
cd ~/actions-runner-k2app
sudo ./svc.sh install david
```

Verify the plist has `ProcessType: Interactive` and `SessionCreate: true` (same as existing runner):
```bash
cat ~/Library/LaunchAgents/actions.runner.kaitu-io-k2app.macOS-runner-k2app.plist
```

**Step 6: Start runner**

Run:
```bash
sudo ./svc.sh start
```

**Step 7: Verify runner is online**

Run:
```bash
gh api /repos/kaitu-io/k2app/actions/runners --jq '.runners[] | {name, status, labels: [.labels[].name]}'
```

Expected:
```json
{"name":"macOS-runner-k2app","status":"online","labels":["self-hosted","macOS","ARM64"]}
{"name":"Windows-runner-k2app","status":"online","labels":["self-hosted","Windows","X64"]}
```

### Task 2.2: Configure runner environment

The runner needs access to SimpliSign and signing tools. The runner's `.env` and `.path` files control its environment.

**Step 1: Set runner PATH**

Edit `~/actions-runner-k2app/.path` to include:
```
/Users/david/.cargo/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/Users/david/go/bin:/Users/david/.nvm/versions/node/v22.19.0/bin:/usr/bin:/bin:/usr/sbin:/sbin
```

Key paths: `~/.cargo/bin` (cargo-xwin), `/opt/homebrew/bin` (makensis, osslsigncode, brew tools), `/Users/david/go/bin` (go).

**Step 2: Set runner environment variables**

Edit `~/actions-runner-k2app/.env`:
```
LANG=zh_CN.UTF-8
ANDROID_HOME=/Users/david/Library/Android/sdk
```

(SIMPLISIGN_TOTP_URI will be passed via GitHub secrets, not stored in .env)

**Step 3: Restart runner**

Run:
```bash
cd ~/actions-runner-k2app && sudo ./svc.sh stop && sudo ./svc.sh start
```

### Task 2.3: Verify runner GUI access

SimplySign Desktop requires GUI session. Verify the runner can interact with it.

**Step 1: Create a test workflow**

Create `.github/workflows/test-macos-runner.yml`:

```yaml
name: Test macOS Runner
on: workflow_dispatch

jobs:
  test:
    runs-on: [self-hosted, macOS, ARM64]
    timeout-minutes: 5
    steps:
      - name: Check environment
        run: |
          echo "=== OS ==="
          uname -a
          echo "=== Tools ==="
          which cargo-xwin && cargo-xwin --version || echo "cargo-xwin missing"
          which makensis || echo "makensis missing"
          which osslsigncode || echo "osslsigncode missing"
          which go && go version || echo "go missing"
          rustup show active-toolchain
          rustup target list --installed | grep windows
          echo "=== Node ==="
          node -v
          yarn -v
          echo "=== SimpliSign ==="
          pgrep -f "SimplySign Desktop" && echo "SimplySign running" || echo "SimplySign NOT running"
          pkcs11-tool --module /usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib --list-slots 2>&1 | head -5

      - name: Test SimpliSign auto-login
        env:
          SIMPLISIGN_TOTP_URI: ${{ secrets.SIMPLISIGN_TOTP_URI }}
        run: |
          if ! pkcs11-tool --module /usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib --list-slots 2>&1 | grep -q "token label"; then
            echo "PKCS#11 token not available, running auto-login..."
            bash scripts/ci/macos/simplisign-login.sh
          else
            echo "PKCS#11 token already available"
          fi

      - name: Test signing
        run: |
          echo "MZ test binary" > /tmp/test.exe
          osslsigncode sign \
            -pkcs11engine "/opt/homebrew/Cellar/openssl@3/3.6.1/lib/engines-3/pkcs11.dylib" \
            -pkcs11module "/usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib" \
            -pkcs11cert "pkcs11:token=wordgate;object=334AB051AA095E46AF497253EB398C98;type=cert" \
            -key "pkcs11:token=wordgate" \
            -h sha256 -n "Kaitu Desktop" \
            -ts "http://time.certum.pl" \
            -in /tmp/test.exe -out /tmp/test-signed.exe \
            && echo "Signing OK" || echo "Signing FAILED"
          rm -f /tmp/test.exe /tmp/test-signed.exe
```

**Step 2: Push and trigger**

Run:
```bash
git add .github/workflows/test-macos-runner.yml
git commit -m "ci: add macOS runner test workflow"
git push origin fix/ci-gates
gh workflow run test-macos-runner.yml --ref fix/ci-gates
```

**Step 3: Watch results**

Run:
```bash
gh run list --workflow=test-macos-runner.yml -L 1
# Then:
gh run watch <RUN_ID>
```

Expected: All checks green — tools available, SimpliSign login succeeds, signing succeeds.

**Step 4: Also verify locally that runner is running and reachable**

Run:
```bash
# Check runner process
launchctl list | grep actions.runner.kaitu-io-k2app
# Check runner logs
tail -20 ~/Library/Logs/actions.runner.kaitu-io-k2app.macOS-runner-k2app/stdout.log
```

### Task 2.4: Modify release-desktop.yml for macOS Windows builds

**Files:**
- Modify: `.github/workflows/release-desktop.yml`

**Step 1: Change Windows matrix runner**

Replace line 134:
```yaml
          - runner: [self-hosted, Windows]
            target: x86_64-pc-windows-msvc
            platform: Windows
```
with:
```yaml
          - runner: [self-hosted, macOS, ARM64]
            target: x86_64-pc-windows-msvc
            platform: Windows
```

**Step 2: Replace Windows-specific setup steps**

Replace the "Setup Node.js (self-hosted)" step (lines 165-183) — the Windows `nvm` logic doesn't apply to macOS. Instead, the macOS runner should use its system Node (already in `.path`).

Replace:
```yaml
      - name: Setup Node.js (self-hosted)
        if: matrix.platform == 'Windows'
        shell: bash
        run: |
          NODE_VERSION=$(cat .nvmrc)
          ...
```
with:
```yaml
      - name: Setup Node.js (self-hosted macOS)
        if: matrix.platform == 'Windows'
        shell: bash
        run: |
          echo "Using system Node: $(node -v)"
          echo "Using system Yarn: $(yarn -v)"
```

**Step 3: Replace Windows cache/sccache steps**

Remove the "Set persistent cache paths (Windows)" step (lines 213-220) — was setting `C:/cache/` paths.

Remove the "Setup sccache (Windows local disk)" step (lines 222-228) — was installing sccache via cargo on Windows.

**Step 4: Add SimpliSign login step before build**

Add before the "Build Windows" step (before line 299):

```yaml
      - name: SimpliSign auto-login (Windows cross-build)
        if: matrix.platform == 'Windows'
        env:
          SIMPLISIGN_TOTP_URI: ${{ secrets.SIMPLISIGN_TOTP_URI }}
        run: |
          if ! pkcs11-tool --module /usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib --list-slots 2>&1 | grep -q "token label"; then
            echo "PKCS#11 token not available, running auto-login..."
            bash scripts/ci/macos/simplisign-login.sh
          else
            echo "PKCS#11 token already available"
          fi
```

**Step 5: Update Build Windows step**

The existing `make build-windows` will now use cargo-xwin automatically (from Task 1.3).
No change needed to the build step itself.

**Step 6: Update Windows S3 upload**

The artifact path changes because cross-compilation output goes to the same target directory. Verify the NSIS path is correct:
```
desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/
```

This should be the same regardless of whether built natively or via cargo-xwin. No change needed.

### Task 2.5: Add SIMPLISIGN_TOTP_URI to GitHub secrets

**Step 1: Set secret**

Run:
```bash
gh secret set SIMPLISIGN_TOTP_URI --repo kaitu-io/k2app
```

Paste the TOTP URI when prompted (from `scripts/ci/windows/set_simplisign_env.ps1`).

### Task 2.6: Test CI via workflow dispatch

**Step 1: Commit and push**

```bash
git add .github/workflows/release-desktop.yml
git commit -m "ci: Windows build on macOS self-hosted runner"
git push origin fix/ci-gates
```

**Step 2: Trigger release workflow**

```bash
gh workflow run "Release Desktop" --ref fix/ci-gates
```

**Step 3: Watch and verify**

```bash
gh run list --workflow=release-desktop.yml -L 1
gh run watch <RUN_ID>
```

Expected: Both macOS and Windows builds succeed. Windows build runs on `macOS-runner-k2app`.

### Task 2.7: Phase 2 Review

Run `/using-superpowers` to review all changes. Confidence must be 10/10 before proceeding.

**Checklist:**
- [ ] macOS runner registered and online for k2app (`gh api .../runners`)
- [ ] Runner LaunchAgent has `ProcessType: Interactive` + `SessionCreate: true`
- [ ] Test workflow passes (tools, SimpliSign, signing)
- [ ] release-desktop.yml Windows job runs on macOS runner
- [ ] Full release build produces signed .exe uploaded to S3

**Commit:**
```bash
git add -A
git commit -m "ci: complete macOS-based Windows build pipeline"
```

---

## Phase 3: Cleanup + Final Commit

### Task 3.1: Remove Windows-only build infrastructure

Only after CI is confirmed green.

**Files to remove:**
- `scripts/build-windows-test.sh` — Remote VM build delegation (no longer needed)
- `scripts/ci/windows/sign-binary.ps1` — PowerShell signing (replaced by windows-sign.sh wrapper)
- `scripts/ci/windows/sign.ps1` — Alternative PowerShell signing
- `scripts/ci/windows/simplisign_login.ps1` — Windows SimpliSign login (replaced by macOS version)
- `scripts/ci/windows/simplisign_keeper_ctl.ps1` — Windows SimpliSign keeper
- `scripts/ci/windows/set_simplisign_env.ps1` — Windows env setup (contains secrets, should not be in repo)
- `scripts/ci/windows/deploy_to_runner.ps1` — Windows runner deployment
- `scripts/ci/windows/start_simplisign_keeper.vbs` — VBS wrapper

**Files to KEEP:**
- `scripts/ci/windows/README.md` — Reference documentation (update to reflect new macOS-based flow)
- `desktop/src-tauri/windows-sign.sh` — New cross-platform signing wrapper
- `scripts/ci/macos/windows-sign.sh` — macOS osslsigncode signing
- `scripts/ci/macos/simplisign-login.sh` — macOS SimpliSign auto-login
- `.github/workflows/test-macos-runner.yml` — Remove after CI confirmed (temporary test)

**Makefile:**
- Remove `build-windows-test` target (line 65-66) — no longer needed

### Task 3.2: Remove test workflow

```bash
git rm .github/workflows/test-macos-runner.yml
```

### Task 3.3: Update CLAUDE.md

Update `CLAUDE.md` Quick Commands section — change `make build-windows` description to note it now cross-compiles from macOS.

### Task 3.4: Phase 3 Review

Run `/using-superpowers` to review all cleanup changes. Confidence must be 10/10.

**Checklist:**
- [ ] No orphaned references to removed files
- [ ] No secrets left in tracked files
- [ ] CI still passes after cleanup
- [ ] CLAUDE.md reflects new build flow

**Final commit:**
```bash
git add -A
git commit -m "chore: remove Windows-only build scripts (replaced by macOS cross-build)"
```

---

## Appendix: Environment Requirements

For any macOS machine to run this pipeline:

| Tool | Install | Version Verified |
|------|---------|-----------------|
| cargo-xwin | `cargo install --locked cargo-xwin` | (to install) |
| makensis | `brew install nsis` | 3.11 |
| osslsigncode | `brew install osslsigncode` | installed |
| libp11 | `brew install libp11` | installed (PKCS#11 engine) |
| Rust target | `rustup target add x86_64-pc-windows-msvc` | installed |
| clang-cl | `brew install llvm` | 21.1.5 |
| Go | `brew install go` or via `setup-go` | 1.24 |
| SimplySign Desktop | Manual install from Certum | running |
| pyotp | `pip3 install pyotp` | for TOTP generation |
| pkcs11-tool | `brew install opensc` | for token verification |

**PKCS#11 engine symlink** (one-time):
```bash
ln -sf /opt/homebrew/Cellar/libp11/*/lib/engines-3/pkcs11.dylib \
  /opt/homebrew/Cellar/openssl@3/*/lib/engines-3/pkcs11.dylib
```

**SimplySign config paths:**
- PKCS#11 engine: `/opt/homebrew/Cellar/openssl@3/3.6.1/lib/engines-3/pkcs11.dylib`
- PKCS#11 module: `/usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib`
- Certificate: `pkcs11:token=wordgate;object=334AB051AA095E46AF497253EB398C98;type=cert`

Note: OpenSSL and libp11 version paths may differ across macOS machines. The signing script should auto-detect or the paths should be parameterized via environment variables.
