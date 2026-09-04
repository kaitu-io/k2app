/**
 * Rendered-output brand-leak guard — the counterpart to brand-guard.test.ts.
 *
 * brand-guard scans source/message FILES; this one scans what actually reaches
 * the user under each baked brand: page metadata (canonical / hreflang / og /
 * siteName / icons) and rendered DOM.
 *
 * Why both halves exist: the two Critical brand leaks found in review were
 * invisible to a file scan. /support's kaitu canonical came from a default
 * parameter (identifiers in source, no literal to grep), and SupportClient's
 * contact address was assembled from string fragments that no regex could
 * match. Only running the code shows them.
 *
 * Legal-signature exception (decision 2026-07-15): BOTH deployments sign legal
 * documents as "Overleap LLC" — root CLAUDE.md permits exactly one cross-brand
 * appearance, 法务文书署名. The kaitu assertions therefore strip that signature
 * before scanning, and a dedicated test pins that it is actually rendered, so
 * the exception stays scoped to the legal line instead of silently widening.
 */
import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render } from '@testing-library/react';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';

const ALL_LOCALES = ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'] as const;
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
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
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

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const OVERLEAP_WORDS = /Overleap|overleap\.io/;

afterEach(() => vi.unstubAllEnvs());

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

describe('rendered chrome carries only its own brand', () => {
  it('overleap build (en-US): zero kaitu words in Header+Footer', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect(renderChrome('en-US')).not.toMatch(KAITU_WORDS);
  });

  it('overleap build renders the Overleap LLC legal signature', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect(renderChrome('en-US')).toContain('Overleap LLC');
  });

  it('kaitu build (zh-CN) renders the Overleap LLC legal signature (the one approved cross-brand appearance)', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    expect(renderChrome('zh-CN')).toContain('Overleap LLC');
  });

  it('kaitu build (zh-CN): zero overleap words outside the legal signature', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    // Remove ONLY the legal signature; any other Overleap mention must fail.
    const html = renderChrome('zh-CN').replaceAll('Overleap LLC', '');
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
// 页面树按品牌编译（next.config pageExtensions）：开途独有页是 page.kaitu.tsx，Overleap 构建里
// 根本不存在；两品牌各自分裂的页（首页/购买/账户/安装/帮助）各有 page.<brand>.tsx。
// 所以 metadata 守卫按品牌各扫各的页面树；共用页（privacy/terms）两边都扫。
const METADATA_ROUTES_OVERLEAP: Array<{ route: string; mod: string }> = [
  { route: '/', mod: '../src/app/[locale]/page.overleap' },
  { route: '/privacy', mod: '../src/app/[locale]/privacy/page' },
  { route: '/purchase', mod: '../src/app/[locale]/purchase/page.overleap' },
  { route: '/terms', mod: '../src/app/[locale]/terms/page' },
];
const METADATA_ROUTES_KAITU: Array<{ route: string; mod: string }> = [
  { route: '/', mod: '../src/app/[locale]/page.kaitu' },
  { route: '/discovery', mod: '../src/app/[locale]/discovery/page.kaitu' },
  { route: '/opensource', mod: '../src/app/[locale]/opensource/page.kaitu' },
  { route: '/privacy', mod: '../src/app/[locale]/privacy/page' },
  { route: '/purchase', mod: '../src/app/[locale]/purchase/page.kaitu' },
  { route: '/releases', mod: '../src/app/[locale]/releases/page.kaitu' },
  { route: '/routers', mod: '../src/app/[locale]/routers/page.kaitu' },
  { route: '/support', mod: '../src/app/[locale]/support/page.kaitu' },
  { route: '/terms', mod: '../src/app/[locale]/terms/page' },
];

describe('page metadata carries only its own brand', () => {
  it.each(METADATA_ROUTES_OVERLEAP)('overleap build: $route metadata has zero kaitu words', async ({ mod }) => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { generateMetadata } = await import(mod);

    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'en-US' }) });

    expect(JSON.stringify(meta)).not.toMatch(KAITU_WORDS);
  });

  it.each(METADATA_ROUTES_KAITU)('kaitu build: $route metadata has zero overleap words', async ({ mod }) => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    vi.resetModules();
    const { generateMetadata } = await import(mod);

    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(JSON.stringify(meta)).not.toMatch(OVERLEAP_WORDS);
  });
});

// ─── Rendered pages ───────────────────────────────────────────────────────────

describe('rendered pages carry only their own brand (overleap build)', () => {
  // 开途独有页（opensource/routers/releases/家长指南 support）在 Overleap 构建里不存在——
  // 这是 tests/brand-page-tree.test.ts 守的结构事实，不再在这里渲染"被门挡住"的开途页。
  it('the home page renders zero kaitu words on overleap', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { default: HomePage } = await import('../src/app/[locale]/page.overleap');

    const html = await renderPage(HomePage, 'en-US');

    expect(html).not.toBeNull();
    expect(html).not.toMatch(KAITU_WORDS);
  });
});
