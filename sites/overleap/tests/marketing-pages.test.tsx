/**
 * Marketing page render guard (home, /pricing, /support, /install).
 *
 * Real message files through next-intl's real ICU translator (createTranslator)
 * with onError → throw, so a missing key, a wrong nesting level or an unfilled
 * {placeholder} fails the render instead of printing a fallback. A key-echo stub
 * would make every assertion below pass vacuously.
 *
 * Every page × every locale: real copy renders, zero raw keys, zero unfilled
 * placeholders, zero other-brand words / China-market payment channels.
 */
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { NextIntlClientProvider, createTranslator } from 'next-intl';
import fs from 'fs';
import path from 'path';
import { LOCALES, type Locale } from '@/lib/site';
import { NAMESPACES } from '../messages/namespaces';
import type { AllDownloadLinks } from '@/lib/downloads';

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(
    NAMESPACES.map((ns) => [ns, JSON.parse(fs.readFileSync(path.join(dir, `${ns}.json`), 'utf8'))]),
  );
}

vi.mock('next-intl/server', () => ({
  getTranslations: async (opts: { locale: string; namespace?: string }) =>
    createTranslator({
      locale: opts.locale as never,
      messages: loadMessages(opts.locale),
      namespace: opts.namespace as never,
      onError: (e) => {
        throw e;
      },
    }),
  setRequestLocale: vi.fn(),
}));
vi.mock('next/navigation', () => ({
  notFound: () => {
    throw new Error('NEXT_NOT_FOUND');
  },
}));
vi.mock('@/i18n/routing', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, href, ...p }: any) => {
    const h = typeof href === 'string' ? href : `${href.pathname}?${new URLSearchParams(href.query)}`;
    return <a href={h} {...p}>{children}</a>;
  },
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));
// Header (client, auth-aware) and Footer (async server component) are covered by
// site-chrome.test.tsx; jsdom cannot render an async component inside a tree.
vi.mock('@/components/Header', () => ({ default: () => <header data-testid="header" /> }));
vi.mock('@/components/Footer', () => ({ default: () => <footer data-testid="footer" /> }));
const downloadsMock = vi.hoisted(() => ({ fetchAllDownloadLinks: vi.fn() }));
vi.mock('@/lib/downloads', async (orig) => ({ ...(await orig<object>()), ...downloadsMock }));

// Built from parts so the strings do not appear in this file as-is.
const OTHER_BRAND = new RegExp([['Kai', 'tu'], ['开', '途'], ['開', '途'], ['kai', 'tu\\.(io|me)']].map((p) => p.join('')).join('|'));
const CN_PAYMENT = /Alipay|WeChat|UnionPay|支付宝|微信|银联/;
const RAW_KEY = /\b(landing|download|help|pricing|purchase|nav|common)\.[a-zA-Z]+\.?/;
const PLACEHOLDER = /\{[a-zA-Z]+\}/;

const NO_LINKS: AllDownloadLinks = { desktop: { beta: null, stable: null }, mobile: null };

beforeEach(() => {
  downloadsMock.fetchAllDownloadLinks.mockResolvedValue(NO_LINKS);
});

type PageModule = { default: (p: { params: Promise<{ locale: string }> }) => Promise<React.ReactElement> };
const PAGES: Record<string, () => Promise<PageModule>> = {
  home: () => import('../src/app/[locale]/page'),
  pricing: () => import('../src/app/[locale]/pricing/page'),
  support: () => import('../src/app/[locale]/support/page'),
  install: () => import('../src/app/[locale]/install/page'),
};

async function renderPage(page: keyof typeof PAGES, locale: Locale): Promise<string> {
  const { default: Page } = await PAGES[page]();
  const element = await Page({ params: Promise.resolve({ locale }) });
  const { container, unmount } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)} onError={(e) => { throw e; }}>
      {element}
    </NextIntlClientProvider>,
  );
  const html = container.innerHTML;
  unmount();
  return html;
}

function expectClean(html: string, label: string) {
  // Liveness: an empty render would make every not.toMatch below pass vacuously.
  expect(html.length, label).toBeGreaterThan(2000);
  expect(html, label).toContain('Overleap');
  expect(html, label).not.toMatch(OTHER_BRAND);
  expect(html, label).not.toMatch(CN_PAYMENT);
  expect(html, label).not.toMatch(RAW_KEY);
  expect(html, label).not.toMatch(PLACEHOLDER);
}

describe.each(Object.keys(PAGES) as (keyof typeof PAGES)[])('%s page', (page) => {
  it.each(LOCALES)('%s renders cleanly', async (locale) => {
    expectClean(await renderPage(page, locale), `${page}/${locale}`);
  });

  it('a non-locale segment 404s before touching Intl', async () => {
    const mod = (await PAGES[page]()) as PageModule & { generateMetadata: (p: { params: Promise<{ locale: string }> }) => Promise<unknown> };
    for (const locale of ['wp-login.php', '.env', 'zh-CN']) {
      await expect(mod.default({ params: Promise.resolve({ locale }) }), locale).rejects.toThrow('NEXT_NOT_FOUND');
      await expect(mod.generateMetadata({ params: Promise.resolve({ locale }) }), locale).rejects.toThrow('NEXT_NOT_FOUND');
    }
  });
});

