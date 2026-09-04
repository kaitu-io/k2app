/**
 * Overleap home page render guard.
 * - real message files (a key-echo stub would pass vacuously);
 * - zero kaitu words, zero China-market payment channels;
 * - zero raw i18n keys (the "key added at the wrong nesting level" trap
 *   documented in web/CLAUDE.md is only visible when the real tree renders);
 * - the kaitu build still renders the original home (no regression).
 */
import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
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
/** Real translator with ICU-style {brand} interpolation. Missing key → key. */
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
vi.mock('../src/app/[locale]/HomeClient', () => ({ default: () => null }));

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const CN_PAYMENT = /Alipay|WeChat|UnionPay|支付宝|微信/;

afterEach(() => vi.unstubAllEnvs());

async function renderHome(locale: 'en-US' | 'ja' | 'zh-CN'): Promise<string> {
  vi.resetModules();
  const { default: HomePage } = await import('../src/app/[locale]/page');
  const element = await HomePage({ params: Promise.resolve({ locale }) });
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>{element}</NextIntlClientProvider>,
  );
  return container.innerHTML.replaceAll('github.com/getoverleap', '');
}

describe('overleap home (NEXT_PUBLIC_BRAND=overleap)', () => {
  for (const locale of ['en-US', 'ja'] as const) {
    it(`${locale}: renders the landing sections with real copy`, async () => {
      vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
      const html = await renderHome(locale);
      expect(html).toContain('id="hero"');
      expect(html).toContain('id="features"');
      expect(html).toContain('id="pricing"');
      expect(html).toContain('id="faq"');
      expect(html).toContain('$79');
      expect(html).toContain('$8.99');
      expect(html).toContain('Overleap');
    });

    it(`${locale}: zero kaitu words, zero China-market payment channels, zero raw keys`, async () => {
      vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
      const html = await renderHome(locale);
      // Liveness: an async-component element renders to '' in jsdom, which
      // would make every "not.toMatch" below pass vacuously.
      expect(html.length).toBeGreaterThan(2000);
      expect(html).not.toMatch(KAITU_WORDS);
      expect(html).not.toMatch(CN_PAYMENT);
      expect(html).not.toMatch(/landing\.[a-z]+\./i);
      expect(html).not.toContain('{brand}');
    });
  }

  it('metadata title carries the brand and no k2cc suffix', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { generateMetadata } = await import('../src/app/[locale]/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-US' }) });
    expect(String(meta.title)).toMatch(/\| Overleap$/);
    expect(String(meta.title)).not.toMatch(/k2cc/);
    expect(JSON.stringify(meta)).not.toMatch(KAITU_WORDS);
  });
});

describe('kaitu home is untouched (NEXT_PUBLIC_BRAND=kaitu)', () => {
  it('zh-CN still renders the original hero (no pricing section, no landing copy)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const html = await renderHome('zh-CN');
    expect(html).toContain('id="hero"');
    expect(html).not.toContain('id="pricing"');
    expect(html).not.toContain('$79');
  });
});
