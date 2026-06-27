import { describe, it, expect } from 'vitest';
import { parseServerUrl, nodeEntriesFromTunnels } from '../node-descriptor';
import type { Tunnel } from '../api-types';

function tunnel(partial: Partial<Tunnel>): Tunnel {
  return {
    id: 1, domain: 'd', name: 'n', protocol: 'k2v5', port: 443,
    node: { name: 'n', country: 'jp', region: 'jp', ipv4: '', ipv6: '', isAlive: true, load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore: 0.5,
    ...partial,
  } as Tunnel;
}

describe('parseServerUrl', () => {
  it('extracts ip/pin/ech from a k2v5 url', () => {
    const got = parseServerUrl('k2v5://jp.example.com:443?ech=AEX&pin=sha256:BB&ip=1.2.3.4');
    expect(got).toEqual({ ip: '1.2.3.4', pin: 'sha256:BB', ech: 'AEX' });
  });

  it('returns undefined fields when absent', () => {
    const got = parseServerUrl('k2v5://jp.example.com:443?ech=AEX');
    expect(got.ech).toBe('AEX');
    expect(got.pin).toBeUndefined();
    expect(got.ip).toBeUndefined();
  });

  it('returns empty object on malformed url', () => {
    expect(parseServerUrl('not a url')).toEqual({});
  });
});

describe('nodeEntriesFromTunnels', () => {
  it('builds entries, using node.ipv4 when url has no ip param', () => {
    const items = [
      tunnel({ serverUrl: 'k2v5://a:443?ech=E1&pin=sha256:P1', node: { ...tunnel({}).node, ipv4: '9.9.9.9' } }),
    ];
    expect(nodeEntriesFromTunnels(items)).toEqual([{ ip: '9.9.9.9', pin: 'sha256:P1', ech: 'E1' }]);
  });

  it('skips tunnels missing pin or ech or ip', () => {
    const items = [
      tunnel({ serverUrl: 'k2v5://a:443?ech=E1', node: { ...tunnel({}).node, ipv4: '9.9.9.9' } }), // no pin
      tunnel({ serverUrl: 'k2v5://b:443?pin=sha256:P', node: { ...tunnel({}).node, ipv4: '8.8.8.8' } }), // no ech
      tunnel({ serverUrl: 'k2v5://c:443?ech=E&pin=sha256:P', node: { ...tunnel({}).node, ipv4: '' } }), // no ip anywhere
      tunnel({ serverUrl: undefined }), // no serverUrl
    ];
    expect(nodeEntriesFromTunnels(items)).toEqual([]);
  });

  it('dedups by ip (first wins)', () => {
    const items = [
      tunnel({ serverUrl: 'k2v5://a:443?ech=E1&pin=sha256:P1&ip=1.1.1.1' }),
      tunnel({ serverUrl: 'k2v5://b:443?ech=E2&pin=sha256:P2&ip=1.1.1.1' }),
    ];
    expect(nodeEntriesFromTunnels(items)).toEqual([{ ip: '1.1.1.1', pin: 'sha256:P1', ech: 'E1' }]);
  });
});
