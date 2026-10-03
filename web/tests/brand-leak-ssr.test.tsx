/**
 * Rendered-output brand-leak guard — the counterpart to brand-guard.test.ts.
 *
 * brand-guard scans source/message FILES; this one scans what actually reaches
 * the user: page metadata (canonical / hreflang / og / siteName / icons) and
 * rendered DOM. kaitu.io must never name the other brand.
 *
 * Why both halves exist: the two Critical brand leaks found in review were
 * invisible to a file scan. /support's kaitu canonical came from a default
 * parameter (identifiers in source, no literal to grep), and SupportClient's
 * contact address was assembled from string fragments that no regex could
 * match. Only running the code shows them.
 *
 * Legal-signature exception (decision 2026-07-15): legal documents sign as
 * "Overleap LLC" — root CLAUDE.md permits exactly one cross-brand appearance,
 * 法务文书署名. The assertions therefore strip that signature
 * before scanning, and a dedicated test pins that it is actually rendered, so
 * the exception stays scoped to the legal line instead of silently widening.
 */
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';

const ALL_LOCALES = ['zh-CN', 'zh-TW', 'zh-HK'] as const;
type Locale = (typeof ALL_LOCALES)[number];

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(
    fs.readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => [
      f.replace('.json', ''),
      JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')),
    ]),
  );
}

/** Dotted-path lookup, e.g. 'faq.items.multiDevice.question'. */
function lookup(root: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>(
    (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
    root,
  );
}

/**
 * Real translator over the real message files. Deliberately NOT a key-echoing
 * stub: a stub would render zero brand words and every assertion below would
 * pass vacuously. Missing keys fall back to the key itself (a missing
 * translation is not a brand leak, and should not mask one either).
 */
function makeTranslator(locale: string, namespace?: string) {
  const messages = loadMessages(locale);
  const root = namespace ? lookup(messages, namespace) : messages;
  const t = (key: string) => {
    const v = lookup(root, key);
    return typeof v === 'string' ? v : key;
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

// Pages reach `server-only` through @/lib/brand-server; it is a side-effect
// module that throws outside RSC. Same stub as tests/sitemap-brand.test.ts.
vi.mock('server-only', () => ({}));

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, user: null, logout: vi.fn() }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/i18n/routing', () => ({
  // Inlined, not ALL_LOCALES: vi.mock factories are hoisted above module consts.
  routing: {
    locales: ['zh-CN', 'zh-TW', 'zh-HK'],
    defaultLocale: 'zh-CN',
  },
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
  redirect: vi.fn(),
}));

vi.mock('next/navigation', () => ({
  notFound: vi.fn(() => {
    throw new Error('NOT_FOUND');
  }),
  usePathname: () => '/',
  useSearchParams: () => new URLSearchParams(),
}));

import Footer from '@/components/Footer';
import Header from '@/components/Header';

const OVERLEAP_WORDS = /Overleap|overleap\.io/;

/** Render an element tree with real messages and return its HTML. */
function renderWithIntl(node: React.ReactElement, locale: Locale): string {
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>
      {node}
    </NextIntlClientProvider>,
  );
  // Strip the allow-listed protocol-layer org link before asserting.
  return container.innerHTML.replaceAll('github.com/getoverleap', '');
}

function renderChrome(locale: Locale): string {
  return renderWithIntl(
    <>
      <Header />
      <Footer />
    </>,
    locale,
  );
}

/**
 * Render a real page Server Component under the baked brand.
 * Returns null when the page 404s — a gated surface is not a leak.
 */
async function renderPage(
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Component: (props: any) => Promise<React.ReactElement>,
  locale: Locale,
): Promise<string | null> {
  let element: React.ReactElement;
  try {
    element = await Component({ params: Promise.resolve({ locale }) });
  } catch (e) {
    if ((e as Error).message === 'NOT_FOUND') return null;
    throw e;
  }
  return renderWithIntl(element, locale);
}

describe('rendered chrome never names the other brand', () => {
  it.each(ALL_LOCALES)('%s renders the Overleap LLC legal signature (the one approved cross-brand appearance)', (locale) => {
    expect(renderChrome(locale)).toContain('Overleap LLC');
  });

  it.each(ALL_LOCALES)('%s: zero overleap words outside the legal signature', (locale) => {
    // Remove ONLY the legal signature; any other Overleap mention must fail.
    const html = renderChrome(locale).replaceAll('Overleap LLC', '');
    expect(html).not.toMatch(OVERLEAP_WORDS);
  });
});

// ─── Page metadata ────────────────────────────────────────────────────────────

/**
 * Every sitemap staticPage that exports generateMetadata.
 *
 * Metadata is where the /support leak lived: canonical, hreflang, x-default,
 * og:url, siteName and icons are all brand-scoped and none of them are visible
 * to a DOM scan.
 */
const METADATA_ROUTES: Array<{ route: string; mod: string }> = [
  { route: '/', mod: '../src/app/[locale]/page' },
  { route: '/discovery', mod: '../src/app/[locale]/discovery/page' },
  { route: '/opensource', mod: '../src/app/[locale]/opensource/page' },
  { route: '/pricing', mod: '../src/app/[locale]/pricing/page' },
  { route: '/privacy', mod: '../src/app/[locale]/privacy/page' },
  { route: '/purchase', mod: '../src/app/[locale]/purchase/page' },
  { route: '/releases', mod: '../src/app/[locale]/releases/page' },
  { route: '/routers', mod: '../src/app/[locale]/routers/page' },
  { route: '/support', mod: '../src/app/[locale]/support/page' },
  { route: '/terms', mod: '../src/app/[locale]/terms/page' },
];

describe('page metadata never names the other brand', () => {
  it.each(METADATA_ROUTES)('$route metadata has zero overleap words', async ({ mod }) => {
    vi.resetModules();
    const { generateMetadata } = await import(mod);

    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(JSON.stringify(meta)).not.toMatch(OVERLEAP_WORDS);
  });
});

// ─── Rendered pages ───────────────────────────────────────────────────────────

describe('rendered pages never name the other brand', () => {
  it('the home page renders zero overleap words outside the legal signature', async () => {
    vi.resetModules();
    const { default: HomePage } = await import('../src/app/[locale]/page');

    const html = await renderPage(HomePage, 'zh-CN');

    expect(html).not.toBeNull();
    expect(html!.replaceAll('Overleap LLC', '')).not.toMatch(OVERLEAP_WORDS);
  });
});
