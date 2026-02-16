# Plan: openwrt-support

## Meta

| Field   | Value                                              |
|---------|----------------------------------------------------|
| Feature | openwrt-support                                    |
| Spec    | docs/features/openwrt-support.md                   |
| Created | 2026-02-16                                         |
| Status  | draft                                              |

## Complexity Assessment

**Moderate** — 10-15 files, no major refactoring (webapp embed already exists), mostly additive changes.

Key simplification: `k2/cloud/embed.go` + `daemon/api.go:webappHandler()` already implement
webapp embedding and SPA serving. Desktop builds use `-tags nowebapp` to disable it.
OpenWrt just needs to build WITHOUT that tag, with real webapp files in `cloud/dist/`.

## Existing Infrastructure (leveraged, not modified)

| File | What it does |
|------|-------------|
| `k2/cloud/embed.go` | `//go:embed dist/*` — embeds `cloud/dist/` into binary |
| `k2/cloud/embed_nowebapp.go` | `//go:build nowebapp` — returns error (desktop mode) |
| `k2/daemon/api.go:webappHandler()` | SPA serving: FileServer + index.html fallback |
| `k2/daemon/daemon.go:83` | `mux.Handle("/", webappHandler())` already registered |
| `k2/cloud/gen.go` | Download helper (optional, we'll copy directly) |

## Task Dependency Graph

```
T1 (Go: listen + CORS) ──┐
                          ├── T3 (Build + Package) ── T4 (CI/CD)
T2 (Webapp: same-origin) ─┘
```

T1 and T2 are parallel (different repos: k2/ vs webapp/).
T3 depends on both (build script needs Go + webapp changes).
T4 depends on T3 (CI workflow tests the build script).

**Recommended execution**: Single branch (submodule changes are small, worktree overhead not justified per task-splitting knowledge).

---

## T1: Go Daemon — Configurable Listen Address + LAN CORS

**Branch**: `feat/openwrt-support` (single branch for all tasks)
**Scope**: k2 submodule only

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `k2/config/config.go` | Add `Listen string` to ClientConfig + default |
| MODIFY | `k2/daemon/daemon.go` | Accept listen addr from config in `Run()` |
| MODIFY | `k2/daemon/api.go` | Expand CORS to allow LAN origins when served from same origin |
| MODIFY | `k2/daemon/api_test.go` | Tests for new behavior |

### Implementation Details

**config/config.go**:
```go
type ClientConfig struct {
    Listen string       `yaml:"listen"` // daemon HTTP listen address, default "127.0.0.1:1777"
    Server string       `yaml:"server"` // k2v5:// URL
    // ... rest unchanged
}

func setClientDefaults(cfg *ClientConfig) {
    if cfg.Listen == "" {
        cfg.Listen = "127.0.0.1:1777"
    }
    // ... rest unchanged
}
```

**daemon/daemon.go**:
```go
// Run now accepts an optional listen address.
func (d *Daemon) Run(ctx context.Context, listenAddr string) error {
    if listenAddr == "" {
        listenAddr = DefaultAddr
    }
    log.Printf("k2 daemon starting on %s", listenAddr)
    // ... use listenAddr instead of DefaultAddr for srv.Addr
}
```

**daemon/api.go — CORS**:
When webapp is served from daemon itself (same-origin), requests have no `Origin` header — CORS is irrelevant.
When accessed from LAN (e.g., LuCI iframe on different port), `Origin` will be the LuCI host.
Safest approach: if no `Origin` header (same-origin request), skip CORS. If `Origin` present, allow it when listening on `0.0.0.0` (router mode).

**cmd/k2/cmd_run.go**: Update `runDaemon()` to pass config listen address to `d.Run()`.

### TDD Steps

**RED**:
- `TestDaemonRunCustomListenAddr` — verify daemon starts on custom address
- `TestConfigListenDefault` — verify default listen is 127.0.0.1:1777
- `TestConfigListenFromYAML` — verify listen parsed from config file
- `TestCORSSameOriginNoHeader` — verify no CORS headers when no Origin (same-origin)

**GREEN**:
- Add `Listen` field to ClientConfig
- Update `setClientDefaults` with default value
- Modify `Daemon.Run()` to accept listen addr parameter
- Update `cmdRun` to pass config value
- CORS: skip when no Origin header (same-origin requests don't send Origin)

**REFACTOR**:
- [SHOULD] Extract listen addr validation helper
- [SHOULD] Log warning when listening on 0.0.0.0 (security reminder)

### AC Coverage
- AC1 (webapp serving) — partially: Go side ready (existing code)
- AC3 (configurable listen) — full
- AC8 (no regression) — full: default unchanged

---

## T2: Webapp — Same-Origin HttpVpnClient + Platform Hiding

**Branch**: same as T1
**Scope**: webapp/ only

### Files

| Action | File | Description |
|--------|------|-------------|
| MODIFY | `webapp/src/vpn-client/http-client.ts` | Same-origin baseUrl detection |
| MODIFY | `webapp/src/components/ServiceReadiness.tsx` | Skip on WebPlatform |
| MODIFY | `webapp/src/components/UpdatePrompt.tsx` | Hide on WebPlatform |
| MODIFY | `webapp/src/vpn-client/__tests__/http-client.test.ts` | Tests for same-origin |

### Implementation Details

**http-client.ts**:
```typescript
constructor() {
    // Same-origin mode: when served by daemon itself (port 1777),
    // use relative paths (no baseUrl needed).
    // Tauri mode: absolute URL to localhost:1777.
    // Dev mode: empty (Vite proxy handles it).
    if (import.meta.env.DEV) {
        this.baseUrl = '';
    } else if (window.__TAURI__) {
        this.baseUrl = 'http://127.0.0.1:1777';
    } else {
        // WebPlatform (browser, OpenWrt) — same-origin
        this.baseUrl = '';
    }
}
```

Wait — this is simpler than I thought. In dev mode it's already `''`. In production without Tauri, it should also be `''` (same-origin). The only case needing absolute URL is Tauri desktop (webview served from localhost:14580, needs to reach daemon on :1777).

Simplified: `this.baseUrl = window.__TAURI__ ? 'http://127.0.0.1:1777' : ''`

**ServiceReadiness.tsx**: Check `getPlatform().isTauri` (or similar). When on WebPlatform, render children directly (skip daemon readiness check — webapp IS the daemon).

Actually, looking at the existing platform detection: `__TAURI__` is set by Tauri, `Capacitor.isNativePlatform()` for mobile. On OpenWrt, neither is set → WebPlatform. ServiceReadiness should pass through on WebPlatform since the webapp is served by the daemon itself (if it's serving, it's ready).

**UpdatePrompt.tsx**: Similar — hide when on WebPlatform.

### TDD Steps

**RED**:
- `test_http_client_same_origin_mode` — baseUrl is '' when not Tauri
- `test_http_client_tauri_mode` — baseUrl is absolute URL when __TAURI__ set
- `test_service_readiness_skips_on_web_platform` — renders children immediately
- `test_update_prompt_hidden_on_web_platform` — not rendered

**GREEN**:
- Simplify baseUrl logic in HttpVpnClient constructor
- Add WebPlatform bypass to ServiceReadiness
- Add WebPlatform check to UpdatePrompt

**REFACTOR**:
- [SHOULD] Add `isEmbeddedWebapp()` helper to platform detection (cleaner than checking __TAURI__)

### AC Coverage
- AC2 (same-origin HttpVpnClient) — full
- AC8 (no regression) — full: Tauri behavior unchanged

---

## T3: Build Scripts + Packaging

**Branch**: same
**Scope**: scripts/, Makefile, scripts/openwrt/

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `scripts/build-openwrt.sh` | Cross-compile + package for all architectures |
| CREATE | `scripts/openwrt/install.sh` | OpenWrt one-click installer |
| CREATE | `scripts/openwrt/k2.init` | procd init.d service script |
| CREATE | `scripts/openwrt/luci-app-k2/controller/k2.lua` | LuCI menu entry |
| CREATE | `scripts/openwrt/luci-app-k2/view/k2.htm` | LuCI iframe page |
| MODIFY | `Makefile` | Add `build-openwrt` target |

### Implementation Details

**scripts/build-openwrt.sh**:
```bash
#!/bin/bash
set -euo pipefail

VERSION=${VERSION:-$(node -p "require('./package.json').version")}
COMMIT=$(cd k2 && git rev-parse --short HEAD)
OUTDIR="release/openwrt/${VERSION}"

TARGETS=(
    "linux:arm64::aarch64"
    "linux:amd64::x86_64"
    "linux:arm:7:armv7"
    "linux:mipsle::mipsle"
)

# 1. Build webapp
echo "=== Building webapp ==="
cd webapp && yarn build && cd ..

# 2. Copy dist to cloud embed path
echo "=== Copying webapp to k2/cloud/dist/ ==="
rm -rf k2/cloud/dist
cp -r webapp/dist k2/cloud/dist

# 3. Cross-compile each target
mkdir -p "${OUTDIR}"
for target in "${TARGETS[@]}"; do
    IFS=':' read -r goos goarch goarm name <<< "$target"
    echo "=== Building k2-openwrt-${name} ==="

    env CGO_ENABLED=0 GOOS=${goos} GOARCH=${goarch} ${goarm:+GOARM=${goarm}} \
        go build \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o "${OUTDIR}/k2-openwrt-${name}" \
        ./k2/cmd/k2

    # Verify architecture
    file "${OUTDIR}/k2-openwrt-${name}"

    # Package tar.gz
    PKGDIR=$(mktemp -d)
    cp "${OUTDIR}/k2-openwrt-${name}" "${PKGDIR}/k2"
    cp scripts/openwrt/install.sh "${PKGDIR}/"
    cp scripts/openwrt/k2.init "${PKGDIR}/"
    cp -r scripts/openwrt/luci-app-k2 "${PKGDIR}/"

    tar -czf "${OUTDIR}/k2-openwrt-${name}-v${VERSION}.tar.gz" -C "${PKGDIR}" .
    rm -rf "${PKGDIR}"

    echo "=== Packaged k2-openwrt-${name}-v${VERSION}.tar.gz ==="
done

# 4. Restore cloud/dist (put back placeholder)
git -C k2 checkout -- cloud/dist/

echo "=== Build complete ==="
ls -la "${OUTDIR}"
```

**Makefile addition**:
```makefile
build-openwrt: pre-build
	bash scripts/build-openwrt.sh
```

**scripts/openwrt/install.sh**: As specified in feature spec PR3.
**scripts/openwrt/k2.init**: As specified in feature spec PR4 (procd).
**scripts/openwrt/luci-app-k2/**: As specified in feature spec PR5.

### TDD Steps

**RED**:
- `test_build_openwrt_produces_binaries` — shell: verify 4 binaries exist after build
- `test_build_openwrt_correct_arch` — shell: `file` checks ELF architecture
- `test_package_contains_all_files` — shell: tar lists all expected files
- `test_install_script_syntax` — shell: `sh -n install.sh` (syntax check)

**GREEN**:
- Create all files listed above
- Verify with local `make build-openwrt` (at least amd64 target)

**REFACTOR**:
- [SHOULD] Add `--arch` flag to build-openwrt.sh for single-arch builds (faster dev iteration)

### AC Coverage
- AC1 (webapp embedded) — full: binary built without nowebapp tag
- AC4 (cross-compile) — full: 4 architectures
- AC5 (packaging) — full: tar.gz with all files
- AC6 (LuCI) — full: luci-app-k2 files included

---

## T4: CI/CD — GitHub Actions Release Workflow

**Branch**: same
**Scope**: .github/workflows/

### Files

| Action | File | Description |
|--------|------|-------------|
| CREATE | `.github/workflows/release-openwrt.yml` | OpenWrt release workflow |

### Implementation Details

```yaml
name: Release OpenWrt

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
            name: aarch64
          - goos: linux
            goarch: amd64
            name: x86_64
          - goos: linux
            goarch: arm
            goarm: '7'
            name: armv7
          - goos: linux
            goarch: mipsle
            name: mipsle

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
        run: yarn install --frozen-lockfile

      - name: Build webapp
        run: cd webapp && yarn build

      - name: Copy webapp to k2 embed path
        run: |
          rm -rf k2/cloud/dist
          cp -r webapp/dist k2/cloud/dist

      - name: Cross-compile k2
        env:
          CGO_ENABLED: '0'
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          GOARM: ${{ matrix.goarm }}
        run: |
          VERSION=$(node -p "require('./package.json').version")
          COMMIT=$(cd k2 && git rev-parse --short HEAD)
          cd k2 && go build \
            -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
            -o ../build/k2-openwrt-${{ matrix.name }} ./cmd/k2

      - name: Verify binary architecture
        run: file build/k2-openwrt-${{ matrix.name }}

      - name: Smoke test with qemu
        run: |
          sudo apt-get update && sudo apt-get install -y qemu-user-static binfmt-support
          build/k2-openwrt-${{ matrix.name }} version

      - name: Package tar.gz
        run: |
          VERSION=$(node -p "require('./package.json').version")
          PKGDIR=$(mktemp -d)
          cp build/k2-openwrt-${{ matrix.name }} "${PKGDIR}/k2"
          cp scripts/openwrt/install.sh "${PKGDIR}/"
          cp scripts/openwrt/k2.init "${PKGDIR}/"
          cp -r scripts/openwrt/luci-app-k2 "${PKGDIR}/"
          tar -czf build/k2-openwrt-${{ matrix.name }}-v${VERSION}.tar.gz -C "${PKGDIR}" .
          rm -rf "${PKGDIR}"

      - name: Upload to S3
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
        run: |
          VERSION=$(node -p "require('./package.json').version")
          aws s3 cp build/k2-openwrt-${{ matrix.name }}-v${VERSION}.tar.gz \
            s3://d0.all7.cc/kaitu/openwrt/${VERSION}/

      - name: Notify Slack on success
        if: success()
        run: |
          VERSION=$(node -p "require('./package.json').version")
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "OpenWrt-${{ matrix.name }}"
        env:
          SLACK_WEBHOOK_RELEASE: ${{ secrets.SLACK_WEBHOOK_RELEASE }}

      - name: Notify Slack on failure
        if: failure()
        run: |
          ./scripts/ci/notify-slack.sh build-failure \
            --platform "OpenWrt-${{ matrix.name }}" \
            --error "OpenWrt build failed for ${{ matrix.name }}"
        env:
          SLACK_WEBHOOK_ALERT: ${{ secrets.SLACK_WEBHOOK_ALERT }}
```

### TDD Steps

**RED**:
- `test_workflow_syntax` — `actionlint release-openwrt.yml` (if available)
- `test_workflow_triggers_on_tag` — manual: push test tag to verify trigger
- Verify all `secrets.*` references match existing secrets

**GREEN**:
- Create workflow file
- Test with `workflow_dispatch` trigger first

**REFACTOR**:
- [SHOULD] Extract version/commit computation to shared action or script
- [SHOULD] Add binary size check step (fail if > 25MB)

### AC Coverage
- AC4 (cross-compile) — qemu smoke test
- AC7 (CI/CD) — full: tag trigger, S3 upload, Slack notification

---

## AC Coverage Matrix

| AC | Description | Test(s) | Task |
|----|-------------|---------|------|
| AC1 | Webapp embedded serving | `test_build_openwrt_produces_binaries`, manual browse to :1777 | T3 |
| AC2 | Same-origin HttpVpnClient | `test_http_client_same_origin_mode`, `test_http_client_tauri_mode` | T2 |
| AC3 | Configurable listen | `TestDaemonRunCustomListenAddr`, `TestConfigListenDefault`, `TestConfigListenFromYAML` | T1 |
| AC4 | Cross-compile | `test_build_openwrt_correct_arch`, qemu smoke in CI | T3, T4 |
| AC5 | Packaging | `test_package_contains_all_files`, `test_install_script_syntax` | T3 |
| AC6 | LuCI integration | Manual: verify LuCI menu + iframe | T3 |
| AC7 | CI/CD auto-publish | Workflow run on tag push | T4 |
| AC8 | Desktop no regression | `yarn test` (284 tests), `cargo test` (4 tests), existing CI | T1, T2 |

## Deliverable Ownership Check

| Deliverable | Owner Task |
|-------------|-----------|
| `k2/config/config.go` (Listen field) | T1 |
| `k2/daemon/daemon.go` (listen addr) | T1 |
| `k2/daemon/api.go` (CORS) | T1 |
| `webapp/src/vpn-client/http-client.ts` | T2 |
| `webapp/src/components/ServiceReadiness.tsx` | T2 |
| `webapp/src/components/UpdatePrompt.tsx` | T2 |
| `scripts/build-openwrt.sh` | T3 |
| `scripts/openwrt/install.sh` | T3 |
| `scripts/openwrt/k2.init` | T3 |
| `scripts/openwrt/luci-app-k2/` | T3 |
| `Makefile` (build-openwrt target) | T3 |
| `.github/workflows/release-openwrt.yml` | T4 |

No orphan deliverables.

## Execution Summary

| Phase | Tasks | Parallelism | Files |
|-------|-------|-------------|-------|
| 1 | T1 + T2 | Parallel (k2/ vs webapp/) | 6 files |
| 2 | T3 | Sequential (depends on T1+T2) | 6 new files |
| 3 | T4 | Sequential (depends on T3) | 1 new file |

**Total**: 13 files (4 modified, 9 new)
**Critical path**: T1 → T3 → T4 (or T2 → T3 → T4)
**Estimated complexity**: Moderate (existing embed infra eliminates largest risk)
