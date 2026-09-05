/**
 * overleap 下载页 / 帮助页渲染守卫（spec 2026-09-04-overleap-site-decoupling §3.3 / §3.4）。
 *
 * - 下载页：四张平台卡；无产物 / 未上架 → "Coming soon" 而不是 `Overleap_null_*` 死链；
 *   有产物时 URL 来自品牌 CDN 与产物前缀。
 * - 帮助页：真实文案渲染（server component），含账户/账单、FAQPage JSON-LD、联系邮箱；
 *   零 kaitu 词、零家长指南残留、零原始 key、零未填占位。
 * 与 landing-overleap-ssr 同一套真实文案 translator（key-echo stub 会让断言空绿）。
 */
import React from 'react';
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';
import { buildInstallTargets } from '../src/lib/install-targets';
import { OVERLEAP, KAITU } from '../src/lib/brands';
import type { AllDownloadLinks } from '../src/lib/downloads';

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
  const t = (key: string, values?: Record<string, string>) => {
    const v = lookup(root, key);
    if (typeof v !== 'string') return key;
    return values ? v.replace(/\{(\w+)\}/g, (_, k) => values[k] ?? `{${k}}`) : v;
  };
  t.raw = (key: string) => lookup(root, key);
  t.rich = (key: string) => t(key);
  t.markup = (key: string) => t(key);
  t.has = (key: string) => lookup(root, key) !== undefined;
  return t;
}

const downloadsMock = vi.hoisted(() => ({ fetchAllDownloadLinks: vi.fn() }));
vi.mock('@/lib/downloads', () => downloadsMock);
vi.mock('next-intl/server', () => ({
  getTranslations: async (opts: { locale: string; namespace?: string }) =>
    makeTranslator(opts.locale, opts.namespace),
  setRequestLocale: vi.fn(),
}));
vi.mock('server-only', () => ({}));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, user: null, logout: vi.fn() }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'], defaultLocale: 'zh-CN' },
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
  redirect: vi.fn(),
}));

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const NO_LINKS: AllDownloadLinks = { desktop: { beta: null, stable: null }, mobile: null };
const WITH_LINKS: AllDownloadLinks = {
  desktop: {
    beta: null,
    stable: {
      version: '0.4.10',
      links: {
        windows: { primary: 'https://cdn.example/overleap/desktop/0.4.10/Overleap_0.4.10_x64.exe', backup: '' },
        macos: { primary: 'https://cdn.example/overleap/desktop/0.4.10/Overleap_0.4.10_universal.pkg', backup: '' },
        linux: { primary: '', backup: '' },
      },
    },
  },
  mobile: null,
};

beforeEach(() => {
  vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
  vi.resetModules();
});
afterEach(() => vi.unstubAllEnvs());

async function renderPage(mod: string, locale: 'en-GB' | 'en-US' | 'ja'): Promise<string> {
  const { default: Page } = await import(mod);
  const element = await Page({ params: Promise.resolve({ locale }) });
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>{element}</NextIntlClientProvider>,
  );
  return container.innerHTML.replaceAll('github.com/getoverleap', '');
}

describe('buildInstallTargets', () => {
  it('no artifacts, no store listings → every platform is "coming soon" (empty url), never a null-versioned link', () => {
    const targets = buildInstallTargets(NO_LINKS, OVERLEAP);
    expect(targets.map((t) => t.platform)).toEqual(['windows', 'macos', 'ios', 'android']);
    expect(targets.every((t) => t.url === '')).toBe(true);
    expect(JSON.stringify(targets)).not.toContain('null');
  });

  it('desktop artifacts come from the brand CDN with the brand artifact prefix', () => {
    const targets = buildInstallTargets(WITH_LINKS, OVERLEAP);
    expect(targets.find((t) => t.platform === 'windows')).toMatchObject({ url: expect.stringContaining('Overleap_0.4.10_x64.exe'), version: '0.4.10' });
    expect(targets.find((t) => t.platform === 'macos')).toMatchObject({ url: expect.stringContaining('Overleap_0.4.10_universal.pkg') });
  });

  it('store listings win over CDN manifests; kaitu iOS points at its App Store record', () => {
    const targets = buildInstallTargets(NO_LINKS, KAITU);
    expect(targets.find((t) => t.platform === 'ios')).toMatchObject({ url: KAITU.storeLinks.ios, store: true });
    const withManifest = buildInstallTargets({ ...NO_LINKS, mobile: { ios: { url: 'https://apps.apple.com/app/id1', version: '1' }, android: { primary: 'https://cdn/a.apk', backup: '', version: '1' } } }, OVERLEAP);
    expect(withManifest.find((t) => t.platform === 'ios')?.url).toBe('https://apps.apple.com/app/id1');
    expect(withManifest.find((t) => t.platform === 'android')).toMatchObject({ url: 'https://cdn/a.apk', store: false });
  });
});

