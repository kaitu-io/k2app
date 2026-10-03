/**
 * /pricing 的真实文案渲染守卫 + /routers 预售期 / 发售后两种日期。
 *
 * - 真实 message 文件（key 回显的替身会让"错层级 key"漏网）；
 * - 价格必须出现在服务端 HTML 里（SEO / 首屏），并且与 lib/site 的价表 / lib/router-edition 的
 *   预售常量一致；
 * - 零另一品牌的词（法务署名除外）、零原始 key、零未填占位；
 * - 预售期：$359 划线 $399 + 发货日；发售后：$399、无划线、无预售徽标。
 */
import React from 'react';
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(
    fs.readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => [
      f.replace('.json', ''),
      JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')),
    ]),
  );
}
function lookup(root: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>(
    (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
    root,
  );
}
function makeTranslator(locale: string, namespace?: string) {
  const messages = loadMessages(locale);
  const root = namespace ? lookup(messages, namespace) : messages;
  const t = (key: string, values?: Record<string, string | number>) => {
    const v = lookup(root, key);
    if (typeof v !== 'string') return key;
    return values ? v.replace(/\{(\w+)\}/g, (_, k) => String(values[k] ?? `{${k}}`)) : v;
  };
  t.raw = (key: string) => lookup(root, key);
  t.rich = (key: string) => t(key);
  t.markup = (key: string) => t(key);
  t.has = (key: string) => lookup(root, key) !== undefined;
  return t;
}

vi.mock('next-intl/server', () => ({
  getTranslations: async (opts: { locale: string; namespace?: string } | string) =>
    typeof opts === 'string' ? makeTranslator('zh-CN', opts) : makeTranslator(opts.locale, opts.namespace),
  setRequestLocale: vi.fn(),
}));
vi.mock('server-only', () => ({}));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, user: null, logout: vi.fn() }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'], defaultLocale: 'zh-CN' },
  usePathname: () => '/pricing',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
  redirect: vi.fn(),
}));
// 定价页挂载后会拉 /api/plans 覆盖快照；SSR 守卫只看服务端 HTML，把请求替身成永远 pending。
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return { ...actual, api: { ...actual.api, getPlans: () => new Promise(() => {}) } };
});

const OVERLEAP_WORDS = /Overleap|overleap\.io/;
const PRESALE_NOW = new Date('2026-10-01T12:00:00+08:00');
const AFTER_LAUNCH = new Date('2026-11-12T12:00:00+08:00');

beforeEach(() => {
  vi.resetModules();
  vi.useFakeTimers({ toFake: ['Date'] });
});
afterEach(() => {
  vi.useRealTimers();
});

type Locale = 'zh-CN' | 'zh-TW' | 'zh-HK';

async function renderPage(mod: string, locale: Locale): Promise<string> {
  const { default: Page } = await import(mod);
  const element = await Page({ params: Promise.resolve({ locale }) });
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>{element}</NextIntlClientProvider>,
  );
  return container.innerHTML.replaceAll('github.com/getoverleap', '').replaceAll('Overleap LLC', '');
}

