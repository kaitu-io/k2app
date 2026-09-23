/**
 * Router Edition Website — Task 4 SSR + content guards.
 *
 * /routers is the router-edition product page (the self-supplied hardware
 * tutorial has been removed). These tests pin:
 *   1. The page is an async Server Component with generateMetadata wired to
 *      the edition.product i18n keys, and nothing on it links to the removed
 *      tutorial route.
 *   2. routers.json carries no stale admin-panel / port-9000 copy
 *      (spec 2026-09-16-router-edition-web-onboarding-design.md).
 *   3. EDITION_PRICE_CENTS matches the plan prices in
 *      docs/router-edition-prod-deploy.md (single source of truth for price).
 */
import { describe, it, expect, vi } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

// Mock next-intl/server (Server Component API) — echoes the key so assertions
// can pin exactly which translation key a page reaches for.
vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));

// Mock next-intl (Client Component API — used by child components)
vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));

// Mock @/i18n/routing
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'],
  },
  Link: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));

describe('routers/page.kaitu (edition product page)', () => {
  it('default export is an async function returning a Promise', async () => {
    const { default: RoutersEditionPage } = await import('../src/app/[locale]/routers/page.kaitu');

    expect(RoutersEditionPage).toBeTypeOf('function');
    const result = RoutersEditionPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
    const element = await result;
    expect(element).not.toBeNull();
  });

  it('generateMetadata returns edition.product.metaTitle / metaDescription', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/routers/page.kaitu');

    const metadata = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(metadata.title).toBe('edition.product.metaTitle');
    expect(metadata.description).toBe('edition.product.metaDescription');
  });
});

describe('removed self-supplied tutorial stays removed', () => {
  const ROUTERS_DIR = path.resolve(__dirname, '../src/app/[locale]/routers');

  function sourceFiles(dir: string): string[] {
    return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) return sourceFiles(full);
      return /\.tsx?$/.test(entry.name) ? [full] : [];
    });
  }

  it('the /routers/diy route directory no longer exists', () => {
    expect(fs.existsSync(path.join(ROUTERS_DIR, 'diy'))).toBe(false);
  });

  it('no product page source links to /routers/diy', () => {
    const files = sourceFiles(ROUTERS_DIR);
    expect(files.length).toBeGreaterThan(0);
    const hits = files.filter((f) => fs.readFileSync(f, 'utf8').includes('/routers/diy'));
    expect(hits).toEqual([]);
  });
});

describe('routers.json content guards — no stale admin-panel / port-9000 copy', () => {
  const MESSAGES_DIR = path.resolve(__dirname, '../messages');
  // 后半段：已下架的「官方代刷」预售卡与浏览器设置流程的残留文案（终审 M-1）；
  // 无凭证的 `| sudo sh` 安装命令在无面板构建里装不上任何线路。
  const FORBIDDEN = [
    '9000', '智能选服', '智慧選服', 'admin 面板',
    '代刷', 'K2 Mini', 'K2-001', '页面底部', '頁面底部', '图形界面', '圖形介面', '| sudo sh',
  ];

  it.each(['zh-CN', 'zh-TW', 'zh-HK'])('%s/routers.json contains none of the forbidden phrases', (locale) => {
    const raw = fs.readFileSync(path.join(MESSAGES_DIR, locale, 'routers.json'), 'utf8');
    const hits = FORBIDDEN.filter((phrase) => raw.includes(phrase));
    expect(hits).toEqual([]);
  });
});

describe('EDITION_PRICE_CENTS', () => {
  it('matches the router-std-1y / router-svc-1y prices in the prod deploy doc', async () => {
    const { EDITION_PRICE_CENTS } = await import('../src/lib/router-edition');

    expect(EDITION_PRICE_CENTS.firstYear).toBe(39900);
    expect(EDITION_PRICE_CENTS.renewal).toBe(29900);

    const sql = fs.readFileSync(
      path.resolve(__dirname, '../../docs/router-edition-prod-deploy.md'),
      'utf8',
    );
    const stdMatch = sql.match(/'router-std-1y'[^\n]*?,\s*(\d+)\s*,\s*\d+\s*,/);
    const svcMatch = sql.match(/'router-svc-1y'[^\n]*?,\s*(\d+)\s*,\s*\d+\s*,/);
    expect(stdMatch).not.toBeNull();
    expect(svcMatch).not.toBeNull();
    expect(Number(stdMatch![1])).toBe(EDITION_PRICE_CENTS.firstYear);
    expect(Number(svcMatch![1])).toBe(EDITION_PRICE_CENTS.renewal);
  });
});
