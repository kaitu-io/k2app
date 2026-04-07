/**
 * ClientConfig - VPN connection configuration
 *
 * Matches Go's config.ClientConfig JSON tags (snake_case).
 * Assembled from defaults + user preferences + server URL,
 * then passed to _k2.run('up', config).
 */

export interface RouteConfig {
  via: string;
  match: {
    all?: boolean;
    domain_suffix?: string[];
    ip_cidr?: string[];
    [key: string]: any;
  };
}

export interface ClientConfig {
  server?: string;
  mode?: 'tun' | 'proxy';
  routes?: RouteConfig[];
  tun?: { ipv4?: string; ipv6?: string };
  rule?: {
    global?: boolean;
    rule_url?: string;
    geoip_url?: string;
    antiporn?: boolean;
    porn_url?: string;
    cache_dir?: string;
  };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
}

export const CLIENT_CONFIG_DEFAULTS: ClientConfig = {
  mode: 'tun',
  rule: { global: false },
  log: { level: 'info' },
};
