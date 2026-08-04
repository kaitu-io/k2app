import { describe, it, expect } from 'vitest';
import type { MatchConfig, PresetName, RouteConfig } from '../client-config';

// Type enforcement via tsc-checked contract file: see client-config.contract.ts
describe('MatchConfig wire contract', () => {
  it('accepts destination-port specs as strings', () => {
    const m: MatchConfig = { network: 'udp', port: ['27015', '27000-28000'] };
    expect(m.port).toHaveLength(2);
  });

  it('accepts sniffed-protocol specs', () => {
    const m: MatchConfig = { protocol: ['stun', 'quic'] };
    expect(m.protocol).toContain('stun');
  });

  it('accepts the games preset', () => {
    const p: PresetName = 'games';
    const route: RouteConfig = { via: 'direct', match: { preset: p } };
    expect(route.match.preset).toBe('games');
  });

  it('accepts the newly published country presets', () => {
    const presets: PresetName[] = ['tm-access', 'kz-access', 'uz-access'];
    expect(presets).toHaveLength(3);
  });
});
