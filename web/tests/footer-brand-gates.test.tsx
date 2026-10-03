/**
 * Header / Footer 只能链到存在的页面。
 *
 * 结构来自 lib/site/kaitu.ts。断言是对配置的回归锚：一条不少，已下线 / 纯跳转的路径一条不多。
 *
 * 文案 key 是否真的存在由 tests/site-config-keys.test.ts 静态核对（本文件的 next-intl 被
 * src/test/setup.ts 全局 mock 成回显 key，无法在这里看文案）。
 */
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
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
  routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'], defaultLocale: 'zh-CN' },
  usePathname: () => '/',
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
  redirect: vi.fn(),
}));

async function renderChrome(component: 'Footer' | 'Header', locale: 'zh-CN'): Promise<string> {
  vi.resetModules();
  const { default: Component } = await import(`../src/components/${component}`);
  const { container } = render(
    <NextIntlClientProvider locale={locale} messages={loadMessages(locale)}>
      <Component />
    </NextIntlClientProvider>,
  );
  return container.innerHTML;
}

const PRODUCT_HREFS = ['href="/routers"', 'href="/releases"', 'href="/retailer/rules"', 'href="/guides"', 'href="/opensource"'];
// /routers 是路由器版产品页，由产品栏链接；已下线的自备教程 /routers/diy 不得再出现在页脚。
// /changelog 只是到 /releases 的兼容跳转，页脚不再链它（同一目标只出现一次）。
// 「定价」是真实页面 /pricing（购买页由定价页的 CTA 进入，不再直接挂在导航上）。
const KAITU_FOOTER = [...PRODUCT_HREFS, 'href="/install"', 'href="/pricing"', 'href="/k2"', 'href="/k2/quickstart"', 'href="/support"', 'href="/support#faq"', 'href="/support#contact"', 'href="/privacy"', 'href="/terms"'];
// 退出统计是同站 API 路径，必须是原生 <a>（next-intl 的 Link 会补 locale 前缀，打到不存在的路由）。
const OPT_OUT = 'href="/api/px/optout"';

describe('footer links only existing pages', () => {
  it('kaitu: every configured link, no redirect-only or retired paths', async () => {
    const html = await renderChrome('Footer', 'zh-CN');
    for (const href of KAITU_FOOTER) expect(html, href).toContain(href);
    expect(html).toContain(OPT_OUT);
    expect(html).not.toContain('href="/routers/diy"');
    expect(html).not.toContain('href="/changelog"');
    expect(html).not.toContain('mailto:');
    expect(html).not.toContain('github.com');
  });
});

// 顶栏的一级项都是链接（带子项的分组也是——父项可点击，子项在悬停时才渲染），
// 所以初始 HTML 里必须能看到每个一级路径。
describe('header links only existing pages', () => {
  it('kaitu: Features / Pricing / Router Edition / Help / Developers as links, Free Download CTA', async () => {
    const html = await renderChrome('Header', 'zh-CN');
    for (const href of ['href="/#features"', 'href="/pricing"', 'href="/routers"', 'href="/guides"', 'href="/k2"', 'href="/install"']) {
      expect(html, href).toContain(href);
    }
    // 首页锚点不再是一级项：只允许作为"功能"分组的父项出现一次。
    expect(html.match(/href="\/#/g)?.length ?? 0).toBe(1);
  });
});
