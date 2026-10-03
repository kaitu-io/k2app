/**
 * Header / Footer: the primary nav, the Download CTA and the footer Product column
 * render with real copy in every locale, and every link lands on a route that exists.
 */
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { NextIntlClientProvider, createTranslator } from 'next-intl';
import fs from 'fs';
import path from 'path';
import { FOOTER, LOCALES, NAV, STATIC_ROUTES, type Locale } from '@/lib/site';
import { NAMESPACES } from '../messages/namespaces';

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(NAMESPACES.map((ns) => [ns, JSON.parse(fs.readFileSync(path.join(dir, `${ns}.json`), 'utf8'))]));
}

vi.mock('next-intl/server', () => ({
  getTranslations: async (opts: { locale: string; namespace?: string }) =>
    createTranslator({ locale: opts.locale, messages: loadMessages(opts.locale), namespace: opts.namespace as never, onError: (e) => { throw e; } }),
}));
vi.mock('@/i18n/routing', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, href, ...p }: any) => <a href={href} {...p}>{children}</a>,
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ profile: null, loading: false }) }));

import Header from '@/components/Header';
import Footer from '@/components/Footer';

const ROUTE_FILES = path.resolve(__dirname, '../src/app/[locale]');
function routeExists(href: string): boolean {
  const pathname = href.split('#')[0].split('?')[0];
  if (pathname === '/' || pathname === '') return fs.existsSync(path.join(ROUTE_FILES, 'page.tsx'));
  return fs.existsSync(path.join(ROUTE_FILES, pathname, 'page.tsx'));
}

describe('nav config', () => {
  it('every internal header / footer link points at a page that exists', () => {
    const hrefs = [...NAV.primary, NAV.cta, ...FOOTER.flatMap((c) => c.items)].filter((i) => !i.external).map((i) => i.href);
    expect(hrefs.filter((h) => !routeExists(h))).toEqual([]);
  });

  it('every sitemap route exists', () => {
    expect(STATIC_ROUTES.filter((r) => !routeExists(r || '/'))).toEqual([]);
  });

  it('/#features targets a section the home page renders', () => {
    const home = fs.readFileSync(path.resolve(__dirname, '../src/components/marketing/Features.tsx'), 'utf8');
    expect(home).toContain('id="features"');
  });
});

describe.each(LOCALES)('%s', (locale: Locale) => {
  it('header renders nav, CTA and the mobile menu', () => {
    const messages = loadMessages(locale);
    render(
      <NextIntlClientProvider locale={locale} messages={messages} onError={(e) => { throw e; }}>
        <Header />
      </NextIntlClientProvider>,
    );
    const nav = messages.nav as Record<string, string>;
    for (const href of ['/#features', '/pricing', '/support']) expect(document.querySelector(`a[href="${href}"]`)).not.toBeNull();
    expect(screen.getAllByText(nav.download)).toHaveLength(1);
    fireEvent.click(screen.getByLabelText(nav.menu));
    expect(screen.getAllByText(nav.download)).toHaveLength(2);
    expect(document.querySelector('#site-mobile-menu a[href="/install"]')).not.toBeNull();
  });

  it('footer renders the Product and Company columns', async () => {
    const element = await Footer({ locale });
    const { container } = render(element);
    const html = container.innerHTML;
    const footer = (loadMessages(locale).nav as { footer: Record<string, string> }).footer;
    expect(html).toContain(footer.product);
    expect(html).toContain(footer.company);
    for (const href of ['/install', '/pricing', '/support', '/privacy']) expect(html).toContain(`href="${href}"`);
    expect(html).not.toMatch(/\bnav\.[a-zA-Z]+/);
  });
});
