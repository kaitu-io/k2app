/**
 * Rendered-output brand-leak guard — the counterpart to brand-guard.test.ts.
 *
 * brand-guard scans source/message FILES; this one scans what actually reaches
 * the DOM for the shared chrome (Header + Footer) under each baked brand.
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

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, user: null, logout: vi.fn() }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
}));

import Footer from '@/components/Footer';
import Header from '@/components/Header';
import { BrandProvider } from '@/components/providers/BrandProvider';
import { siteBrand } from '@/lib/brands';

function loadMessages(locale: string) {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(
    fs.readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => [
      f.replace('.json', ''),
      JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')),
    ]),
  );
}

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const OVERLEAP_WORDS = /Overleap|overleap\.io/;

afterEach(() => vi.unstubAllEnvs());

function renderChrome(locale: string) {
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>
      <BrandProvider brand={siteBrand()}>
        <Header />
        <Footer />
      </BrandProvider>
    </NextIntlClientProvider>,
  );
  // Strip the allow-listed protocol-layer org link before asserting.
  return container.innerHTML.replaceAll('github.com/getoverleap', '');
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
