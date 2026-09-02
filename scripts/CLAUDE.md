# scripts — Build, Deploy, and Test Helpers

Shell/Node helpers orchestrated by the root `Makefile` and `.github/workflows/`. Most are
invoked through a `make` target or a workflow step, not directly — check the caller for the
canonical invocation before running one by hand.

## Build-driven (Makefile / webapp `package.json`)

| Group | Scripts |
|-------|---------|
| Dev servers | `dev-standalone.sh`, `dev-macos.sh`, `dev-windows.sh` (dev daemon on `:11777`, not `:1777`), `dev-openwrt.sh` |
| Build | `build-k2.sh`, `build-k2-standalone.sh` (k2/k2s for linux+darwin), `build-macos.sh` (k2 universal → tauri → re-sign → `.app.tar.gz` → `pkgbuild` `.pkg` → notarize → brand purity gate), `build-mobile-{ios,android}.sh`, `build-openwrt.sh`, `gen-embedded-seed.js` (`make fetch-embedded-seed`: rewrites `webapp/src/services/antiblock-seed-embedded.ts`, fail-soft) |
| Publish / deploy | `publish-desktop.sh` (`--brand=`, `--channel=`; latest.json ×2 + GitHub Release), `publish-mobile.sh`, `publish-k2.sh`, `publish-docker.sh`, `deploy-center.sh` |
| Brand purity | `check-desktop-brand-purity.sh <brand> <path>` (checks `.app.tar.gz` by **content**), `check-mobile-brand-purity.sh`, `apply-ios-brand.sh`. The webapp dist gate `check-brand-purity.sh` and `smoke-dist.mjs` live in **`webapp/scripts/`**, not here |
| Verification | `test_build.sh` (count computed at runtime), `check-embed-size.sh` (committed `k2/rule/embed/krs.tar.gz` ≤ 300 KB, reads the k2 HEAD blob), `check-i18n.mjs` (webapp `yarn build` runs it `--ci`: locale keys vs zh-CN), `test-openwrt.sh` + `openwrt/` (`install.sh`, `k2r.init`, `luci-app-k2r`) |
| iOS device | `detect-ios-device.sh`, `deploy-ios-device.sh`, `ios-logs.sh` |
| Misc | `sync-version.sh` (package.json → Cargo.toml / build.gradle / K2Helpers.swift / pbxproj; run by `make pre-build`), `sync-adb-tools.sh`, `pkg-scripts/{preinstall,postinstall}` (macOS PKG hooks; `@APP_NAME@` / `@BUNDLE_ID@` templated by `build-macos.sh`) |

## CI-driven (`ci/` unless noted)

- `ci/api-db-test.sh [config.yml]` (`ci.yml` `test-api-db`) — runs the api suite against a real MariaDB and **fails on any `config.yml not available` skip**. Plain `go test ./...` in `api/` silently skips every DB test when `center/config.yml` is absent (256 of 1085 at 0.4.8) and still reports green.
- `check-k2-plugin-fresh.sh` (webapp `pretest` + `test-webapp-reusable.yml`) — `mobile/plugins/k2-plugin/dist` vs `tsc(src)`, and freshness of the yarn `file:` copy.
- `test-ios-build-number.sh` (`ci.yml`) — drives `build-mobile-ios.sh --print-build-number` instead of re-implementing the formula.
- `ci/web-ota-manifest.mjs` (+ `.test.mjs`; `publish-web-ota.yml`) — `version` = root version + seconds since 2026-01-01; `manifest` derives `min_native` / `min_desktop` / `min_linux` / `min_bridge` from `contracts/webapp-support-floor.json` (support floor — never hand-written).
- `antiblock-cursor.sh`, `antiblock-cursor.test.sh`, `antiblock-encrypt.js` (+ `antiblock-keygen.js`) (`publish-antiblock.yml`) — AES-256-GCM JSONP antiblock config.
- `ci/upload-release.sh --windows|--macos|--linux|--android [--skip-cdn] [--brand=kaitu|overleap]` — S3 upload + CDN invalidation, one platform per call; `--web` is handled by `publish-web-ota.yml`, not this script.
- Windows signing chain: `desktop/src-tauri/windows-sign.sh` (Tauri `signCommand`, journals to `$K2_SIGN_LOG`) dispatches to `ci/macos/windows-sign.sh` (osslsigncode + SimplySign PKCS#11 + `-ac certum-chain.pem`) or `ci/windows/sign-binary.ps1` (signtool on a Windows host). `ci/macos/simplisign-login.sh` (`make simplisign-login`), `simplisign-keepalive.sh` + `install-simplisign-keepalive.sh` (LaunchAgent re-login every ~90 min), and `ci/windows-sign-preflight.sh` (signs a throwaway k2.exe before the 15-min bundle) keep that chain alive.
- `ci/windows/assert-ui-rendered.ps1` (`ci.yml` `smoke-windows`) — reads the WebView2 accessibility tree to prove the desktop UI actually rendered; must stay ASCII-only.
- `ci/notify-slack.sh`, `ci/generate-k2s-manifest.js` (`release-k2s.yml`), `ci/push-secrets.sh` (pushes the listed env vars as GitHub secrets).
- `lambda/s3-log-notify/` — S3 → notify Lambda for log uploads; understands `desktop/`, `mobile/` and the legacy `service-logs/` / `feedback-logs/` prefixes.

Node-side ops scripts are **not** here — they live in `docker/scripts/` (`provision-node.sh`, `auto-update.sh`, …).

## Manual-only (no Makefile / workflow caller)

`k2-quick-diag.sh` (client log triage), `check-nat-type.py` / `check-game-udp.py` (UDP NAT + game-protocol probes through a live tunnel), `test-k2r-webapp.sh`, `test-macos-ne-build.sh`, `test-publish-mobile.sh`, `test_version_propagation.sh`, `test-windows-service-smoke.ps1` (admin; k2 service install → ping → reinstall → uninstall), `stripe-setup-overleap.sh`, `asc-iap-setup.mjs`, `k2subs-uat/` (daemon on `:11777`), `enterprise-router/` (OpenWrt image), plus the Windows k2 test launchers below.

## Windows k2 Test Workflow (needs repair)

`start-k2-admin.ps1` / `test-k2-tun.bat` elevate via UAC and run `k2 run -c k2-test-config.yml`
(daemon on `:1778`; `k2-test-config.yml` is gitignored — derive it from the committed
`k2-test-proxy-config.yml`: proxy mode, SOCKS5 `:1080`). `test-k2-ctl.sh {up|down|status|debug|info|logs|test}`
drives it from Git Bash. Known drift: `up` still posts the deprecated top-level `server` field and
must move to `routes: [{via}]`; the proxy config logs to `stderr`, so `logs` tails a file the daemon
never writes; both launchers hardcode a personal log path.
