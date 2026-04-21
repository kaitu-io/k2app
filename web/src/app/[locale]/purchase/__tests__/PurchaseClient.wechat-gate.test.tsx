/**
 * PurchaseClient WeChat Android gate — integration test.
 *
 * Goal: prove end-to-end in jsdom that when the WeChat Android UA is active,
 * the purchase flow is replaced by <WeChatBrowserGuide />, and when it's not,
 * the regular purchase flow renders.
 *
 * We mock the heavy peripheral deps (API, contexts, step components, header/footer)
 * because this test is about the branch decision, not those integrations.
 */
import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

// Mock i18n routing — the underlying next-intl/navigation chain fails to load in jsdom
vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN'], defaultLocale: 'zh-CN' },
  Link: ({ children, ...props }: { children: React.ReactNode }) =>
    <a {...props}>{children}</a>,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), back: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/',
  redirect: vi.fn(),
  getPathname: () => '/',
}));

// Mock API layer — both endpoints PurchaseClient hits on mount
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
  return {
    ...actual,
    api: {
      getUserProfile: vi.fn().mockResolvedValue({
        expiredAt: 0,
        isFirstOrderDone: false,
        tier: 'basic',
      }),
      getPlans: vi.fn().mockResolvedValue({ items: [] }),
      createOrder: vi.fn().mockResolvedValue({ order: null, payUrl: '' }),
    },
  };
});

// Mock Auth context
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({
    isAuthenticated: false,
    isAuthLoading: false,
    user: null,
    login: vi.fn(),
    logout: vi.fn(),
    navigateToLogin: vi.fn(),
  }),
}));

// Mock AppConfig context
vi.mock('@/contexts/AppConfigContext', () => ({
  useAppConfig: () => ({
    appConfig: { inviteReward: null },
    isLoading: false,
  }),
}));

// Mock embed hook (keep default: show nav & footer)
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({ showNavigation: true, showFooter: true, isEmbedMode: false, theme: null }),
}));

// Mock step components to cheap sentinels so we can assert on their presence
vi.mock('@/components/PurchaseStep1', () => ({
  default: () => <div data-testid="purchase-step-1" />,
}));
vi.mock('@/components/PurchaseStep2', () => ({
  default: () => <div data-testid="purchase-step-2" />,
}));
vi.mock('@/components/PurchaseStep3', () => ({
  default: () => <div data-testid="purchase-step-3" />,
}));
vi.mock('@/components/MembershipBenefits', () => ({
  default: () => <div data-testid="membership-benefits" />,
}));
vi.mock('@/components/Header', () => ({ default: () => <header data-testid="header" /> }));
vi.mock('@/components/Footer', () => ({ default: () => <footer data-testid="footer" /> }));

// Import AFTER mocks so they take effect
import PurchaseClient from '../PurchaseClient';

const WECHAT_ANDROID_UA =
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN';

const CHROME_ANDROID_UA =
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36';

function spoofUA(ua: string) {
  Object.defineProperty(window.navigator, 'userAgent', {
    value: ua,
    configurable: true,
  });
}

describe('PurchaseClient — WeChat Android gate', () => {
  const originalUA = window.navigator.userAgent;

  afterEach(() => {
    spoofUA(originalUA);
  });

  it('shows the guide overlay (and not the purchase steps) on WeChat Android', async () => {
    spoofUA(WECHAT_ANDROID_UA);
    render(<PurchaseClient />);

    // The guide's title (i18n key in our test mocks) must appear.
    await waitFor(() => {
      expect(screen.getByText('purchase.wechatGuide.title')).toBeInTheDocument();
    });

    // The purchase flow's step components must NOT render.
    expect(screen.queryByTestId('purchase-step-1')).toBeNull();
    expect(screen.queryByTestId('purchase-step-2')).toBeNull();
    expect(screen.queryByTestId('purchase-step-3')).toBeNull();
    expect(screen.queryByTestId('membership-benefits')).toBeNull();

    // The guide must also expose its other 4 i18n keys.
    expect(screen.getByText('purchase.wechatGuide.tapHere')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.step1')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.step2')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.reason')).toBeInTheDocument();
  });

  it('shows the purchase flow (and not the guide) on regular Chrome Android', async () => {
    spoofUA(CHROME_ANDROID_UA);
    render(<PurchaseClient />);

    // Wait for the purchase flow to settle (profile + plans fetches resolve).
    await waitFor(() => {
      expect(screen.getByTestId('purchase-step-1')).toBeInTheDocument();
    });

    expect(screen.getByTestId('purchase-step-2')).toBeInTheDocument();
    expect(screen.getByTestId('purchase-step-3')).toBeInTheDocument();

    // The guide must NOT appear.
    expect(screen.queryByText('purchase.wechatGuide.title')).toBeNull();
  });
});
