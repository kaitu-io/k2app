import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import SubscriptionStatusCard from '../SubscriptionStatusCard';
import type { User } from '@/lib/api';

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

// 2026-08-18T00:00:00Z 作为"现在"，让剩余天数断言可复现
const NOW_MS = 1786924800000;
const DAY = 86400;
const NOW_S = NOW_MS / 1000;

function makeUser(overrides: Partial<User> = {}): User {
  return {
    uuid: 'u-1',
    expiredAt: 0,
    isFirstOrderDone: false,
    hasPassword: true,
    loginIdentifies: [],
    deviceCount: 0,
    ...overrides,
  };
}

describe('SubscriptionStatusCard', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW_MS);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('有效订阅：显示到期日期与剩余天数', () => {
    render(<SubscriptionStatusCard profile={makeUser({ expiredAt: NOW_S + 30 * DAY, tier: 'basic' })} />);
    expect(screen.getByTestId('subscription-status-card')).toBeTruthy();
    expect(screen.getByTestId('status-active')).toBeTruthy();
    // 剩余天数向上取整，30 天后到期 → 30
    expect(screen.getByText(/subscription\.daysLeft.*"days":30/)).toBeTruthy();
  });

  it('已过期：显示过期状态而非有效状态', () => {
    render(<SubscriptionStatusCard profile={makeUser({ expiredAt: NOW_S - DAY, tier: 'basic' })} />);
    expect(screen.getByTestId('status-expired')).toBeTruthy();
    expect(screen.queryByTestId('status-active')).toBeNull();
  });

  it('从未订阅（expiredAt=0）：显示未订阅状态', () => {
    render(<SubscriptionStatusCard profile={makeUser({ expiredAt: 0 })} />);
    expect(screen.getByTestId('status-none')).toBeTruthy();
    expect(screen.queryByTestId('status-active')).toBeNull();
    expect(screen.queryByTestId('status-expired')).toBeNull();
  });

  it('档位存在时渲染档位，缺失时不渲染档位行', () => {
    const { unmount } = render(
      <SubscriptionStatusCard profile={makeUser({ expiredAt: NOW_S + DAY, tier: 'family' })} />
    );
    expect(screen.getByTestId('tier-row').textContent).toContain('family');
    unmount();
    render(<SubscriptionStatusCard profile={makeUser({ expiredAt: NOW_S + DAY })} />);
    expect(screen.queryByTestId('tier-row')).toBeNull();
  });

  it('compact 模式不渲染续费 CTA（购买页已经是 CTA 本体，避免重复按钮）', () => {
    render(
      <SubscriptionStatusCard compact profile={makeUser({ expiredAt: NOW_S + 30 * DAY })} />
    );
    expect(screen.getByTestId('subscription-status-card')).toBeTruthy();
    expect(screen.queryByTestId('renew-cta')).toBeNull();
  });

  it('非 compact 模式渲染指向 /purchase 的 CTA', () => {
    render(<SubscriptionStatusCard profile={makeUser({ expiredAt: NOW_S + 30 * DAY })} />);
    expect(screen.getByTestId('renew-cta').getAttribute('href')).toBe('/purchase');
  });
});
