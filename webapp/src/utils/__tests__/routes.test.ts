/**
 * Unit tests for profileToRoutes — verifies that every known profile name
 * produces the correct routes shape, and that unknown profiles fall back to
 * global with a warning.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

import {
  profileToRoutes,
  legacyRuleModeToProfile,
  KNOWN_PROFILES,
  PROFILE_TO_PRESET,
} from '../routes';

const SERVER_URL = 'k2v5://uid:tok@host.example:443?foo=bar';

describe('profileToRoutes', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('global → single all-match proxy route', () => {
    const routes = profileToRoutes('global', SERVER_URL);
    expect(routes).toEqual([
      { via: SERVER_URL, match: { all: true } },
    ]);
  });

  // Exhaustive check of every known country profile.
  const cases: Array<[profile: string, preset: string]> = [
    ['cnroute', 'cn-access'],
    ['iroute', 'ir-access'],
    ['ruroute', 'ru-access'],
    ['troute', 'tr-access'],
    ['pkroute', 'pk-access'],
    ['vnroute', 'vn-access'],
    ['mmroute', 'mm-access'],
    ['egroute', 'eg-access'],
    ['idroute', 'id-access'],
    ['saroute', 'sa-access'],
    ['aeroute', 'ae-access'],
    ['throute', 'th-access'],
    ['bdroute', 'bd-access'],
    ['byroute', 'by-access'],
  ];

  it.each(cases)('%s → direct preset %s then fallback proxy', (profile, preset) => {
    const routes = profileToRoutes(profile, SERVER_URL);
    expect(routes).toEqual([
      { via: 'direct', match: { preset } },
      { via: SERVER_URL, match: {} },
    ]);
  });

  it('covers all 14 country profiles (no drift with PROFILE_TO_PRESET)', () => {
    const profilesInCases = new Set(cases.map(([p]) => p));
    const profilesInMap = new Set(Object.keys(PROFILE_TO_PRESET));
    expect(profilesInCases).toEqual(profilesInMap);
  });

  it('unknown profile falls back to global and warns', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const routes = profileToRoutes('zzroute', SERVER_URL);
    expect(routes).toEqual([{ via: SERVER_URL, match: { all: true } }]);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn.mock.calls[0][0]).toMatch(/Unknown profile "zzroute"/);
  });

  it('empty string profile falls back to global and warns', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const routes = profileToRoutes('', SERVER_URL);
    expect(routes).toEqual([{ via: SERVER_URL, match: { all: true } }]);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it('direct sentinel is spelled exactly "direct"', () => {
    const routes = profileToRoutes('cnroute', SERVER_URL);
    expect(routes[0].via).toBe('direct');
  });

  it('KNOWN_PROFILES includes all country profiles plus global', () => {
    expect(KNOWN_PROFILES.has('global')).toBe(true);
    for (const profile of Object.keys(PROFILE_TO_PRESET)) {
      expect(KNOWN_PROFILES.has(profile)).toBe(true);
    }
    expect(KNOWN_PROFILES.size).toBe(15); // 14 countries + global
  });
});

describe('legacyRuleModeToProfile', () => {
  it("maps 'global' → 'global'", () => {
    expect(legacyRuleModeToProfile('global')).toBe('global');
  });

  it("maps 'chnroute' → 'cnroute'", () => {
    expect(legacyRuleModeToProfile('chnroute')).toBe('cnroute');
  });
});
