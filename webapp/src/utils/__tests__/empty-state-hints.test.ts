import { describe, it, expect } from 'vitest';
import {
  cloudNodesUnavailableHintKey,
  membershipExpiredHintKey,
} from '../empty-state-hints';
import enAU from '../../i18n/locales/en-AU/dashboard.json';
import enGB from '../../i18n/locales/en-GB/dashboard.json';
import enUS from '../../i18n/locales/en-US/dashboard.json';
import ja from '../../i18n/locales/ja/dashboard.json';
import zhCN from '../../i18n/locales/zh-CN/dashboard.json';
import zhHK from '../../i18n/locales/zh-HK/dashboard.json';
import zhTW from '../../i18n/locales/zh-TW/dashboard.json';

// All 7 locales, deliberately: routing these keys through a picker makes them
// DYNAMIC, and i18n/__tests__/static-keys.ts only scans statically-written
// `t('…')` literals (its own doc calls dynamic keys blind spot (a)). A key
// missing from one locale would render as the raw key string with nothing red.
const LOCALES = {
  'en-AU': enAU,
  'en-GB': enGB,
  'en-US': enUS,
  ja,
  'zh-CN': zhCN,
  'zh-HK': zhHK,
  'zh-TW': zhTW,
} as const;

/** 'dashboard:dashboard.foo' -> the string in the given bundle. */
function lookup(bundle: Record<string, any>, key: string): string | undefined {
  const path = key.replace(/^dashboard:/, '').split('.');
  return path.reduce<any>((acc, seg) => (acc == null ? acc : acc[seg]), bundle);
}

describe('membershipExpiredHintKey', () => {
  it('offers both escape hatches only when the build has both', () => {
    expect(membershipExpiredHintKey(true, true)).toBe('dashboard:dashboard.membershipExpiredHint');
  });

  it('drops the Self-Deployed clause on a brand without self-hosted tunnels', () => {
    expect(membershipExpiredHintKey(true, false)).toBe('dashboard:dashboard.membershipExpiredHintRenewOnly');
  });

  it('drops the renew clause when no purchase surface is registered', () => {
    expect(membershipExpiredHintKey(false, true)).toBe('dashboard:dashboard.membershipExpiredHintSelfHostedOnly');
  });

  // The regression: Play-only brand on Android. Both gates off, yet the copy
  // told the user to "switch to Self-Deployed" — a tab that build does not have.
  it('promises nothing when neither path exists in this build', () => {
    expect(membershipExpiredHintKey(false, false)).toBe('dashboard:dashboard.membershipExpiredHintNoAction');
  });
});

describe('cloudNodesUnavailableHintKey', () => {
  it('mentions Self-Deployed only where it exists', () => {
    expect(cloudNodesUnavailableHintKey(true)).toBe('dashboard:dashboard.cloudNodesUnavailableHint');
    expect(cloudNodesUnavailableHintKey(false)).toBe('dashboard:dashboard.cloudNodesUnavailableHintRetryOnly');
  });
});

describe('every key these pickers can return is translated', () => {
  const keys = [
    membershipExpiredHintKey(true, true),
    membershipExpiredHintKey(true, false),
    membershipExpiredHintKey(false, true),
    membershipExpiredHintKey(false, false),
    cloudNodesUnavailableHintKey(true),
    cloudNodesUnavailableHintKey(false),
  ];

  it.each(Object.keys(LOCALES))('%s has a non-empty string for all of them', (locale) => {
    const bundle = LOCALES[locale as keyof typeof LOCALES] as Record<string, any>;
    for (const key of keys) {
      const value = lookup(bundle, key);
      expect(value, `${locale} missing ${key}`).toBeTypeOf('string');
      expect((value as string).length, `${locale} empty ${key}`).toBeGreaterThan(0);
    }
  });

  // The self-hosted-off copy must not name the Self-Deployed tab at all —
  // that naming is the whole defect being fixed.
  it.each(Object.keys(LOCALES))('%s: the no-self-hosted copy never names the Self-Deployed tab', (locale) => {
    const bundle = LOCALES[locale as keyof typeof LOCALES] as Record<string, any>;
    const label = lookup(bundle, 'dashboard:dashboard.selfDeployed') as string;
    expect(label).toBeTypeOf('string');
    for (const key of [
      membershipExpiredHintKey(true, false),
      membershipExpiredHintKey(false, false),
      cloudNodesUnavailableHintKey(false),
    ]) {
      expect(lookup(bundle, key) as string, `${locale} ${key}`).not.toContain(label);
    }
  });
});
