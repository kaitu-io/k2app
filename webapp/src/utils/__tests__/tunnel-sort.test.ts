/**
 * Tunnel Sorting Tests
 *
 * Sort by route quality (primary), then country alphabetical (secondary).
 */
import { describe, it, expect } from 'vitest';
import { sortTunnelsByRecommendation, type RouteQualityProvider } from '../tunnel-sort';
import type { Tunnel } from '../../services/api-types';

describe('sortTunnelsByRecommendation', () => {
  const createTunnel = (domain: string, country: string): Tunnel => ({
    id: 1,
    name: domain,
    domain: domain,
    protocol: 'k2v4',
    port: 443,
    node: {
      name: domain,
      country: country,
      region: 'asia',
      ipv4: '1.2.3.4',
      ipv6: '',
      isAlive: true,
      load: 0,
      trafficUsagePercent: 0,
      bandwidthUsagePercent: 0,
    },
  });

  const createQualityProvider = (qualityMap: Map<string, number>): RouteQualityProvider => ({
    getRouteQuality: (domain: string) => qualityMap.get(domain.toLowerCase()) ?? 0,
  });

  it('should sort tunnels by quality (higher first)', () => {
    const tunnels = [
      createTunnel('low.example.com', 'JP'),
      createTunnel('high.example.com', 'JP'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['low.example.com', 2],
      ['high.example.com', 5],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);
    expect(sorted[0].domain).toBe('high.example.com');
    expect(sorted[1].domain).toBe('low.example.com');
  });

  it('should use country as secondary sort when quality is equal', () => {
    const tunnels = [
      createTunnel('us.example.com', 'US'),
      createTunnel('jp.example.com', 'JP'),
      createTunnel('sg.example.com', 'SG'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['us.example.com', 4],
      ['jp.example.com', 4],
      ['sg.example.com', 4],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);
    // Same quality → country alphabetical: JP, SG, US
    expect(sorted[0].node.country).toBe('JP');
    expect(sorted[1].node.country).toBe('SG');
    expect(sorted[2].node.country).toBe('US');
  });

  it('should return empty array for empty input', () => {
    const qualityProvider = createQualityProvider(new Map());
    const sorted = sortTunnelsByRecommendation([], qualityProvider);
    expect(sorted).toEqual([]);
  });

  it('should not mutate the original array', () => {
    const tunnels = [
      createTunnel('b.example.com', 'US'),
      createTunnel('a.example.com', 'JP'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['a.example.com', 5],
      ['b.example.com', 3],
    ]));

    const originalOrder = tunnels.map(t => t.domain);
    sortTunnelsByRecommendation(tunnels, qualityProvider);
    expect(tunnels.map(t => t.domain)).toEqual(originalOrder);
  });
});
