/**
 * 账户侧栏「我的路由器」入口（spec §4.4 第 10 点）：挂载后拉一次 `getUserRouter`，
 * `hasRouter=true` 才在 `/account/delegate` 之前插入条目。
 */
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import AccountLayout from '../layout';

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

describe('AccountLayout 路由器侧栏入口', () => {
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
