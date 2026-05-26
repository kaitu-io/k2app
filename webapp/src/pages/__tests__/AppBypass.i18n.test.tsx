/**
 * AppBypass i18n regression guard.
 *
 * The v2 redesign (2026-05-25) added 4 new keys to the AppBypass page but the
 * locale catalogue was not updated, so users saw raw "dashboard:appBypass.*"
 * strings on the page. This test fails fast if any of the 7 locales drops
 * one of the keys again.
 */
import { describe, it, expect } from 'vitest';
import zhCN from '../../i18n/locales/zh-CN/dashboard.json';
import enUS from '../../i18n/locales/en-US/dashboard.json';
import ja from '../../i18n/locales/ja/dashboard.json';
import zhTW from '../../i18n/locales/zh-TW/dashboard.json';
import zhHK from '../../i18n/locales/zh-HK/dashboard.json';
import enAU from '../../i18n/locales/en-AU/dashboard.json';
import enGB from '../../i18n/locales/en-GB/dashboard.json';

const locales = { zhCN, enUS, ja, zhTW, zhHK, enAU, enGB } as const;

const REQUIRED_KEYS: Array<[string, (ab: any) => unknown]> = [
  ['rescanRefreshed', (ab) => ab.rescanRefreshed],
  ['smartStatus.enabled', (ab) => ab.smartStatus?.enabled],
  ['smartStatus.disabled', (ab) => ab.smartStatus?.disabled],
  ['ruleCard.manualSummary', (ab) => ab.ruleCard?.manualSummary],
];

describe('AppBypass i18n catalogue', () => {
  for (const [name, doc] of Object.entries(locales)) {
    describe(name, () => {
      const ab = (doc as any).appBypass;
      it('has appBypass namespace', () => {
        expect(ab).toBeTruthy();
      });
      for (const [key, get] of REQUIRED_KEYS) {
        it(`has appBypass.${key}`, () => {
          const v = get(ab);
          expect(typeof v).toBe('string');
          expect(v).not.toBe('');
        });
      }
    });
  }

  it('smartStatus.enabled has {{region}} placeholder', () => {
    for (const [name, doc] of Object.entries(locales)) {
      const v = (doc as any).appBypass.smartStatus.enabled as string;
      expect(v, `${name} missing {{region}}`).toContain('{{region}}');
    }
  });

  it('ruleCard.manualSummary has {{count}} placeholder', () => {
    for (const [name, doc] of Object.entries(locales)) {
      const v = (doc as any).appBypass.ruleCard.manualSummary as string;
      expect(v, `${name} missing {{count}}`).toContain('{{count}}');
    }
  });
});