describe('home', () => {
  const PRICE_ANCHORS: Partial<Record<Locale, string[]>> = {
    'en-GB': ['£79', '£9.99', '£6.58'],
    'en-US': ['$79', '$11.99', '$6.58'],
    ja: ['$79', '$11.99'],
  };
  it.each(Object.keys(PRICE_ANCHORS) as Locale[])('%s: all sections, real prices, JSON-LD', async (locale) => {
    const html = await renderPage('home', locale);
    for (const id of ['hero', 'steps', 'features', 'pricing', 'faq', 'download']) expect(html).toContain(`id="${id}"`);
    for (const anchor of PRICE_ANCHORS[locale]!) expect(html, anchor).toContain(anchor);
    for (const type of ['SoftwareApplication', 'Organization', 'FAQPage']) expect(html).toContain(`"@type":"${type}"`);
    expect(html).toContain('href="/purchase?plan=overleap-basic-1y"');
    expect(html).toContain('href="/purchase?plan=overleap-basic-1m"');
    expect(html).toContain('href="/install"');
    if (locale !== 'ja') expect(html).toContain('Your browsing is your business.');
  });

  it('FAQ answers are in the server HTML, not behind client JS', async () => {
    const html = await renderPage('home', 'en-GB');
    expect(html).toContain("We don't record the sites you visit");
    expect(html).toContain('£79 a year, or £9.99 a month');
  });

  it('metadata: brand-suffixed title, canonical + hreflang from the public host', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-US' }) });
    expect(String(meta.title)).toBe('Private VPN with no logs, fast on any network | Overleap');
    expect(meta.alternates?.canonical).toBe('https://overleap.io/en-US');
    expect(String(meta.description)).not.toMatch(PLACEHOLDER);
  });
});

describe('/pricing', () => {
  it('en-GB: two cards with real prices, money FAQs, FAQPage + Offer JSON-LD', async () => {
    const html = await renderPage('pricing', 'en-GB');
    expect(html).toContain('id="pricing"');
    expect(html).toContain('id="faq"');
    for (const anchor of ['£79', '£9.99', '£6.58']) expect(html).toContain(anchor);
    expect(html).toContain('"@type":"FAQPage"');
    expect(html).toContain('"@type":"Offer"');
    expect(html).toContain('"url":"https://overleap.io/en-GB/purchase"');
    expect(html).toContain('Simple, honest pricing');
  });

  it('metadata carries the brand and both prices', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/pricing/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-GB' }) });
    expect(String(meta.title)).toBe('Pricing | Overleap');
    expect(String(meta.description)).toContain('£79');
    expect(String(meta.description)).toContain('£9.99');
  });
});

describe('/support', () => {
  it('en-GB: sections, FAQ JSON-LD, support address, account + install links', async () => {
    const html = await renderPage('support', 'en-GB');
    for (const id of ['getting-started', 'billing', 'faq', 'contact']) expect(html).toContain(`id="${id}"`);
    expect(html).toContain('href="mailto:support@overleap.io"');
    expect(html).toContain('support@overleap.io with what you were trying to do');
    expect(html).toContain('"@type":"FAQPage"');
    expect(html).toContain('href="/account"');
    expect(html).toContain('href="/install"');
    expect(html).toContain('How do I manage or cancel my subscription?');
    expect(html).toContain('£79');
  });

  it('metadata', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/support/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-GB' }) });
    expect(String(meta.title)).toBe('Help | Overleap');
  });
});

describe('/install', () => {
  it('nothing published → four "coming soon" cards, no null-versioned link', async () => {
    const html = await renderPage('install', 'en-GB');
    for (const p of ['windows', 'macos', 'ios', 'android']) expect(html).toContain(`data-testid="install-card-${p}"`);
    expect(html.match(/data-available="false"/g)).toHaveLength(4);
    expect(html).not.toContain('_null_');
  });

  it('links published desktop artifacts from the CDN and keeps unpublished stores as coming soon', async () => {
    const { desktopLinks } = await vi.importActual<typeof import('@/lib/downloads')>('@/lib/downloads');
    downloadsMock.fetchAllDownloadLinks.mockResolvedValue({
      desktop: { beta: null, stable: { version: '0.4.10', links: desktopLinks('0.4.10') } },
      mobile: null,
    });
    const html = await renderPage('install', 'en-US');
    expect(html).toContain('href="https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/0.4.10/Overleap_0.4.10_x64.exe"');
    expect(html).toContain('href="https://d13jc1jqzlg4yt.cloudfront.net/overleap/desktop/0.4.10/Overleap_0.4.10_universal.pkg"');
    expect(html.match(/data-available="true"/g)).toHaveLength(2);
    expect(html.match(/data-available="false"/g)).toHaveLength(2);
    expect(html).toContain('"softwareVersion":"0.4.10"');
  });

  it('metadata', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/install/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-GB' }) });
    expect(String(meta.title)).toBe('Download Overleap for Windows, macOS, iOS and Android | Overleap');
    expect((meta.alternates?.languages as Record<string, string>)['x-default']).toBe('https://overleap.io/en-GB/install');
  });
});
