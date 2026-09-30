import { describe, it, expect } from 'vitest';
import { safeNext } from '@/lib/safe-next';

describe('safeNext', () => {
  it.each([
    [null, '/account'],
    ['/account/security', '/account/security'],
    ['https://evil.example', '/account'],
    ['//evil.example', '/account'],
    ['/\\evil.example', '/account'],
    ['account', '/account'],
  ])('%s → %s', (raw, expected) => {
    expect(safeNext(raw)).toBe(expected);
  });
});
