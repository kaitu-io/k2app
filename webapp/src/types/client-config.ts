/**
 * ClientConfig - VPN connection configuration
 *
 * Matches Go's config.ClientConfig JSON tags (snake_case).
 * Assembled from defaults + user preferences + server URL at connect time,
 * then passed to _k2.run('up', config).
 *
 * Wire-contract mirror of Go `k2/config/config.go ClientConfig`. UI-only
 * state (e.g. the chnroute/global toggle) lives in config.store and does
 * NOT belong here.
 */

/**
 * Preset names exposed by the Go rule engine. Keep in sync with
 * `k2/rule/target.go presets`. Each `{cc}-access` entry expands to a
 * combined geoip+domain ruleset for that country.
 */
export type PresetName =
  | 'overseas'
  | 'cn-access'
  | 'ir-access'
  | 'ru-access'
  | 'tr-access'
  | 'pk-access'
  | 'vn-access'
  | 'mm-access'
  | 'eg-access'
  | 'id-access'
  | 'sa-access'
  | 'ae-access'
  | 'th-access'
  | 'bd-access'
  | 'by-access';

export interface MatchConfig {
  // Bundle-based rules
  preset?: PresetName;
  names?: string[];
  exclude?: string[];

  // Inline host matching
  domain_suffix?: string[];
  ip_cidr?: string[];

  // Connection metadata
  process_name?: string[];
  package_name?: string[];
  // v2 (app-bypass): prefix matching + Android installer source.
  process_name_prefix?: string[];
  package_name_prefix?: string[];
  installer_package?: string[];
  network?: 'tcp' | 'udp';
  ip_is_private?: boolean;

  // Catch-all
  all?: boolean;
}

export interface RouteConfig {
  via: string;
  match: MatchConfig;
}

/**
 * Rule-miss telemetry (Phase 1 — "dark instrumentation").
 *
 * Mirrors Go `config.RuleMissConfig`. Zero value means telemetry OFF —
 * the engine skips reporter construction entirely and the match hot
 * path has zero overhead. Phase 2 adds an opt-in UI toggle.
 */
export interface RuleMissTelemetryConfig {
  enabled?: boolean;
  endpoint?: string;
  country?: string;
  client_version?: string;
}

export interface TelemetryConfig {
  rule_miss?: RuleMissTelemetryConfig;
}

/**
 * App Bypass v2 payload — opt-in smart routing of per-app traffic to direct.
 *
 * Mirrors Go `config.AppBypassConfig`. When `region` is set, the engine
 * loads `app-bypass-<region>.yaml` from its rule cache and merges the
 * preset's process / package / installer signals with the user-added
 * `process_adds` / `package_adds` overrides. When `region` is empty (or
 * the preset is missing) the engine still produces direct routes from
 * the custom adds alone, so user-managed bypass entries work in any
 * routing mode.
 */
export interface AppBypassConfig {
  region?: string;
  process_adds?: string[];
  package_adds?: string[];
}

export interface ClientConfig {
  mode?: 'tun' | 'proxy';
  routes?: RouteConfig[];
  tun?: { ipv4?: string; ipv6?: string };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
  telemetry?: TelemetryConfig;
  app_bypass?: AppBypassConfig;
}

export const CLIENT_CONFIG_DEFAULTS: ClientConfig = {
  mode: 'tun',
  log: { level: 'info' },
};
