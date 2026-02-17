/**
 * Tunnel Sorting Tests
 *
 * Tests for sorting tunnels by recommendation (star rating).
 * Higher quality tunnels should appear first, with load as secondary sort.
 */
import { describe, it, expect } from 'vitest';
import { sortTunnelsByRecommendation, type RouteQualityProvider } from '../tunnel-sort';
import type { Tunnel } from '../../services/api-types';

describe('sortTunnelsByRecommendation', () => {
  const createTunnel = (domain: string, load: number): Tunnel => ({
    id: 1,
    name: domain,
    domain: domain,
    protocol: 'k2v4',
    port: 443,
    url: `k2wss://${domain}`,
    node: {
      name: domain,
      country: 'JP',
      region: 'asia',
      ipv4: '1.2.3.4',
      ipv6: '',
      is_alive: true,
      load: load,
    },
  });

  // Helper to create a RouteQualityProvider from a Map
  const createQualityProvider = (qualityMap: Map<string, number>): RouteQualityProvider => ({
    getRouteQuality: (domain: string) => qualityMap.get(domain.toLowerCase()) ?? 0,
  });

  it('should sort tunnels by quality (higher first)', () => {
    const tunnels = [
      createTunnel('low-quality.example.com', 30),
      createTunnel('high-quality.example.com', 30),
      createTunnel('medium-quality.example.com', 30),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['low-quality.example.com', 2],
      ['high-quality.example.com', 5],
      ['medium-quality.example.com', 3],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);

    expect(sorted[0].domain).toBe('high-quality.example.com');
    expect(sorted[1].domain).toBe('medium-quality.example.com');
    expect(sorted[2].domain).toBe('low-quality.example.com');
  });

  it('should use load as secondary sort when quality is equal', () => {
    const tunnels = [
      createTunnel('high-load.example.com', 80),
      createTunnel('low-load.example.com', 20),
      createTunnel('medium-load.example.com', 50),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['high-load.example.com', 4],
      ['low-load.example.com', 4],
      ['medium-load.example.com', 4],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);

    // Same quality, should sort by load (lower first)
    expect(sorted[0].domain).toBe('low-load.example.com');
    expect(sorted[1].domain).toBe('medium-load.example.com');
    expect(sorted[2].domain).toBe('high-load.example.com');
  });

  it('should handle tunnels without evaluation results', () => {
    const tunnels = [
      createTunnel('no-evaluation.example.com', 30),
      createTunnel('with-evaluation.example.com', 30),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['with-evaluation.example.com', 5],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);

    // Tunnel with evaluation should come first (quality 5 > quality 0)
    expect(sorted[0].domain).toBe('with-evaluation.example.com');
    expect(sorted[1].domain).toBe('no-evaluation.example.com');
  });

  it('should return empty array for empty input', () => {
    const qualityProvider = createQualityProvider(new Map());
    const sorted = sortTunnelsByRecommendation([], qualityProvider);
    expect(sorted).toEqual([]);
  });

  it('should not mutate the original array', () => {
    const tunnels = [
      createTunnel('b.example.com', 50),
      createTunnel('a.example.com', 50),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['a.example.com', 5],
      ['b.example.com', 3],
    ]));

    const originalOrder = tunnels.map(t => t.domain);
    sortTunnelsByRecommendation(tunnels, qualityProvider);

    // Original array should be unchanged
    expect(tunnels.map(t => t.domain)).toEqual(originalOrder);
  });
});
