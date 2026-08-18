import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import KaituAccountClient from '../KaituAccountClient';

const mockGetUserProfile = vi.fn();
const mockGetProHistories = vi.fn();

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key,
  useLocale: () => 'zh-CN',
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));
vi.mock('@/lib/api', () => ({
  api: {
    getUserProfile: (...a: unknown[]) => mockGetUserProfile(...a),
    getProHistories: (...a: unknown[]) => mockGetProHistories(...a),
  },
}));

const NOW_MS = 1786924800000;
const NOW_S = NOW_MS / 1000;

const PROFILE = {
  uuid: 'u-1',
  expiredAt: NOW_S + 60 * 86400,
  isFirstOrderDone: true,
  hasPassword: true,
  tier: 'basic',
  loginIdentifies: [{ type: 'email' as const, value: 'buyer@example.com' }],
  deviceCount: 2,
};

describe('KaituAccountClient', () => {
  beforeEach(() => {
    // 只假 Date：vi.useFakeTimers() 默认连 setTimeout 一起假掉，
    // 而 testing-library 的 waitFor 靠真实定时器轮询 —— 全假会直接死锁成 timeout。
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(NOW_MS);
    vi.clearAllMocks();
    mockGetUserProfile.mockResolvedValue(PROFILE);
    mockGetProHistories.mockResolvedValue({
      items: [
        { type: 'recharge', days: 365, reason: '', createdAt: NOW_S - 86400, order: { uuid: 'o-1', title: '年付套餐', originAmount: 19900, campaignReduceAmount: 0, payAmount: 19900, isPaid: true } },
        { type: 'reward', days: 30, reason: '邀请奖励', createdAt: NOW_S - 172800 },
      ],
    });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('渲染订阅状态卡（购买后能看到到期时间）', async () => {
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('subscription-status-card')).toBeTruthy());
    expect(screen.getByTestId('status-active')).toBeTruthy();
  });

  it('渲染登录邮箱与设备数', async () => {
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('account-email')).toBeTruthy());
    expect(screen.getByTestId('account-email').textContent).toContain('buyer@example.com');
    expect(screen.getByTestId('account-devices').textContent).toContain('2');
  });

  it('接上此前从未被调用的 pro-histories 接口并渲染条目', async () => {
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('history-list')).toBeTruthy());
    expect(mockGetProHistories).toHaveBeenCalledWith({ page: 1, pageSize: 10 });
    expect(screen.getAllByTestId('history-item')).toHaveLength(2);
    expect(screen.queryByTestId('history-empty')).toBeNull();
  });

  it('历史为空时显示空态而非空白', async () => {
    mockGetProHistories.mockResolvedValue({ items: [] });
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('history-empty')).toBeTruthy());
    expect(screen.queryByTestId('history-list')).toBeNull();
  });

  it('历史接口失败不拖垮订阅状态：状态卡仍渲染', async () => {
    mockGetProHistories.mockRejectedValue(new Error('boom'));
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('subscription-status-card')).toBeTruthy());
    expect(screen.getByTestId('history-empty')).toBeTruthy();
  });

  it('档案接口失败时给出错误态，不无限转圈', async () => {
    mockGetUserProfile.mockRejectedValue(new Error('boom'));
    render(<KaituAccountClient />);
    await waitFor(() => expect(screen.getByTestId('profile-error')).toBeTruthy());
  });
});
