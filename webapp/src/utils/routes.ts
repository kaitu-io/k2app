/**
 * Routes builder — translates Center-suggested routing profile names into
 * the wire-contract `routes[]` shape expected by the Go engine.
 *
 * Center API returns a `suggestedProfile` string on the user profile endpoint
 * based on the request IP country (e.g. CN → "cnroute", IR → "iroute",
 * fallback → "global"). This module is the pure translation layer from that
 * profile name to the outbound route list.
 *
 * Profile naming convention: `{cc}route` maps to preset `{cc}-access` (see
 * `k2/rule/target.go` for the canonical preset list). New country presets
 * added to the Go side should be added to `KNOWN_PROFILES` here.
 *
 * The shapes:
 *
 * - `"global"` → everything goes through the tunnel.
 *   ```
 *   [{ via: <serverUrl>, match: { all: true } }]
 *   ```
 *
 * - `"{cc}route"` → traffic matching the `{cc}-access` preset bypasses the
 *   tunnel (direct), everything else falls through to the tunnel.
 *   ```
 *   [
 *     { via: "direct",     match: { preset: "{cc}-access" } },
 *     { via: <serverUrl>, match: {} },
 *   ]
 *   ```
 *
 * Unknown profile names fall back to `"global"` with a console warning so
 * we never ship a broken route table to the daemon.
 */

import type { PresetName, RouteConfig } from '../types/client-config';

/**
 * Canonical profile → preset mapping.
 *
 * Keep in sync with `k2/rule/target.go presets`. The map values are the
 * preset name (what goes into `match.preset`), not the profile name.
 */
export const PROFILE_TO_PRESET: Readonly<Record<string, PresetName>> = Object.freeze({
  cnroute: 'cn-access',
  iroute: 'ir-access',
  ruroute: 'ru-access',
  troute: 'tr-access',
  pkroute: 'pk-access',
  vnroute: 'vn-access',
  mmroute: 'mm-access',
  egroute: 'eg-access',
  idroute: 'id-access',
  saroute: 'sa-access',
  aeroute: 'ae-access',
  throute: 'th-access',
  bdroute: 'bd-access',
  byroute: 'by-access',
});

/** Set of recognized profile names (including "global"). */
export const KNOWN_PROFILES: ReadonlySet<string> = new Set([
  'global',
  ...Object.keys(PROFILE_TO_PRESET),
]);

/**
 * Translate a suggested profile name into a concrete `routes[]` list.
 *
 * @param profile - profile name from Center (e.g. `"cnroute"`, `"global"`)
 * @param serverUrl - resolved k2v5:// URL for the selected tunnel
 * @returns ordered route list ready to drop into `ClientConfig.routes`
 *
 * An empty/missing `serverUrl` still returns a shaped array so callers (and
 * tests) can inspect the structure; the daemon will reject the connect.
 *
 * Unknown profiles log a warning and fall back to the `"global"` shape.
 */
export function profileToRoutes(profile: string, serverUrl: string): RouteConfig[] {
  // "global" — single proxy route catching everything.
  if (profile === 'global') {
    return [{ via: serverUrl, match: { all: true } }];
  }

  const preset = PROFILE_TO_PRESET[profile];
  if (!preset) {
    console.warn(
      `[routes] Unknown profile "${profile}", falling back to global. `
        + `Expected one of: ${Array.from(KNOWN_PROFILES).join(', ')}`,
    );
    return [{ via: serverUrl, match: { all: true } }];
  }

  // `{cc}route` — matching preset goes direct, everything else falls through
  // to the tunnel via an empty match (engine treats empty match as fallback).
  return [
    { via: 'direct', match: { preset } },
    { via: serverUrl, match: {} },
  ];
}

/**
 * Legacy `ruleMode` toggle → profile name.
 *
 * Used by the connect flow when the user has `modeOverride === 'manual'` to
 * keep the old chnroute/global toggle working without any Center hints.
 */
export function legacyRuleModeToProfile(ruleMode: 'global' | 'chnroute'): string {
  return ruleMode === 'global' ? 'global' : 'cnroute';
}
