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

  // Region selects a loaded *.krs bundle by basename (e.g. 'cn').
  // Engine expands it into host + meta routes at build time. Plan B's
  // single smart-bypass routing vocabulary.
  region?: string;

  // Inline host matching
  domain_suffix?: string[];
  ip_cidr?: string[];

  // Connection metadata
  process_name?: string[];
  package_name?: string[];
  // Prefix matching + Android installer source.
  process_name_prefix?: string[];
  package_name_prefix?: string[];
  installer_package?: string[];

  // Apps are platform-dispatched glob patterns for force-overrides.
  // Plan C wires the UI; Plan B reserves the wire shape.
  apps?: string[];

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

export interface ClientConfig {
  mode?: 'tun' | 'proxy';
  routes?: RouteConfig[];
  tun?: { ipv4?: string; ipv6?: string };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
  telemetry?: TelemetryConfig;
}

export const CLIENT_CONFIG_DEFAULTS: ClientConfig = {
  mode: 'tun',
  log: { level: 'info' },
};
