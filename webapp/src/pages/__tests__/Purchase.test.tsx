import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

/**
 * Regression guard for the 2026-04-20 tier-rename refactor: proxy-purchase UI
 * was deleted from Purchase.tsx. A source-level check is more reliable than
 * a render test for guarding against a deletion — render tests can silently
 * mask missing features if mocked stores default to empty state, while a
 * forbidden-string check fails the moment someone re-introduces the symbol.
 *
 * Spec: docs/superpowers/specs/2026-04-20-proxy-purchase-users.md (Task 22).
 */
describe('Purchase page: proxy-purchase removal regression', () => {
  const source = readFileSync(
    resolve(__dirname, '..', 'Purchase.tsx'),
    'utf8',
  );

  it('no MemberSelection import or usage', () => {
    expect(source).not.toMatch(/MemberSelection/);
  });

  it('no selectedForMyself state', () => {
    expect(source).not.toMatch(/selectedForMyself/);
  });

  it('no selectedMemberUUIDs state', () => {
    expect(source).not.toMatch(/selectedMemberUUIDs/);
  });

  it('request payload does not send forUsers or forUserUUIDs', () => {
    expect(source).not.toMatch(/forUserUUIDs\s*:/);
    expect(source).not.toMatch(/forUsers\s*:/);
  });
});
