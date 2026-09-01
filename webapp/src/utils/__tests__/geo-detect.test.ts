import { describe, it, expect, vi, afterEach } from 'vitest';
import { getSystemTimeZone, countryFromTimeZone, detectCountry } from '../geo-detect';
import { isRoutableCountry, routableCountry } from '../routes';
import { SUPPORTED_COUNTRY_CODES } from '../countries';
import { TZ_COUNTRY } from '../tz-country.gen';

afterEach(() => vi.restoreAllMocks());

function stubTimeZone(tz: string | undefined) {
  return vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
    .mockReturnValue({ timeZone: tz } as Intl.ResolvedDateTimeFormatOptions);
}

describe('countryFromTimeZone', () => {
  it('maps canonical zones for every supported routing country', () => {
    // Every whitelisted country must be reachable from at least one zone —
    // a country whose zones all went missing would silently never detect.
    for (const cc of SUPPORTED_COUNTRY_CODES) {
      const zones = Object.entries(TZ_COUNTRY).filter(([, c]) => c === cc);
      expect(zones.length, `no timezone maps to ${cc}`).toBeGreaterThan(0);
      expect(countryFromTimeZone(zones[0][0])).toBe(cc);
    }
  });

  it('maps deprecated aliases', () => {
    expect(countryFromTimeZone('Asia/Saigon')).toBe('vn');
    expect(countryFromTimeZone('Turkey')).toBe('tr');
  });

  it('returns null for unknown or missing input', () => {
    expect(countryFromTimeZone('Not/AZone')).toBeNull();
    expect(countryFromTimeZone('')).toBeNull();
    expect(countryFromTimeZone(null)).toBeNull();
    expect(countryFromTimeZone(undefined)).toBeNull();
  });
});

describe('getSystemTimeZone / detectCountry', () => {
  it('reads the system timezone through Intl', () => {
    stubTimeZone('Asia/Tehran');
    expect(getSystemTimeZone()).toBe('Asia/Tehran');
    expect(detectCountry()).toBe('ir');
  });

  it('survives a runtime without timezone support', () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions')
      .mockImplementation(() => { throw new Error('no ICU'); });
    expect(getSystemTimeZone()).toBeNull();
    expect(detectCountry()).toBeNull();
  });

  it('returns null when Intl reports no timezone', () => {
    stubTimeZone(undefined);
    expect(detectCountry()).toBeNull();
  });
});

describe('routableCountry clamp', () => {
  it('passes every supported country through unchanged', () => {
    for (const cc of SUPPORTED_COUNTRY_CODES) {
      expect(isRoutableCountry(cc)).toBe(true);
      expect(routableCountry(cc)).toBe(cc);
      expect(routableCountry(cc.toUpperCase())).toBe(cc);
    }
  });

  it('clamps bundle-less countries and empty input to cn', () => {
    expect(routableCountry('jp')).toBe('cn');
    expect(routableCountry('us')).toBe('cn');
    expect(routableCountry(null)).toBe('cn');
    expect(routableCountry(undefined)).toBe('cn');
    expect(isRoutableCountry('jp')).toBe(false);
  });
});
