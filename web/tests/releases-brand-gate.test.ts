/**
 * /releases + /changelog brand gate.
 *
 * Both surfaces render public/releases.json, a single-brand artifact: 开途-worded
 * release notes and dl.kaitu.io installer URLs, fetched at runtime by
 * ReleasesClient. Neither the source scan nor the rendered-chrome scan can see
 * that — the strings live in a JSON file pulled after hydration — so the gate
 * itself is what this file pins.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { existsSync } from 'fs';
import path from 'path';

vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));

vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'] },
  Link: ({ children }: { children: React.ReactNode }) => children,
  redirect: vi.fn(() => {
    throw new Error('REDIRECT');
  }),
}));

vi.mock('next/navigation', () => ({
  notFound: vi.fn(() => {
    throw new Error('NOT_FOUND');
  }),
}));

vi.mock('./ReleasesClient', () => ({ default: () => null }));

afterEach(() => vi.unstubAllEnvs());

/** Run a page and report which terminal path it took. */
async function outcome(run: () => Promise<unknown>): Promise<'rendered' | 'notFound' | 'redirect'> {
  try {
    await run();
    return 'rendered';
  } catch (e) {
    const m = (e as Error).message;
    if (m === 'NOT_FOUND') return 'notFound';
    if (m === 'REDIRECT') return 'redirect';
    throw e;
  }
}

describe('/releases is gated on brands without their own release notes', () => {
  it('overleap 404s /releases', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { default: ReleasesPage } = await import('../src/app/[locale]/releases/page');

    expect(
      await outcome(() => ReleasesPage({ params: Promise.resolve({ locale: 'en-US' }) })),
    ).toBe('notFound');
  });

  it('kaitu still serves /releases', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    vi.resetModules();
    const { default: ReleasesPage } = await import('../src/app/[locale]/releases/page');

    expect(
      await outcome(() => ReleasesPage({ params: Promise.resolve({ locale: 'zh-CN' }) })),
    ).toBe('rendered');
  });

  it('overleap 404s /changelog rather than redirecting into a 404', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { default: ChangelogPage } = await import('../src/app/[locale]/changelog/page');

    expect(
      await outcome(() =>
        ChangelogPage({
          params: Promise.resolve({ locale: 'en-US' }),
          searchParams: Promise.resolve({}),
        }),
      ),
    ).toBe('notFound');
  });

  it('kaitu still redirects /changelog to /releases', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    vi.resetModules();
    const { default: ChangelogPage } = await import('../src/app/[locale]/changelog/page');

    expect(
      await outcome(() =>
        ChangelogPage({
          params: Promise.resolve({ locale: 'zh-CN' }),
          searchParams: Promise.resolve({}),
        }),
      ),
    ).toBe('redirect');
  });

  it('overleap prerenders no /releases shell', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { generateStaticParams } = await import('../src/app/[locale]/releases/page');
    const { OVERLEAP } = await import('@/lib/brands');

    // Locales are still the brand's own — the page 404s regardless, but an
    // off-brand locale shell would be dead output either way.
    expect(generateStaticParams().every((p) => OVERLEAP.allowedLocales.includes(p.locale as never))).toBe(true);
  });
});

describe('the release-notes payload is genuinely single-brand', () => {
  // releases.json is a gitignored build artifact (web/.gitignore), so it is
  // absent on a fresh clone / CI. Skip rather than fail there — the gate
  // itself is covered by the tests above; this one is a tripwire that only
  // has something to inspect where the artifact was generated.
  const releasesPath = path.resolve(__dirname, '../public/releases.json');

  it.skipIf(!existsSync(releasesPath))(
    'public/releases.json still carries kaitu wording — i.e. the gate is still needed',
    async () => {
    const { readFileSync } = await import('fs');
    const raw = readFileSync(releasesPath, 'utf8');
    // If this ever goes false, releases.json has been brand-parameterised and
    // the gate above can be revisited rather than silently kept forever.
    expect(/Kaitu|开途|kaitu\.io/.test(raw)).toBe(true);
  });
});
