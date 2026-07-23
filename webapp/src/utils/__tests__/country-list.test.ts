import { describe, it, expect } from 'vitest';
import { buildCountryList } from '../country-list';
import type { Tunnel } from '../../services/api-types';

function tunnel(id: number, country: string): Tunnel {
  return {
    id,
    domain: `t${id}.example.com`,
    name: `t${id}`,
    protocol: 'k2v5',
    port: 443,
    serverUrl: `k2v5://t${id}`,
    node: { name: '', country, region: '', ipv4: '', ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore: 0.5,
  } as Tunnel;
}

describe('buildCountryList', () => {
  it('returns empty for empty input', () => {
    expect(buildCountryList([])).toEqual([]);
  });

  it('lowercases codes and counts per country, sorted by count desc', () => {
    const t = [tunnel(1, 'JP'), tunnel(2, 'JP'), tunnel(3, 'HK'), tunnel(4, 'JP'), tunnel(5, 'HK'), tunnel(6, 'SG')];
    expect(buildCountryList(t)).toEqual([
      { code: 'jp', count: 3 },
      { code: 'hk', count: 2 },
      { code: 'sg', count: 1 },
    ]);
  });

  it('skips tunnels with empty country', () => {
    const t = [tunnel(1, ''), tunnel(2, 'JP')];
    expect(buildCountryList(t)).toEqual([{ code: 'jp', count: 1 }]);
  });
});
