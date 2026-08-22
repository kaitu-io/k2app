/**
 * BRIDGE_API_VERSION — single integer version of the ENTIRE webapp↔shell
 * bridge surface (`window._k2` / `window._platform`): the Capacitor named
 * methods (mobile/plugins/k2-plugin/src/definitions.ts), the Tauri named
 * commands (desktop/src-tauri/src/main.rs generate_handler!), and the daemon
 * HTTP actions they proxy.
 *
 * BUMP THIS whenever the bridge surface gains a method the webapp depends
 * on, or a method changes/disappears. The gate in
 * src/types/__tests__/bridge-contract.test.ts turns method-table drift
 * without a bump into a red test (2026-03 storageGet incident, never again).
 *
 * R2 semantics (spec §4): bumping does NOT lock old apps out — the manifest
 * publishes the SUPPORT FLOOR (contracts/webapp-support-floor.json), not the
 * current version. The consequence of a bump is an obligation on the webapp:
 * every use of the new surface must sit behind runtime capability detection
 * (existence check preferred over version compare — see webapp/CLAUDE.md
 * 兼容模型 section). Raising the floor is a separate, explicit
 * support-drop decision.
 *
 * Known blind spot (spec §10, recorded honestly): the gate covers method-table
 * add/remove only. A behavior change behind an unchanged signature still relies
 * on review + bump discipline.
 *
 * Platform-drift blind spot (recorded 2026-08-14, corrected 2026-08-18): the
 * method-table snapshot matches on both platforms even when one platform's
 * native silently lacks a declared method, so this gate cannot see per-platform
 * implementation drift. The original instance was `getUpdateChannel` /
 * `setUpdateChannel`: declared in TS, implemented on Android first. It is now
 * resolved — iOS implements both (K2Plugin.swift, endpoints built from
 * `channelPrefix`), and they ship on BOTH natives in 0.4.8, the first release
 * to carry them at all. (The earlier note claimed "shipped iOS 0.4.8 does not
 * implement them"; no 0.4.8 was ever shipped — it was built on 2026-08-03 and
 * never published, so nothing in the field has these methods yet.)
 *
 * The lasting rule is the one that outlived that instance: gate on CAPABILITY,
 * never on platform. capacitor-k2.ts now probes with getUpdateChannel and only
 * exposes setChannel when the probe succeeds, which covers both the 0.4.7-and-
 * older field population and any future drift in either direction.
 *
 * Shell mirrors — every shell declares which surface version it implements.
 * A shell only implements the parts of the surface that apply to it (a mobile
 * shell can never carry a Tauri command), so "mirror == this value" means
 * "up to date with the surface", not "implements every method in it".
 *   Android: K2PluginUtils.BRIDGE_API_VERSION          — gated by this test
 *   iOS:     k2BridgeApiVersion (K2Helpers.swift)      — gated by this test
 *   Desktop: DESKTOP_BRIDGE_VERSION (web_ota.rs)       — gated by this test
 *   Linux:   LinuxBridgeVersion (k2/webui/webota.go)   — NOT gated: k2 is a
 *     submodule that a fresh worktree may not have checked out, and a gate on
 *     unpopulated content is a gate that fails for the wrong reason. Bumping
 *     it is a k2-repo commit + submodule pointer bump; carry it in the same
 *     change set as a bump here.
 *
 * v2 (2026-08-16): desktop web OTA added five Tauri commands the webapp calls
 * — `ui_boot_ok` (tauri-k2.ts) and `storage_migration_{put,get,clear,done}`
 * (desktop-storage-migration.ts). They shipped with the golden regenerated but
 * without a bump, which let bridge v1 denote two different surfaces; this bump
 * restores the invariant. Zero behavioral effect: the manifest publishes
 * min_bridge = floor (1), so every shell in the field still passes its gate.
 *
 * v3 (2026-08-22): Capacitor gained `confirmWebBootOk` — the mobile web-OTA
 * boot handshake. It exists because clearing `.boot-pending` inside
 * `checkReady()` cleared it during bridge init, i.e. before store init and the
 * first React render, so a bundle that died in either stage still counted as
 * "booted" and the rollback never fired. Desktop already had the correct shape
 * (`ui_boot_ok`, called from main.tsx after ReactDOM.render); this brings the
 * mobile shells in line. Capability-gated as required: `confirmWebBootOk()` in
 * capacitor-k2.ts swallows the rejection on shells that lack the method.
 * Zero behavioral effect on the field for the same reason as v2 — the manifest
 * publishes min_bridge = floor (1).
 */
export const BRIDGE_API_VERSION = 3;