describe('overleap /install', () => {
  it('renders four platform cards, all "coming soon" when nothing is published', async () => {
    downloadsMock.fetchAllDownloadLinks.mockResolvedValue(NO_LINKS);
    const html = await renderPage('../src/app/[locale]/install/page.overleap', 'en-GB');
    for (const p of ['windows', 'macos', 'ios', 'android']) {
      expect(html).toContain(`data-testid="install-card-${p}"`);
    }
    expect(html.match(/data-available="false"/g)).toHaveLength(4);
    expect(html).not.toContain('_null_');
    expect(html).not.toMatch(KAITU_WORDS);
    expect(html).not.toMatch(/[一-鿿]/); // no Chinese on the overleap download page
  });

  it('links the published desktop artifacts and keeps unpublished stores as coming soon', async () => {
    downloadsMock.fetchAllDownloadLinks.mockResolvedValue(WITH_LINKS);
    const html = await renderPage('../src/app/[locale]/install/page.overleap', 'en-US');
    expect(html).toContain('href="https://cdn.example/overleap/desktop/0.4.10/Overleap_0.4.10_x64.exe"');
    expect(html).toContain('href="https://cdn.example/overleap/desktop/0.4.10/Overleap_0.4.10_universal.pkg"');
    expect(html.match(/data-available="true"/g)).toHaveLength(2);
    expect(html.match(/data-available="false"/g)).toHaveLength(2);
    expect(html).toContain('"softwareVersion":"0.4.10"');
  });

  it('metadata is brand-scoped', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/install/page.overleap');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-GB' }) });
    expect(String(meta.title)).toBe('Download Overleap for Windows, macOS, iOS and Android | Overleap');
    expect(JSON.stringify(meta)).not.toMatch(KAITU_WORDS);
    expect((meta.alternates?.languages as Record<string, string>)['x-default']).toBe('https://overleap.io/en-GB/install');
  });
});

describe('overleap /support (help page)', () => {
  for (const locale of ['en-GB', 'ja'] as const) {
    it(`${locale}: renders help sections with real copy, FAQ JSON-LD and the support address`, async () => {
      const html = await renderPage('../src/app/[locale]/support/page.overleap', locale);
      expect(html).toContain('id="getting-started"');
      expect(html).toContain('id="billing"');
      expect(html).toContain('id="faq"');
      expect(html).toContain('href="mailto:support@overleap.io"');
      expect(html).toContain('"@type":"FAQPage"');
      expect(html).toContain('href="/account"');
      expect(html).toContain('href="/install"');
      if (locale === 'en-GB') {
        expect(html).toContain('Getting started');
        expect(html).toContain('How do I manage or cancel my subscription?');
        expect(html).toContain('£79'); // pricing FAQ interpolated for the GB reader
      }
      expect(html).not.toMatch(KAITU_WORDS);
      expect(html).not.toMatch(/homeschool|parents|家长/i);
      expect(html).not.toMatch(/\b(help|landing)\.[a-z]+\./);
      expect(html).not.toMatch(/\{(brand|email|yearly|monthly|currency)\}/);
    });
  }

  it('metadata is the help page, not the parents guide', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/support/page.overleap');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-GB' }) });
    expect(String(meta.title)).toBe('Help | Overleap');
    expect(JSON.stringify(meta)).not.toMatch(KAITU_WORDS);
  });
});
