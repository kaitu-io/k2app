/**
 * ClientConfig - VPN connection configuration
 *
 * Matches Go's config.ClientConfig JSON tags (snake_case).
 * Assembled from defaults + user preferences + server URL,
 * then passed to _k2.run('up', config).
 */

export interface ClientConfig {
  server?: string;
  mode?: 'tun' | 'proxy';
  rule?: { global?: boolean };
  dns_mode?: 'fake-ip' | 'real-ip';
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
}

export const CLIENT_CONFIG_DEFAULTS: ClientConfig = {
  mode: 'tun',
  rule: { global: false },
  dns_mode: 'fake-ip',
  log: { level: 'info' },
};
