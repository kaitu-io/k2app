/**
 * Footer links must respect the brand feature gates. web/CLAUDE.md: "A gated
 * surface must be gated everywhere it can be reached: page, navigation tile,
 * AND sitemap." On 2026-09-04 the overleap footer still linked /routers,
 * /changelog and /releases (all notFound() on that build) and the kaitu-only
 * retailer program — dead links on the launch page.
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

afterEach(() => vi.unstubAllEnvs());

async function renderFooter(locale: 'en-US' | 'zh-CN'): Promise<string> {
  vi.resetModules();
  const { default: Footer } = await import('../src/components/Footer');
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>
      <Footer />
    </NextIntlClientProvider>,
  );
  return container.innerHTML;
}

const GATED_ON_OVERLEAP = ['href="/routers"', 'href="/changelog"', 'href="/releases"', 'href="/retailer/rules"'];

describe('footer respects brand feature gates', () => {
  it('overleap build links none of the gated kaitu-only surfaces', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const html = await renderFooter('en-US');
    expect(html).toContain('href="/install"'); // liveness: the footer really rendered
    for (const href of GATED_ON_OVERLEAP) expect(html, href).not.toContain(href);
  });

  it('kaitu build still links every one of them (no regression)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const html = await renderFooter('zh-CN');
    for (const href of GATED_ON_OVERLEAP) expect(html, href).toContain(href);
  });
});
