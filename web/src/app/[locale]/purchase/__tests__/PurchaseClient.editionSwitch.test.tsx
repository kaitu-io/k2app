/**
 * PurchaseClient — App 版 / 路由器版切换 + 路由器版 cross-sell（task 4）。
 *
 * 顶部切换让从 /purchase 落地的用户知道还有路由器版；底部 cross-sell 把还没决定的人
 * 导去 /routers。两者都只在独立页展示——embed 模式（desktop app 内嵌 iframe）下宿主
 * 自己控制导航，这里不应该多塞东西进去。
 */
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import PurchaseClient from '../PurchaseClient';

const mockGetUserProfile = vi.fn();
const mockGetPlans = vi.fn();
const mockGetDelegate = vi.fn();
const mockUseEmbedMode = vi.fn();

const NOW_MS = 1786924800000;

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key,
  useLocale: () => 'zh-CN',
}));
vi.mock('@/i18n/routing', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: true, isAuthLoading: false }),
}));
vi.mock('@/contexts/AppConfigContext', () => ({
  useAppConfig: () => ({ appConfig: null, isLoading: false }),
}));
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => mockUseEmbedMode(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/components/MembershipBenefits', () => ({ default: () => null }));
vi.mock('@/components/PurchaseStep1', () => ({ default: () => <div data-testid="step1" /> }));
vi.mock('@/components/PurchaseStep2', () => ({ default: () => <div data-testid="step2" /> }));
vi.mock('@/components/PurchaseStep3', () => ({ default: () => <div data-testid="step3" /> }));
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      getUserProfile: (...a: unknown[]) => mockGetUserProfile(...a),
      getPlans: (...a: unknown[]) => mockGetPlans(...a),
      getDelegate: (...a: unknown[]) => mockGetDelegate(...a),
    },
  };
});

describe('PurchaseClient — App 版 / 路由器版切换', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(NOW_MS);
    vi.clearAllMocks();
    mockGetPlans.mockResolvedValue({ items: [] });
    mockGetDelegate.mockResolvedValue(null);
    mockGetUserProfile.mockResolvedValue({ expiredAt: 0, isFirstOrderDone: false });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('独立页（showNavigation: true）：存在指向 /purchase/router 与 /routers 的链接', async () => {
    mockUseEmbedMode.mockReturnValue({ showNavigation: true, showFooter: true });
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('step1')).toBeTruthy());

    const routerSwitchLink = screen
      .getAllByRole('link')
      .find((a) => a.getAttribute('href') === '/purchase/router');
    const crossSellLink = screen
      .getAllByRole('link')
      .find((a) => a.getAttribute('href') === '/routers');

    expect(routerSwitchLink).toBeTruthy();
    expect(crossSellLink).toBeTruthy();
  });

  it('embed 模式（showNavigation: false）：两个链接都不存在', async () => {
    mockUseEmbedMode.mockReturnValue({ showNavigation: false, showFooter: false });
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('step1')).toBeTruthy());

    const links = screen.queryAllByRole('link').map((a) => a.getAttribute('href'));
    expect(links).not.toContain('/purchase/router');
    expect(links).not.toContain('/routers');
  });
});
