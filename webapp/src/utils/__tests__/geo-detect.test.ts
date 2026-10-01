import { describe, it, expect, vi, afterEach } from 'vitest';
import { getSystemTimeZone, countryFromTimeZone, detectCountry } from '../geo-detect';
import { isRoutableCountry, routableCountry, ROUTABLE_COUNTRY_CODES } from '../routes';
import { brandConfig } from '../../brands';
import { KAITU_BRAND } from '../../brands/kaitu';
import { OVERLEAP_BRAND } from '../../brands/overleap';
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

  it('clamps bundle-less countries and empty input to the brand default', () => {
    const fallback = brandConfig.defaultRoutingCountry;
    expect(routableCountry('jp')).toBe(fallback);
    expect(routableCountry('us')).toBe(fallback);
    expect(routableCountry(null)).toBe(fallback);
    expect(routableCountry(undefined)).toBe(fallback);
    expect(isRoutableCountry('jp')).toBe(false);
  });

  // 兜底国家按品牌分叉，是最容易改错的地方：开途必须永远是 cn（面向中国市场，
  // cn 规则包内置），另一个品牌是 gb。用字面量钉死两边，不随构建品牌变化。
  it('pins each brand\'s default routing country', () => {
    expect(KAITU_BRAND.defaultRoutingCountry).toBe('cn');
    expect(OVERLEAP_BRAND.defaultRoutingCountry).toBe('gb');
  });

  it('every brand default is itself routable (else the clamp emits a bundle-less region)', () => {
    expect(isRoutableCountry(KAITU_BRAND.defaultRoutingCountry)).toBe(true);
    expect(isRoutableCountry(OVERLEAP_BRAND.defaultRoutingCountry)).toBe(true);
  });

  it('gb is routable via its region bundle, and a detected gb is kept as gb', () => {
    expect(isRoutableCountry('gb')).toBe(true);
    expect(routableCountry('GB')).toBe('gb');
    expect(routableCountry(countryFromTimeZone('Europe/London'))).toBe('gb');
  });

  it('the picker list and the routable set are the same countries', () => {
    expect([...SUPPORTED_COUNTRY_CODES].sort()).toEqual([...ROUTABLE_COUNTRY_CODES].sort());
  });
});
