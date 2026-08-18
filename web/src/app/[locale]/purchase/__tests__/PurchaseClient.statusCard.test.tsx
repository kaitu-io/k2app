/**
 * 购买页订阅状态卡的**落地保护**测试。
 *
 * 这一页同时是被邀请人的落地终点：/s/CODE 写下 kaitu_invite_code cookie 后
 * router.push('/purchase')，PurchaseStep1 在这里读 cookie 完成注册。所以任何
 * 加在页面顶部的东西都必须证明自己不会挤掉新用户的首屏。
 *
 * 这里渲染的是真实的 PurchaseClient 与真实的 SubscriptionStatusCard —— 只有
 * 外围重依赖（Header/Footer/各 Step/网络）被替身掉，判定逻辑本身不是替身，
 * 否则测的就不是要保护的那段代码。
 */
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import PurchaseClient from '../PurchaseClient';

const mockGetUserProfile = vi.fn();
const mockGetPlans = vi.fn();
const mockGetDelegate = vi.fn();

const NOW_MS = 1786924800000;
const NOW_S = NOW_MS / 1000;
const DAY = 86400;

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
  useAppConfig: () => ({
    appConfig: { inviteReward: { purchaseRewardDays: 30, minRewardMonths: 12 } },
    isLoading: false,
  }),
}));
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({ showNavigation: false, showFooter: false }),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/components/MembershipBenefits', () => ({ default: () => null }));
vi.mock('@/components/PurchaseStep1', () => ({
  default: () => <div data-testid="step1" />,
}));
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

describe('PurchaseClient — 订阅状态卡的落地保护', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(NOW_MS);
    vi.clearAllMocks();
    mockGetPlans.mockResolvedValue({ items: [] });
    mockGetDelegate.mockResolvedValue(null);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('订阅有效的老用户：显示状态卡', async () => {
    mockGetUserProfile.mockResolvedValue({
      expiredAt: NOW_S + 30 * DAY,
      isFirstOrderDone: true,
      tier: 'basic',
    });
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('subscription-status-card')).toBeTruthy());
    // 购买页是 compact 形态：不重复放续费按钮
    expect(screen.queryByTestId('renew-cta')).toBeNull();
  });

  it('从邀请链接落地的新用户（有邀请码、首单未完成、无订阅）：不显示状态卡', async () => {
    mockGetUserProfile.mockResolvedValue({
      expiredAt: 0,
      isFirstOrderDone: false,
      inviteCode: { code: 'FRIEND01' },
    });
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('step1')).toBeTruthy());
    expect(screen.queryByTestId('subscription-status-card')).toBeNull();
  });

  it('已过期用户：不显示状态卡（那是过期告警的职责，不重复表达）', async () => {
    mockGetUserProfile.mockResolvedValue({
      expiredAt: NOW_S - DAY,
      isFirstOrderDone: true,
    });
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('step1')).toBeTruthy());
    expect(screen.queryByTestId('subscription-status-card')).toBeNull();
  });

  it('未登录（取档案失败，userProfile 为 null）：不显示状态卡', async () => {
    mockGetUserProfile.mockRejectedValue(new Error('unauthorized'));
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('step1')).toBeTruthy());
    expect(screen.queryByTestId('subscription-status-card')).toBeNull();
  });
});
