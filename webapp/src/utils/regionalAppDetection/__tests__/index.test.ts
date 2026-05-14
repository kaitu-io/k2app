import { describe, it, expect } from 'vitest';
import { getRegionalDetector, chinaDetector, noopDetector } from '..';

describe('getRegionalDetector', () => {
  it('returns chinaDetector for "cn"', () => {
    expect(getRegionalDetector('cn')).toBe(chinaDetector);
  });

  it('is case-insensitive (CN, Cn → chinaDetector)', () => {
    expect(getRegionalDetector('CN')).toBe(chinaDetector);
    expect(getRegionalDetector('Cn')).toBe(chinaDetector);
  });

  it('returns noopDetector for unregistered country codes', () => {
    expect(getRegionalDetector('us')).toBe(noopDetector);
    expect(getRegionalDetector('ru')).toBe(noopDetector);
    expect(getRegionalDetector('jp')).toBe(noopDetector);
  });

  it('returns noopDetector for null/undefined/empty', () => {
    expect(getRegionalDetector(null)).toBe(noopDetector);
    expect(getRegionalDetector(undefined)).toBe(noopDetector);
    expect(getRegionalDetector('')).toBe(noopDetector);
  });
});
