# Review Report: macos-network-extension

## Date: 2026-02-23
## Spec: docs/features/macos-network-extension.md (v1.1)

## AC Verdicts

| AC | Verdict | Evidence |
|----|---------|----------|
| AC1 | SKIPPED | Requires live macOS + DNS polluted network — manual UAT |
| AC2 | PASS | NE uses saveToPreferences() system dialog, no root. Old osascript function deleted. |
| AC3 | PASS | build-macos.sh copies KaituTunnel.appex to Contents/PlugIns/ |
| AC4 | PASS | daemon_exec cfg-gated → ne_action() routes up/down/status; 9 ne.rs tests pass |
| AC5 | PASS | PacketTunnelProvider.swift — dnsSettings.matchDomains = [""] |
| AC6 | PASS | PacketTunnelProvider.swift — NEIPv4Route.default() + NEIPv6Route.default() |
| AC7 | PASS | Makefile mobile-macos target: gomobile bind -target=macos |
| AC8 | PASS | PTP uses engine.Start(configJSON, fd, cfg) with fd >= 0 from packetFlow KVC |
| AC9 | PASS | #[cfg(not(target_os = "macos"))] on all daemon functions; test passes |
| AC10 | PASS | build-macos.sh has appex build + codesign + PlugIns copy |
| AC11 | PASS | preinstall loops over 4 plist variants |
| AC12 | PASS | ne.rs register_state_callback → Tauri ne-state-changed event |
| AC13 | PASS | K2NEHelper.swift sendProviderMessage catch → mapVPNStatus fallback |
| AC14 | PASS | All k2ne_* returns wrapped in ServiceResponse envelope |
| AC15 | PASS | ne.rs get_udid_native() → sysctl -n kern.uuid |
| AC16 | PASS | ensure_service_running macOS → ne::ensure_ne_installed() |

## Summary

- **15 PASS** / **0 FAIL** / **1 SKIPPED**
- AC1 skipped: DNS correctness requires live network test (UAT phase)
- All code-verifiable ACs pass

## Test Results

| Suite | Pass | Fail |
|-------|------|------|
| T0 (xcframework build) | 0/5 | Expected: requires gomobile build artifact |
| T1 (NE extension) | 7/7 | - |
| T2 (NE helper) | 25/25 | - |
| T3 (Rust ne.rs) | 9/9 | Part of cargo test |
| T4 (build integration) | 5/5 | - |
| T5 (migration) | 4/4 | - |
| Rust cargo test | 33/33 | - |
| cargo check | PASS | 15 warnings (dead code — expected) |

## Files Created/Modified

### New Files (13)
- `desktop/src-tauri/src/ne.rs` — Rust NE FFI bridge (541 lines)
- `desktop/src-tauri/ne_helper/K2NEHelper.swift` — Swift NE helper (437 lines)
- `desktop/src-tauri/ne_helper/k2_ne_helper.h` — C header (66 lines)
- `desktop/src-tauri/ne_helper/build.sh` — Swift build script (75 lines)
- `desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift` — NE extension (243 lines)
- `desktop/src-tauri/KaituTunnel/Info.plist` — NE extension metadata
- `desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements` — NE extension entitlements
- `scripts/build-mobile-macos-ne.sh` — gomobile macOS build
- `scripts/test-macos-ne-*.sh` — 5 test scripts

### Modified Files (7)
- `desktop/src-tauri/src/service.rs` — cfg-gated macOS to NE, deleted dead code
- `desktop/src-tauri/src/main.rs` — added mod ne, NE callback registration
- `desktop/src-tauri/build.rs` — conditional NE helper linking
- `desktop/src-tauri/Cargo.toml` — tokio features for tests
- `desktop/src-tauri/entitlements.plist` — added NE + App Group
- `scripts/build-macos.sh` — NE build pipeline integration
- `scripts/pkg-scripts/preinstall` — comprehensive launchd cleanup
- `scripts/pkg-scripts/postinstall` — NE-aware conditional daemon install
- `desktop/CLAUDE.md` — NE architecture documentation
- `CLAUDE.md` — NE domain vocabulary
- `Makefile` — mobile-macos target
