/**
 * Tunnel URL 构建和解析工具
 *
 * Canonical URL Format:
 *   k2wss://domain?ipv4=IP[&ipv6=IP][&port=PORT][&hop_port_start=P1&hop_port_end=P2]
 *                 [&country=XX]#name
 *
 * Parameters:
 * - domain: Server domain (TLS SNI only)
 * - ipv4: Server IPv4 address
 * - ipv6: Server IPv6 address (optional)
 * - port: Connection port (default: 443)
 * - hop_port_start/hop_port_end: Port hopping range (optional)
 * - country: ISO 3166-1 alpha-2 country code (for flag display)
 * - #name: Display name (URL fragment, URL encoded)
 *
 * Architecture:
 * - Cloud Tunnel: No auth in URL, Service injects X-K2-Token/X-K2-UDID headers
 * - Self-hosted Tunnel: User can provide token (token@domain format)
 */

import type { Tunnel } from '../services/api-types';

/**
 * SimpleTunnel - Simplified tunnel configuration
 * All configuration is encoded in the URL
 * Note: This type was moved here from control-types.ts for local use only
 */
export interface SimpleTunnel {
  id: string;   // Tunnel ID (auto-generated or from API tunnel.id)
  name: string; // Display name
  url: string;  // Full URL: k2v4://domain?ipv4=...&country=XX#name
}

// Auth credentials (only for self-hosted tunnels, not used by Cloud Tunnel)
export interface TunnelAuth {
  token: string;
}

// Parsed URL information
export interface ParsedTunnelURL {
  domain: string;
  ipv4?: string;
  ipv6?: string;
  port: number;
  hopPortStart?: number;
  hopPortEnd?: number;
  auth?: TunnelAuth;
  name?: string;
  country?: string;
  // ECH (Encrypted Client Hello) config list for K2v4 connections
  echConfigList?: string;
  // Legacy format support
  addrs?: string[];
}

// SimpleTunnel build options
export interface BuildSimpleTunnelOptions {
  tunnel: Tunnel;
}

/**
 * Parse SimpleTunnel URL
 * Supports both canonical format (ipv4/ipv6/port) and legacy format (addrs)
 */
export function parseSimpleTunnelURL(url: string): ParsedTunnelURL {
  try {
    // Handle k2wss:// protocol
    const normalized = url.replace(/^k2wss?:\/\//, 'https://');
    const parsed = new URL(normalized);

    // Parse canonical parameters
    const ipv4 = parsed.searchParams.get('ipv4') || undefined;
    const ipv6 = parsed.searchParams.get('ipv6') || undefined;
    const portStr = parsed.searchParams.get('port');
    const port = portStr ? parseInt(portStr, 10) : 443;

    // Port hopping
    const hopStartStr = parsed.searchParams.get('hop_port_start');
    const hopEndStr = parsed.searchParams.get('hop_port_end');
    const hopPortStart = hopStartStr ? parseInt(hopStartStr, 10) : undefined;
    const hopPortEnd = hopEndStr ? parseInt(hopEndStr, 10) : undefined;

    // Other parameters
    const country = parsed.searchParams.get('country') || undefined;
    const name = parsed.hash ? decodeURIComponent(parsed.hash.slice(1)) : undefined;

    // ECH config list parameter (for K2v4 connections)
    const echConfigList = parsed.searchParams.get('ech_config') || undefined;

    // Legacy addrs parameter (backward compatibility)
    const addrsParam = parsed.searchParams.get('addrs') || '';
    const addrs = addrsParam ? addrsParam.split(',').filter(Boolean) : undefined;

    // Parse auth credentials (if present)
    let auth: TunnelAuth | undefined;
    if (parsed.username) {
      auth = {
        token: decodeURIComponent(parsed.username),
      };
    }

    return {
      domain: parsed.hostname,
      ipv4,
      ipv6,
      port,
      hopPortStart,
      hopPortEnd,
      auth,
      name,
      country,
      echConfigList,
      addrs,
    };
  } catch {
    return { domain: '', port: 443 };
  }
}

/**
 * URL build options
 */
export interface BuildURLOptions {
  domain: string;
  ipv4: string;
  ipv6?: string;
  port?: number;
  hopPortStart?: number;
  hopPortEnd?: number;
  auth?: TunnelAuth;
  name?: string;
  country?: string;
  // ECH (Encrypted Client Hello) config list for K2v4 connections - base64 encoded ECHConfigList
  echConfigList?: string;
}

/**
 * Build SimpleTunnel URL using canonical format
 * @param options URL build options
 */
export function buildSimpleTunnelURL(options: BuildURLOptions): string {
  const { domain, ipv4, ipv6, port = 443, hopPortStart, hopPortEnd, auth, name, country, echConfigList } = options;

  // Build host part (may include token)
  let host = domain;
  if (auth?.token) {
    const encodedToken = encodeURIComponent(auth.token);
    host = `${encodedToken}@${domain}`;
  }

  let url = `k2wss://${host}`;
  const params: string[] = [];

  // Required: ipv4
  params.push(`ipv4=${ipv4}`);

  // Optional: ipv6
  if (ipv6) {
    params.push(`ipv6=${ipv6}`);
  }

  // Optional: port (only if not default 443)
  if (port && port !== 443) {
    params.push(`port=${port}`);
  }

  // Optional: port hopping
  if (hopPortStart && hopPortEnd) {
    params.push(`hop_port_start=${hopPortStart}`);
    params.push(`hop_port_end=${hopPortEnd}`);
  }

  // Optional: ECH config list (base64 encoded ECHConfigList for K2v4 connections)
  if (echConfigList) {
    params.push(`ech_config=${encodeURIComponent(echConfigList)}`);
  }

  // Optional: country
  if (country) {
    params.push(`country=${encodeURIComponent(country)}`);
  }

  if (params.length > 0) {
    url += '?' + params.join('&');
  }

  // Add fragment as display name
  if (name) {
    url += '#' + encodeURIComponent(name);
  }

  return url;
}

/**
 * Build SimpleTunnel object from API Tunnel
 *
 * @param options.tunnel - API Tunnel object
 *
 * Note: Cloud Tunnel URLs don't include auth, Service handles authentication
 */
export function buildSimpleTunnel(options: BuildSimpleTunnelOptions): SimpleTunnel {
  const { tunnel } = options;

  const name = tunnel.name || tunnel.domain;
  const country = tunnel.node?.country;

  return {
    id: String(tunnel.id),
    name,
    url: buildSimpleTunnelURL({
      domain: tunnel.domain,
      ipv4: tunnel.node.ipv4,
      ipv6: tunnel.node.ipv6 || undefined,
      port: tunnel.port,
      name,
      country,
    }),
  };
}

/**
 * Extract domain from SimpleTunnel URL (for matching with API Tunnel)
 */
export function getSimpleTunnelDomain(tunnel: SimpleTunnel | undefined): string {
  if (!tunnel?.url) return '';
  return parseSimpleTunnelURL(tunnel.url).domain;
}
