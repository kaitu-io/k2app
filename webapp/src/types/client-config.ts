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

export interface MatchConfig {
  // Bundle-based rules
  preset?: 'overseas' | 'cn-access';
  names?: string[];
  exclude?: string[];

  // Inline host matching
  domain_suffix?: string[];
  ip_cidr?: string[];

  // Connection metadata
  process_name?: string[];
  package_name?: string[];
  network?: 'tcp' | 'udp';
  ip_is_private?: boolean;

  // Catch-all
  all?: boolean;
}

export interface RouteConfig {
  via: string;
  match: MatchConfig;
}

export interface ClientConfig {
  mode?: 'tun' | 'proxy';
  routes?: RouteConfig[];
  tun?: { ipv4?: string; ipv6?: string };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
}

export const CLIENT_CONFIG_DEFAULTS: ClientConfig = {
  mode: 'tun',
  log: { level: 'info' },
};
