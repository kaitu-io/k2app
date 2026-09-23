/**
 * `/account/router` 服务端 page：输出文档标题 metadata，主体交给客户端组件。
 */
import { describe, it, expect, vi } from 'vitest';

vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));
vi.mock('@/i18n/routing', () => ({ routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'] } }));
vi.mock('../RouterAccountClient', () => ({ default: () => null }));

import { getTranslations } from 'next-intl/server';
import AccountRouterPage, { generateMetadata } from '../page.kaitu';

describe('account/router/page.kaitu', () => {
  it('generateMetadata 取 routers 命名空间的 edition.account.metaTitle', async () => {
    const metadata = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(metadata.title).toBe('edition.account.metaTitle');
    expect(getTranslations).toHaveBeenCalledWith({ locale: 'zh-CN', namespace: 'routers' });
  });

  it('是服务端 async 组件，渲染客户端主体', async () => {
    const element = await AccountRouterPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });
});
