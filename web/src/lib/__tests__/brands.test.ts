import { describe, it, expect } from 'vitest';
import { brandFromHost, brandById, KAITU, OVERLEAP } from '../brands';

describe('brandFromHost', () => {
  it('maps kaitu.io to KAITU', () => {
    expect(brandFromHost('kaitu.io')).toBe(KAITU);
  });

  it('maps www.kaitu.io to KAITU', () => {
    expect(brandFromHost('www.kaitu.io')).toBe(KAITU);
  });

  it('maps overleap.io to OVERLEAP', () => {
    expect(brandFromHost('overleap.io')).toBe(OVERLEAP);
  });

  it('maps www.overleap.io to OVERLEAP', () => {
    expect(brandFromHost('www.overleap.io')).toBe(OVERLEAP);
  });

  it('strips port when matching', () => {
    expect(brandFromHost('overleap.io:3000')).toBe(OVERLEAP);
    expect(brandFromHost('kaitu.io:8080')).toBe(KAITU);
  });

  it('is case-insensitive', () => {
    expect(brandFromHost('OverLeap.IO')).toBe(OVERLEAP);
    expect(brandFromHost('KAITU.IO')).toBe(KAITU);
  });

  it('falls back to KAITU on null/undefined host', () => {
    expect(brandFromHost(undefined)).toBe(KAITU);
    expect(brandFromHost(null)).toBe(KAITU);
    expect(brandFromHost('')).toBe(KAITU);
  });

  it('falls back to KAITU on unknown host', () => {
    expect(brandFromHost('random.example.com')).toBe(KAITU);
    expect(brandFromHost('localhost')).toBe(KAITU);
    expect(brandFromHost('preview-abc.amplifyapp.com')).toBe(KAITU);
  });
});

describe('brandById', () => {
  it('returns KAITU for "kaitu"', () => {
    expect(brandById('kaitu')).toBe(KAITU);
  });

  it('returns OVERLEAP for "overleap"', () => {
    expect(brandById('overleap')).toBe(OVERLEAP);
  });
});

describe('brand configs', () => {
  it('KAITU supports all 7 locales', () => {
    expect(KAITU.allowedLocales.length).toBe(7);
    expect(KAITU.defaultLocale).toBe('zh-CN');
    expect(KAITU.taglineZh).toBe('愿上帝为你开路');
  });

  it('OVERLEAP is English-only', () => {
    expect(OVERLEAP.allowedLocales).toEqual(['en-US', 'en-GB', 'en-AU']);
    expect(OVERLEAP.defaultLocale).toBe('en-US');
    expect(OVERLEAP.taglineZh).toBeUndefined();
  });

  it('brand base URLs are distinct HTTPS origins', () => {
    expect(KAITU.baseUrl).toBe('https://kaitu.io');
    expect(OVERLEAP.baseUrl).toBe('https://overleap.io');
  });

  it('brand ids are distinct', () => {
    expect(KAITU.id).toBe('kaitu');
    expect(OVERLEAP.id).toBe('overleap');
    expect(KAITU.id).not.toBe(OVERLEAP.id);
  });
});
