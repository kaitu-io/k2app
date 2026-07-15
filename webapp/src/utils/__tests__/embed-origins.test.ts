import { describe, it, expect } from 'vitest';
import { allowedEmbedOrigins, isSafeExternalUrl } from '../embed-origins';

describe('allowedEmbedOrigins', () => {
  it('accepts the embed URL origin and its www sibling', () => {
    const origins = allowedEmbedOrigins('https://kaitu.io/discovery?embed=true');
    expect(origins.has('https://kaitu.io')).toBe(true);
    expect(origins.has('https://www.kaitu.io')).toBe(true);
    expect(origins.size).toBe(2);
  });

  it('accepts the apex sibling when embed URL uses www', () => {
    const origins = allowedEmbedOrigins('https://www.kaitu.io/releases?embed=true');
    expect(origins.has('https://www.kaitu.io')).toBe(true);
    expect(origins.has('https://kaitu.io')).toBe(true);
  });

  it('works for a non-default baseURL (brand split)', () => {
    const origins = allowedEmbedOrigins('https://overleap.net/discovery?embed=true');
    expect(origins.has('https://overleap.net')).toBe(true);
    expect(origins.has('https://www.overleap.net')).toBe(true);
    expect(origins.has('https://kaitu.io')).toBe(false);
  });

  it('returns an empty set for an invalid URL', () => {
    expect(allowedEmbedOrigins('not a url').size).toBe(0);
  });
});

describe('isSafeExternalUrl', () => {
  it('accepts http and https', () => {
    expect(isSafeExternalUrl('https://www.wsj.com')).toBe(true);
    expect(isSafeExternalUrl('http://192.168.8.1:1779')).toBe(true);
  });

  it('rejects non-http protocols', () => {
    expect(isSafeExternalUrl('javascript:alert(1)')).toBe(false);
    expect(isSafeExternalUrl('file:///etc/passwd')).toBe(false);
    expect(isSafeExternalUrl('data:text/html,x')).toBe(false);
  });

  it('rejects invalid URLs', () => {
    expect(isSafeExternalUrl('not a url')).toBe(false);
    expect(isSafeExternalUrl('')).toBe(false);
  });
});
