/**
 * Header / Footer 只能链到本品牌构建里存在的页面。
 *
 * 结构来自 lib/site/<brand>.ts（spec 2026-09-04-overleap-site-decoupling §2）。2026-09-04 前
 * 页脚靠 `brand.features.x && <li>` 逐项开关，overleap 站曾同时链着 /routers /changelog
 * /releases（该构建 notFound）、空的 /guides 和不存在的 /#testimonials 锚点。现在两端的
 * 断言都是对配置的回归锚：开途一条不少，Overleap 一条不多。
 *
 * 文案 key 是否真的存在由 tests/site-config-keys.test.ts 静态核对（本文件的 next-intl 被
 * src/test/setup.ts 全局 mock 成回显 key，无法在这里看文案）。
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

async function renderChrome(component: 'Footer' | 'Header', locale: string): Promise<string> {
  vi.resetModules();
  const { default: Component } = await import(`../src/components/${component}`);
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>
      <Component />
    </NextIntlClientProvider>,
  );
  return container.innerHTML;
}

const KAITU_ONLY_HREFS = ['href="/routers"', 'href="/changelog"', 'href="/releases"', 'href="/retailer/rules"', 'href="/guides"'];
const KAITU_FOOTER = [...KAITU_ONLY_HREFS, 'href="/install"', 'href="/k2"', 'href="/k2/quickstart"', 'href="/support"', 'href="/privacy"', 'href="/terms"', 'href="https://github.com/getoverleap"'];
const OVERLEAP_FOOTER = ['href="/install"', 'href="/purchase"', 'href="/support"', 'href="/k2"', 'href="/k2/quickstart"', 'href="https://github.com/getoverleap"', 'href="/privacy"', 'href="/terms"', 'href="mailto:support@overleap.io"'];

describe('footer links only this brand\'s pages', () => {
  it('overleap: every configured link, none of the kaitu-only ones', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const html = await renderChrome('Footer', 'en-GB');
    for (const href of OVERLEAP_FOOTER) expect(html, href).toContain(href);
    for (const href of KAITU_ONLY_HREFS) expect(html, href).not.toContain(href);
  });

  it('kaitu: every link the pre-config footer had (no regression)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const html = await renderChrome('Footer', 'zh-CN');
    for (const href of KAITU_FOOTER) expect(html, href).toContain(href);
    expect(html).not.toContain('mailto:');
  });
});

describe('header links only this brand\'s pages', () => {
  it('overleap: Why / Pricing / Help direct links, Download CTA, no dead anchors', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const html = await renderChrome('Header', 'en-GB');
    for (const href of ['href="/#features"', 'href="/#pricing"', 'href="/support"', 'href="/install"']) {
      expect(html, href).toContain(href);
    }
    expect(html).not.toContain('/#testimonials');
  });

  it('kaitu: Why dropdown + Pricing + Free Download (no regression)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const html = await renderChrome('Header', 'zh-CN');
    expect(html).toContain('href="/purchase"');
    expect(html).toContain('href="/install"');
    expect(html).toContain('<button'); // the dropdown trigger
  });
});
