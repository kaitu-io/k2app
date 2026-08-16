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
 * iOS caveat (bridge v1, recorded 2026-08-14): the TS interface below declares
 * `getUpdateChannel`/`setUpdateChannel`, but shipped iOS 0.4.8 does NOT
 * implement them — only Android 0.4.8 does. iOS support first ships in the
 * release AFTER 0.4.8. Webapp MUST keep the `getPlatform() === 'android'` gate
 * in capacitor-k2.ts until a bridge v2 bump anchored at that first iOS
 * release carrying them. This is a specific instance of a general blind spot:
 * per-platform native implementation drift under an unchanged TS declaration
 * is invisible to this gate — the method-table snapshot matches on both
 * platforms even when one platform's native silently lacks the method.
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
 */
export const BRIDGE_API_VERSION = 2;
