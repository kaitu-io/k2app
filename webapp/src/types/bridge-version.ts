/**
 * BRIDGE_API_VERSION — single integer version of the ENTIRE webapp↔shell
 * bridge surface (`window._k2` / `window._platform`): the Capacitor named
 * methods (mobile/plugins/k2-plugin/src/definitions.ts), the Tauri named
 * commands (desktop/src-tauri/src/main.rs generate_handler!), and the daemon
 * HTTP actions they proxy.
 *
 * BUMP THIS — and add a matching entry to contracts/bridge-versions.json —
 * whenever the bridge surface gains a method the webapp depends on, or a
 * method changes/disappears. The gate in
 * src/types/__tests__/bridge-contract.test.ts turns method-table drift
 * without a bump into a red test (2026-03 storageGet incident, never again).
 *
 * Known blind spot (spec §10, recorded honestly): the gate covers method-table
 * add/remove only. A behavior change behind an unchanged signature still relies
 * on review + bump discipline.
 *
 * Native mirrors (each checked against this value by the same gate):
 *   Android: K2PluginUtils.BRIDGE_API_VERSION
 *   iOS:     k2BridgeApiVersion (K2Helpers.swift)
 */
export const BRIDGE_API_VERSION = 1;
