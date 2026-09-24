/**
 * 顶栏的位置信号与下拉行为。
 *
 * - 当前页高亮：按 locale 无关的 pathname 与配置路径前缀匹配，子项命中则父项高亮；
 *   首页锚点（/#features）永远不高亮——它们是首页内的区块，不是"所在页面"。
 * - 分组父项是可点击链接，子项悬停时才渲染（初始 HTML 里没有子项路径）。
 *
 * next-intl 在 src/test/setup.ts 里被全局 mock 成回显 key，所以这里只看结构和 href，
 * 不看文案；文案 key 是否存在由 tests/site-config-keys.test.ts 静态核对。
 */
import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';

const pathnameRef = { current: '/' };

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, user: null, logout: vi.fn() }),
}));

vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'], defaultLocale: 'zh-CN' },
  usePathname: () => pathnameRef.current,
  useRouter: () => ({ replace: vi.fn(), push: vi.fn(), refresh: vi.fn() }),
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
  redirect: vi.fn(),
}));

afterEach(() => {
  vi.unstubAllEnvs();
  pathnameRef.current = '/';
});

async function renderHeader(brand: 'kaitu' | 'overleap', pathname: string) {
  vi.stubEnv('NEXT_PUBLIC_BRAND', brand);
  pathnameRef.current = pathname;
  vi.resetModules();
  const { default: Header } = await import('../src/components/Header');
  return render(<Header />);
}

describe('isNavItemActive', () => {
  it('matches exact path and nested paths, never home anchors or external links', async () => {
    const { isNavItemActive } = await import('../src/components/Header');
    expect(isNavItemActive('/k2', { labelKey: 'x', href: '/k2' })).toBe(true);
    expect(isNavItemActive('/k2/quickstart', { labelKey: 'x', href: '/k2' })).toBe(true);
    expect(isNavItemActive('/k2cc', { labelKey: 'x', href: '/k2' })).toBe(false);
    expect(isNavItemActive('/', { labelKey: 'x', href: '/#features' })).toBe(false);
    expect(isNavItemActive('/support', { labelKey: 'x', href: '/support#faq' })).toBe(true);
    expect(isNavItemActive('/', { labelKey: 'x', href: 'https://github.com/getoverleap' })).toBe(false);
  });

  it('only the most specific child is highlighted inside a group', async () => {
    const { activeChildKey } = await import('../src/components/Header');
    const children = [
      { labelKey: 'k2', href: '/k2' },
      { labelKey: 'qs', href: '/k2/quickstart' },
      { labelKey: 'os', href: '/opensource' },
    ];
    expect(activeChildKey('/k2/quickstart', children)).toBe('qs');
    expect(activeChildKey('/k2/server', children)).toBe('k2');
    expect(activeChildKey('/routers', children)).toBeNull();
  });

  it('a group is active when any child matches', async () => {
    const { isNavItemActive } = await import('../src/components/Header');
    const group = {
      labelKey: 'g',
      href: '/guides',
      children: [
        { labelKey: 'a', href: '/guides' },
        { labelKey: 'b', href: '/releases' },
      ],
    };
    expect(isNavItemActive('/releases', group)).toBe(true);
    expect(isNavItemActive('/routers', group)).toBe(false);
  });
});

describe('Header current-page highlight', () => {
  it('kaitu: /k2/quickstart marks the Developers group, not Help', async () => {
    const { container } = await renderHeader('kaitu', '/k2/quickstart');
    const current = [...container.querySelectorAll('a[aria-current="page"]')].map((a) => a.getAttribute('href'));
    expect(current).toEqual(['/k2']);
  });

  it('kaitu: /routers marks the Router Edition link only', async () => {
    const { container } = await renderHeader('kaitu', '/routers');
    const current = [...container.querySelectorAll('a[aria-current="page"]')].map((a) => a.getAttribute('href'));
    expect(current).toEqual(['/routers']);
  });

  it('overleap: /support marks Help', async () => {
    const { container } = await renderHeader('overleap', '/support');
    const current = [...container.querySelectorAll('a[aria-current="page"]')].map((a) => a.getAttribute('href'));
    expect(current).toEqual(['/support']);
  });

  it('home page highlights nothing', async () => {
    const { container } = await renderHeader('kaitu', '/');
    expect(container.querySelectorAll('[aria-current="page"]').length).toBe(0);
  });
});

describe('Header dropdown groups', () => {
  it('kaitu: children render on hover and the parent stays a link', async () => {
    const { container } = await renderHeader('kaitu', '/');
    expect(container.innerHTML).not.toContain('href="/k2/quickstart"');
    const parent = container.querySelector('a[href="/k2"]')!;
    expect(parent.tagName).toBe('A');
    fireEvent.mouseEnter(parent.parentElement!);
    const menu = container.querySelector('[role="menu"]')!;
    expect(menu).toBeTruthy();
    const hrefs = [...menu.querySelectorAll('a')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/k2', '/k2/quickstart', '/opensource', 'https://github.com/getoverleap']);
    const github = menu.querySelector('a[href="https://github.com/getoverleap"]')!;
    expect(github.getAttribute('target')).toBe('_blank');
    expect(github.getAttribute('rel')).toContain('noopener');
  });

  it('kaitu: the active group is expanded by default in the mobile menu', async () => {
    const { container, getByLabelText } = await renderHeader('kaitu', '/guides/getting-started');
    fireEvent.click(getByLabelText('nav.nav.menu'));
    const mobile = container.querySelector('#site-mobile-menu')!;
    expect(mobile.innerHTML).toContain('href="/support#faq"');
    expect(mobile.innerHTML).not.toContain('href="/k2/quickstart"');
  });
});
