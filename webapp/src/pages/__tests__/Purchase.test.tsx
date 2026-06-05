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

/**
 * iOS StoreKit IAP wiring guard. Source-level checks (consistent with the
 * existing regression style above): when `window._platform.iap` is present the
 * ENTIRE purchase screen is replaced by the inline iOS panels — IosMembershipPanel
 * (manage/status) or IosSubscribePanel (subscribe) — never the WordGate multi-plan
 * list and never a popup sheet. The WordGate `openExternal(payUrl)` path is
 * preserved only for platforms without `iap`.
 *
 * Plan: docs/superpowers/plans/2026-06-04-ios-storekit-iap.md (webapp phase).
 */
describe('Purchase page: iOS IAP capability gating', () => {
  const source = readFileSync(
    resolve(__dirname, '..', 'Purchase.tsx'),
    'utf8',
  );

  it('reads the iap capability from window._platform', () => {
    expect(source).toMatch(/window\._platform\?\.iap/);
  });

  it('replaces the whole screen with inline iOS panels gated on iap', () => {
    // A single `if (iap) { ... }` early-return owns the entire iOS path.
    expect(source).toMatch(/if\s*\(iap\)\s*\{/);
    expect(source).toMatch(/<IosSubscribePanel/);
    expect(source).toMatch(/<IosMembershipPanel/);
  });

  it('no popup sheet on iOS — IapPurchaseSheet fully removed', () => {
    expect(source).not.toMatch(/IapPurchaseSheet/);
    expect(source).not.toMatch(/iapSheetOpen/);
  });

  it('iap-absent path preserves the existing WordGate openExternal(payUrl)', () => {
    // The WordGate fallback remains for platforms without iap.
    expect(source).toMatch(/if\s*\(!preview\s*&&\s*payUrl\)/);
    expect(source).toMatch(/openExternal\?\.\(payUrl\)/);
  });

  it('passes the user appleAccountToken to the subscribe panel', () => {
    expect(source).toMatch(/accountToken=\{user\?\.appleAccountToken/);
  });
});
