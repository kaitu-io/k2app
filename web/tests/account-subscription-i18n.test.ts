/**
 * 订阅页 i18n key 的嵌套守卫。
 *
 * 组件单测把 next-intl 整个 mock 成 `key => key`，所以那些绿灯**完全不能证明**
 * key 在 messages JSON 里真的存在于对的层级 —— 层级写错的表现是页面上直接显示
 * 原始 key 字符串，单测一路绿。这个文件补的就是那段盲区。
 *
 * key 清单从**源码里反推**，不是手抄一份：手抄的清单在有人改 key 名时会静默
 * 失效（清单还对着旧名字，新名字无人覆盖），而 review 看不出来。
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

const WEB_DIR = path.resolve(__dirname, '..');
const MESSAGES_DIR = path.join(WEB_DIR, 'messages');

const ALL_LOCALES = ['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'en-GB', 'en-AU', 'ja'] as const;

// useTranslations('account') + t('subscription.X')
const NAMESPACED_SOURCES = [
  'src/components/SubscriptionStatusCard.tsx',
  'src/app/[locale]/account/KaituAccountClient.tsx',
];

// useTranslations() 无命名空间 + t("account.subscription.X")
const BARE_SOURCES = ['src/app/[locale]/account/layout.tsx'];

function collectKeys(): string[] {
  const keys = new Set<string>();

  for (const rel of NAMESPACED_SOURCES) {
    const src = fs.readFileSync(path.join(WEB_DIR, rel), 'utf8');
    for (const m of src.matchAll(/t\(\s*['"]subscription\.([A-Za-z0-9_]+)['"]/g)) {
      keys.add(m[1]);
    }
  }
  for (const rel of BARE_SOURCES) {
    const src = fs.readFileSync(path.join(WEB_DIR, rel), 'utf8');
    for (const m of src.matchAll(/t\(\s*['"]account\.subscription\.([A-Za-z0-9_]+)['"]/g)) {
      keys.add(m[1]);
    }
  }
  return [...keys].sort();
}

function subscriptionBlock(locale: string): Record<string, unknown> {
  const raw = fs.readFileSync(path.join(MESSAGES_DIR, locale, 'account.json'), 'utf8');
  const parsed = JSON.parse(raw) as Record<string, unknown>;
  return (parsed.subscription ?? {}) as Record<string, unknown>;
}

describe('account.subscription i18n', () => {
  const used = collectKeys();

  // 判别式：正则失效 / 文件改名会让 used 变空，届时下面所有断言都恒真 ——
  // 这条让守卫失效时自己报红，而不是安静地放行。
  it('从源码里确实抽到了 key（守卫自身的活性检查）', () => {
    expect(used.length).toBeGreaterThanOrEqual(10);
    expect(used).toContain('navTitle'); // layout.tsx 侧栏首项
    expect(used).toContain('daysLeft'); // 状态卡核心信息
  });

  for (const locale of ALL_LOCALES) {
    it(`${locale}/account.json 覆盖组件实际用到的每一个 key`, () => {
      const block = subscriptionBlock(locale);
      const missing = used.filter((k) => typeof block[k] !== 'string' || block[k] === '');
      expect(missing).toEqual([]);
    });
  }

  it('各 locale 的 key 集合完全一致（漏翻 = 线上显示原始 key）', () => {
    const base = Object.keys(subscriptionBlock('zh-CN')).sort();
    for (const locale of ALL_LOCALES) {
      expect(Object.keys(subscriptionBlock(locale)).sort(), locale).toEqual(base);
    }
  });

  it('带插值的文案在每个 locale 都保留了占位符', () => {
    const interpolated: Record<string, string> = {
      activeUntil: '{date}',
      expiredOn: '{date}',
      daysLeft: '{days}',
      historyDays: '{days}',
    };
    for (const locale of ALL_LOCALES) {
      const block = subscriptionBlock(locale);
      for (const [key, placeholder] of Object.entries(interpolated)) {
        expect(String(block[key]), `${locale}/${key}`).toContain(placeholder);
      }
    }
  });
});
