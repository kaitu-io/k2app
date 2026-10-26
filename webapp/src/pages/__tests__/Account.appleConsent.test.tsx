/**
 * 【我的】iOS + Apple 订阅：「退款时向 Apple 提供使用情况」开关，切换发 PUT 并同步用户缓存。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import i18n, { i18nPromise, changeLanguage } from '../../i18n/i18n';

const m = vi.hoisted(() => ({ request: vi.fn(), cacheSet: vi.fn() }));

vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return { ...actual, useAuth: vi.fn() };
});
vi.mock('../../hooks/useUser', () => ({ useUser: vi.fn() }));
vi.mock('../../hooks/useAppLinks', () => ({ useAppLinks: () => ({ links: { walletUrl: '' } }) }));
vi.mock('../../hooks/usePrivateNodes', () => ({ usePrivateNodes: () => ({ nodes: [] }) }));
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { request: m.request, post: vi.fn(), get: vi.fn() },
}));
vi.mock('../../services/cache-store', () => ({
  cacheStore: { get: vi.fn(), set: m.cacheSet, subscribe: vi.fn(() => () => {}) },
}));
vi.mock('../../components/VersionItem', () => ({ default: () => <div /> }));
vi.mock('../../components/BetaChannelToggle', () => ({ default: () => null }));

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';

function setUser(user: any) {
  vi.mocked(useUser).mockReturnValue({
    user, loading: false, isMembership: true, isExpired: false, fetchUser: vi.fn(),
  } as any);
}
const apple = { provider: 'apple', tier: 'basic', currentPeriodEnd: 4102444800, autoRenew: true, isTrial: false, trialEndsAt: 0, manage: { kind: 'apple_settings' } };
const LABEL = '退款时向 Apple 提供使用情况';

describe('Account — Apple 退款使用情况开关', () => {
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    vi.clearAllMocks();
    const styles: Record<string, string> = { visibility: 'visible', display: 'block', paddingRight: '0px', overflow: 'visible' };
    (window.getComputedStyle as any).mockImplementation(() =>
      new Proxy(styles, {
        get(target, prop) {
          if (prop === 'getVersion') return undefined;
          if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
          return typeof prop === 'string' ? target[prop] || '' : undefined;
        },
      }),
    );
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
    (window as any)._platform = { os: 'ios', version: '1.0.0', openExternal: vi.fn() };
    vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
    m.request.mockResolvedValue({ code: 0, data: { appleConsumptionConsent: false } });
  });
  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
  });

  it('shows the switch (on) for an iOS user with an Apple subscription; toggling PUTs false', async () => {
    setUser({ uuid: 'u', expiredAt: 4102444800, loginIdentifies: [{ type: 'email', value: 'a@b.com' }], subscriptions: [apple], appleConsumptionConsent: true });
    render(<Account />);
    const sw = screen.getByRole('checkbox', { name: LABEL }) as HTMLInputElement;
    expect(sw.checked).toBe(true);
    fireEvent.click(sw);
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', '/api/user/apple-consumption-consent', { consented: false }));
    await waitFor(() => expect(m.cacheSet).toHaveBeenCalledWith('api:user_info', { appleConsumptionConsent: false }, { ttl: 3600 }));
  });

  it('never-asked (null) renders off and toggling PUTs true', async () => {
    m.request.mockResolvedValue({ code: 0, data: { appleConsumptionConsent: true } });
    setUser({ uuid: 'u', expiredAt: 4102444800, loginIdentifies: [{ type: 'email', value: 'a@b.com' }], subscriptions: [apple], appleConsumptionConsent: null });
    render(<Account />);
    const sw = screen.getByRole('checkbox', { name: LABEL }) as HTMLInputElement;
    expect(sw.checked).toBe(false);
    fireEvent.click(sw);
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', '/api/user/apple-consumption-consent', { consented: true }));
  });

  it('hidden without an Apple subscription', () => {
    setUser({ uuid: 'u', expiredAt: 4102444800, loginIdentifies: [{ type: 'email', value: 'a@b.com' }], subscriptions: [{ ...apple, provider: 'stripe' }] });
    render(<Account />);
    expect(screen.queryByText(LABEL)).toBeNull();
  });

  it('hidden on non-iOS even with an Apple subscription', () => {
    (window as any)._platform = { os: 'android', version: '1.0.0', openExternal: vi.fn() };
    setUser({ uuid: 'u', expiredAt: 4102444800, loginIdentifies: [{ type: 'email', value: 'a@b.com' }], subscriptions: [apple] });
    render(<Account />);
    expect(screen.queryByText(LABEL)).toBeNull();
  });
});
