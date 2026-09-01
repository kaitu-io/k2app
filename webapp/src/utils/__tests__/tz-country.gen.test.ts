/**
 * Drift guard for the committed timezone → country artifact.
 *
 * Re-derives the table from the `countries-and-timezones` devDependency (the
 * same derivation the generator uses — scripts/tz-country-data.mjs) and
 * asserts the committed tz-country.gen.ts matches exactly. Upgrading the
 * dependency without regenerating turns this red:
 *
 *   node scripts/generate-tz-country.mjs
 */
import { describe, it, expect } from 'vitest';
// eslint-disable-next-line import/no-relative-packages
import { deriveTzCountryRows } from '../../../scripts/tz-country-data.mjs';
import { TZ_COUNTRY } from '../tz-country.gen';

describe('tz-country.gen.ts artifact', () => {
  it('matches a fresh derivation from countries-and-timezones', () => {
    const derived = Object.fromEntries(deriveTzCountryRows());
    expect(TZ_COUNTRY).toEqual(derived);
  });

  it('covers the deprecated aliases OS settings still report', () => {
    // Sampled, not exhaustive — the exhaustive check is the derivation above.
    expect(TZ_COUNTRY['Asia/Saigon']).toBe('vn');
    expect(TZ_COUNTRY['Asia/Rangoon']).toBe('mm');
    expect(TZ_COUNTRY['Asia/Chongqing']).toBe('cn');
    expect(TZ_COUNTRY['PRC']).toBe('cn');
    expect(TZ_COUNTRY['Iran']).toBe('ir');
    expect(TZ_COUNTRY['W-SU']).toBe('ru');
  });
});
