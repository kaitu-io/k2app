/**
 * 内容页面包屑：链接层级 + BreadcrumbList 结构化数据。
 *
 * Breadcrumb 是异步服务端组件，测试里直接 await 它得到元素树再渲染。文案由 mock 的
 * getTranslations 回显 key；URL 断言用 Brand 注册表的真实 baseUrl。
 */
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { KAITU, OVERLEAP } from '../src/lib/brands';

vi.mock('next-intl/server', () => ({
  getTranslations: async () => (key: string) => key,
}));

vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'], defaultLocale: 'zh-CN' },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
}));

async function renderCrumb(props: Parameters<typeof import('../src/components/Breadcrumb').default>[0]) {
  const { default: Breadcrumb } = await import('../src/components/Breadcrumb');
  const tree = await Breadcrumb(props);
  return render(tree);
}

describe('Breadcrumb', () => {
  it('renders Home > section > current with only the last item unlinked', async () => {
    const { container } = await renderCrumb({
      locale: 'zh-CN',
      brand: KAITU,
      items: [{ label: '使用指南', href: '/guides' }, { label: '快速上手' }],
    });
    const nav = container.querySelector('nav[aria-label="nav.breadcrumb"]')!;
    const links = [...nav.querySelectorAll('a')].map((a) => [a.getAttribute('href'), a.textContent]);
    expect(links).toEqual([
      ['/', 'nav.home'],
      ['/guides', '使用指南'],
    ]);
    const current = nav.querySelector('[aria-current="page"]')!;
    expect(current.textContent).toBe('快速上手');
  });

  it('emits BreadcrumbList JSON-LD with absolute, locale-prefixed item URLs', async () => {
    const { container } = await renderCrumb({
      locale: 'en-GB',
      brand: OVERLEAP,
      items: [{ label: 'k2 Protocol', href: '/k2' }, { label: 'Quickstart' }],
    });
    const script = container.querySelector('script[type="application/ld+json"]')!;
    const ld = JSON.parse(script.innerHTML);
    expect(ld['@type']).toBe('BreadcrumbList');
    expect(ld.itemListElement.map((e: { position: number; name: string; item?: string }) => [e.position, e.name, e.item])).toEqual([
      [1, 'nav.home', `${OVERLEAP.baseUrl}/en-GB`],
      [2, 'k2 Protocol', `${OVERLEAP.baseUrl}/en-GB/k2`],
      [3, 'Quickstart', undefined],
    ]);
  });

  it('escapes < so a title cannot close the JSON-LD script', async () => {
    const { container } = await renderCrumb({
      locale: 'zh-CN',
      brand: KAITU,
      items: [{ label: 'a</script><b>' }],
    });
    const script = container.querySelector('script[type="application/ld+json"]')!;
    expect(script.innerHTML).not.toContain('</script>');
    expect(JSON.parse(script.innerHTML).itemListElement[1].name).toBe('a</script><b>');
  });
});
