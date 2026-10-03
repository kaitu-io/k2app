/**
 * /releases renders; /changelog is a compat redirect to it.
 */
import { describe, it, expect, vi } from 'vitest';

vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));

vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'] },
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

describe('/releases + /changelog', () => {
  it('serves /releases', async () => {
    vi.resetModules();
    const { default: ReleasesPage } = await import('../src/app/[locale]/releases/page');

    expect(
      await outcome(() => ReleasesPage({ params: Promise.resolve({ locale: 'zh-CN' }) })),
    ).toBe('rendered');
  });

  it('redirects /changelog to /releases', async () => {
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

  it('prerenders exactly the served locales', async () => {
    vi.resetModules();
    const { generateStaticParams } = await import('../src/app/[locale]/releases/page');
    const { KAITU } = await import('@/lib/brands');

    expect(generateStaticParams().map((p) => p.locale)).toEqual([...KAITU.allowedLocales]);
  });
});
