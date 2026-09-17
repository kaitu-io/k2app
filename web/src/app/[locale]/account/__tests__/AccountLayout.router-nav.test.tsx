/**
 * 账户侧栏「我的路由器」入口（spec §4.4 第 10 点）：挂载后拉一次 `getUserRouter`，
 * `hasRouter=true` 才在 `/account/delegate` 之前插入条目；overleap 品牌不请求也不显示。
 *
 * 品牌判定用真实的 `siteBrand()`（不 mock `@/lib/brands`）——`NEXT_PUBLIC_BRAND` 在默认
 * 测试运行下未设置，`siteBrand()` 回落 kaitu，所以 `describe.runIf` 这组会跑；overleap 分支
 * 用 `it.skipIf` 独立守着，默认跑不到（见任务报告说明）。
 */
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import AccountLayout from '../layout';
import { siteBrand } from '@/lib/brands';

const mockGetUserRouter = vi.fn();

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: true, isAuthLoading: false, logout: vi.fn() }),
}));

vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));

vi.mock('@/i18n/routing', () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: () => '/account',
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: {
    getUserRouter: (...a: unknown[]) => mockGetUserRouter(...a),
  },
}));

describe.runIf(siteBrand().id === 'kaitu')('AccountLayout 路由器侧栏入口（kaitu）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('hasRouter=true 时在 /account/delegate 之前插入「我的路由器」条目', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: true });
    render(
      <AccountLayout>
        <div>children</div>
      </AccountLayout>,
    );

    await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalledWith({ autoRedirectToAuth: false }));
    const links = await screen.findAllByText('routers.edition.account.navTitle');
    expect(links.length).toBeGreaterThan(0);
    const link = links[0].closest('a');
    expect(link).toHaveAttribute('href', '/account/router');
  });

  it('hasRouter=false 时不显示「我的路由器」条目', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: false });
    render(
      <AccountLayout>
        <div>children</div>
      </AccountLayout>,
    );

    await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalled());
    expect(screen.queryByText('routers.edition.account.navTitle')).toBeNull();
  });
});

it.skipIf(siteBrand().id !== 'overleap')('overleap 品牌不请求路由器信息，也不显示入口', async () => {
  render(
    <AccountLayout>
      <div>children</div>
    </AccountLayout>,
  );

  await waitFor(() => expect(screen.getByText('admin.account.title')).toBeTruthy());
  expect(mockGetUserRouter).not.toHaveBeenCalled();
  expect(screen.queryByText('routers.edition.account.navTitle')).toBeNull();
});