describe('/pricing', () => {
  for (const locale of ['zh-CN', 'zh-TW', 'zh-HK'] as const) {
    it(`${locale}: App 版四档带线上快照价、路由器版预售卡、定价 FAQ；零 overleap 词、零原始 key`, async () => {
        vi.setSystemTime(PRESALE_NOW);
      const html = await renderPage('../src/app/[locale]/pricing/page', locale);
      expect(html.length).toBeGreaterThan(2000);
      // App 版：四张卡，2 年热门，价格与 lib/site 快照一致
      expect(html.match(/data-pid="/g)).toHaveLength(4);
      expect(html).toContain('data-pid="2y" data-highlight="true"');
      for (const price of ['$49', '$94.90', '$139', '$199', '$59', '$128', '$177', '$295']) expect(html, price).toContain(price);
      // 路由器版：预售价 + 划线标价 + 发货日
      expect(html).toContain('data-testid="router-plan" data-presale="true"');
      expect(html).toContain('$359');
      expect(html).toContain('<s ');
      expect(html).toContain('$399');
      expect(html).toContain('11月11日');
      expect(html).toContain('$299');
      // FAQ 结构化数据 + 五条
      expect(html).toContain('"@type":"FAQPage"');
      expect(html).toContain('href="/purchase"');
      expect(html).toContain('href="/routers"');
      // 品牌与文案完整性
      expect(html).not.toMatch(OVERLEAP_WORDS);
      expect(html).not.toMatch(/pricing\.pricing\./);
      expect(html).not.toMatch(/\{(price|years|percent|date)\}/);
    });
  }

  it('发售后：路由器版按标价 $399 出，无划线、无预售徽标', async () => {
    vi.setSystemTime(AFTER_LAUNCH);
    const html = await renderPage('../src/app/[locale]/pricing/page', 'zh-CN');
    expect(html).toContain('data-testid="router-plan" data-presale="false"');
    // 只看路由器卡：App 版卡片常年带划线原价，与预售无关。
    const routerCard = html.slice(html.indexOf('data-testid="router-plan"'));
    expect(routerCard).toContain('$399');
    expect(routerCard).not.toContain('$359');
    expect(routerCard).not.toContain('<s ');
  });

  it('metadata：标题带品牌，描述带 App 入门价与路由器版当前价', async () => {
    vi.setSystemTime(PRESALE_NOW);
    const { generateMetadata } = await import('../src/app/[locale]/pricing/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(String(meta.title)).toMatch(/定价 \| 开途/);
    expect(String(meta.description)).toContain('$49');
    expect(String(meta.description)).toContain('$359');
    expect(String(meta.description)).not.toMatch(/\{(app1y|router)\}/);
  });
});

// The purchase paths start at 看定价; a /pricing page that stays silent loses
// every visitor who reads prices there and buys later.
describe('/pricing reports pricing_view', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('exactly once on mount', async () => {
    const srcs: string[] = [];
    vi.stubGlobal('Image', function (this: object) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
    vi.setSystemTime(PRESALE_NOW);
    await renderPage('../src/app/[locale]/pricing/page', 'zh-CN');
    const views = srcs.map((u) => new URL(u, 'http://x').searchParams.get('e')).filter((e) => e === 'pricing_view');
    expect(views).toHaveLength(1);
  });
});

describe('/routers presale copy (real messages)', () => {
  it('预售期：徽标、预售 CTA、划线 $399、预售 FAQ 在最前', async () => {
    vi.setSystemTime(PRESALE_NOW);
    const html = await renderPage('../src/app/[locale]/routers/page', 'zh-CN');
    expect(html).toContain('data-testid="presale-badge"');
    expect(html).toContain('预售价 $359 立即预订');
    expect(html).toContain('data-presale="true"');
    expect(html).toContain('<s ');
    expect(html).toContain('$399');
    expect(html).toContain('预售中 · 11月11日 起发货');
    expect(html.indexOf('预售什么时候发货')).toBeLessThan(html.indexOf('路由器从哪里发货'));
    expect(html).toContain('服务期从什么时候开始算');
    expect(html).not.toMatch(/\{(price|date)\}/);
  });

  it('发售后：无徽标、CTA 回到「立即购买」、$399 无划线、预售 FAQ 消失', async () => {
    vi.setSystemTime(AFTER_LAUNCH);
    const html = await renderPage('../src/app/[locale]/routers/page', 'zh-CN');
    expect(html).not.toContain('data-testid="presale-badge"');
    expect(html).toContain('立即购买');
    expect(html).toContain('data-presale="false"');
    expect(html).not.toContain('<s ');
    expect(html).not.toContain('$359');
    expect(html).not.toContain('预售什么时候发货');
    expect(html).toContain('服务期从什么时候开始算');
  });
});
