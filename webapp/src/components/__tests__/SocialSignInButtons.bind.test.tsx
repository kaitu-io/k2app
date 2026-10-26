/**
 * SocialSignInButtons bind 模式（设备账号 spec §4.3）：已登录的零身份账号在【其他设备安装】
 * 补绑 Apple / Google。走 /api/user/social/bind，body 不带 udid / remark / language；
 * 不是一次「登录」，所以不记 auth_done、不改 isAuthenticated。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';

const m = vi.hoisted(() => ({
  post: vi.fn(),
  setIsAuthenticated: vi.fn(),
  cacheClear: vi.fn(),
  trackFunnel: vi.fn(),
  getDeviceUdid: vi.fn(),
  appConfig: { socialSignIn: { apple: true, google: false } } as any,
}));

vi.mock('../../services/cloud-api', () => ({ cloudApi: { post: m.post } }));
vi.mock('../../services/cache-store', () => ({ cacheStore: { clear: m.cacheClear } }));
vi.mock('../../services/device-udid', () => ({ getDeviceUdid: m.getDeviceUdid }));
vi.mock('../../services/stats', () => ({ statsService: { trackFunnel: m.trackFunnel } }));
vi.mock('../../hooks/useAppConfig', () => ({ useAppConfig: () => ({ appConfig: m.appConfig }) }));
vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) => selector({ setIsAuthenticated: m.setIsAuthenticated }),
}));
vi.mock('../../services/social-sign-in', async () => {
  const actual = await vi.importActual<typeof import('../../services/social-sign-in')>('../../services/social-sign-in');
  return {
    ...actual,
    resolveSocialProviders: (opts: Parameters<typeof actual.resolveSocialProviders>[0]) =>
      actual.resolveSocialProviders({ ...opts, probe: async () => true }),
  };
});

import SocialSignInButtons from '../SocialSignInButtons';
import { sha256Hex } from '../../services/social-sign-in';

const signIn = vi.fn();

function setup() {
  (window as any)._platform = {
    os: 'ios',
    platformType: 'mobile',
    socialSignIn: { availableProviders: vi.fn().mockResolvedValue(['apple']), signIn },
  };
  const onSuccess = vi.fn();
  const onError = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <SocialSignInButtons mode="bind" onSuccess={onSuccess} onError={onError} />
    </I18nextProvider>
  );
  return { onSuccess, onError };
}

const appleButton = async () =>
  (await screen.findByText(i18n.t('account:deviceBind.appleButton'))).closest('button')!;

describe('SocialSignInButtons mode="bind"', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    m.getDeviceUdid.mockResolvedValue('udid-1');
  });

  it('posts to /api/user/social/bind without device fields and skips auth_done', async () => {
    signIn.mockResolvedValue({ idToken: 'jwt', authorizationCode: 'code' });
    m.post.mockResolvedValue({ code: 0 });
    const { onSuccess, onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(onSuccess).toHaveBeenCalled());

    const [path, body] = m.post.mock.calls[0];
    expect(path).toBe('/api/user/social/bind');
    expect(body).toMatchObject({ provider: 'apple', idToken: 'jwt', authorizationCode: 'code' });
    expect(body).not.toHaveProperty('udid');
    expect(body).not.toHaveProperty('remark');
    expect(body).not.toHaveProperty('language');
    expect(signIn).toHaveBeenCalledWith('apple', await sha256Hex(body.nonce));
    expect(m.cacheClear).toHaveBeenCalled();
    expect(m.trackFunnel).not.toHaveBeenCalledWith('auth_done');
    expect(m.setIsAuthenticated).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it('server rejection reaches onError and never calls onSuccess', async () => {
    signIn.mockResolvedValue({ idToken: 'jwt' });
    m.post.mockResolvedValue({ code: 400014, message: 'invalid' });
    const { onSuccess, onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(onError).toHaveBeenCalledWith(i18n.t('auth:auth.socialTokenInvalid')));
    expect(onSuccess).not.toHaveBeenCalled();
    expect(m.cacheClear).not.toHaveBeenCalled();
  });
});
