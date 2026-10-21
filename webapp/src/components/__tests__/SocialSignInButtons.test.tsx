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
  appConfig: { socialSignIn: { apple: true, google: true } } as any,
}));

vi.mock('../../services/cloud-api', () => ({ cloudApi: { post: m.post } }));
vi.mock('../../services/cache-store', () => ({ cacheStore: { clear: m.cacheClear } }));
vi.mock('../../services/device-udid', () => ({ getDeviceUdid: m.getDeviceUdid }));
vi.mock('../../services/stats', () => ({ statsService: { trackFunnel: m.trackFunnel } }));
vi.mock('../../hooks/useAppConfig', () => ({ useAppConfig: () => ({ appConfig: m.appConfig }) }));
vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) => selector({ setIsAuthenticated: m.setIsAuthenticated }),
}));
// Google reachability is a network fact — pin it so the matrix is deterministic.
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

function setup(providers: string[] = ['apple', 'google']) {
  (window as any)._platform = {
    os: 'ios',
    platformType: 'mobile',
    socialSignIn: { availableProviders: vi.fn().mockResolvedValue(providers), signIn },
  };
  const onSuccess = vi.fn();
  const onError = vi.fn();
  render(
    <I18nextProvider i18n={i18n}>
      <SocialSignInButtons onSuccess={onSuccess} onError={onError} />
    </I18nextProvider>
  );
  return { onSuccess, onError };
}

// findByText, not findByRole({name}): accessible-name computation trips over
// this jsdom setup's getComputedStyle stub.
const button = async (key: string) => (await screen.findByText(i18n.t(key))).closest('button')!;
const appleButton = () => button('auth:auth.continueWithApple');

describe('SocialSignInButtons', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    m.getDeviceUdid.mockResolvedValue('udid-1');
    m.appConfig = { socialSignIn: { apple: true, google: true } };
  });

  it('renders nothing without the platform capability', () => {
    (window as any)._platform = { os: 'macos', platformType: 'desktop' };
    const { container } = render(
      <I18nextProvider i18n={i18n}>
        <SocialSignInButtons onSuccess={vi.fn()} onError={vi.fn()} />
      </I18nextProvider>
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('shows Apple and Google when the server and device allow both', async () => {
    setup();
    expect(await appleButton()).toBeInTheDocument();
    expect(await button('auth:auth.continueWithGoogle')).toBeInTheDocument();
  });

  it('shows nothing when the server switches are off', async () => {
    m.appConfig = { socialSignIn: { apple: false, google: false } };
    setup();
    await waitFor(() => expect((window as any)._platform.socialSignIn.availableProviders).toHaveBeenCalled());
    expect(document.querySelector('button')).toBeNull();
  });

  it('Apple success: raw nonce to server, its hash to the provider, then authenticated', async () => {
    signIn.mockResolvedValue({ idToken: 'jwt', authorizationCode: 'code' });
    m.post.mockResolvedValue({ code: 0, data: { accessToken: 'at' } });
    const { onSuccess, onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(onSuccess).toHaveBeenCalled());

    const [path, body] = m.post.mock.calls[0];
    expect(path).toBe('/api/auth/social/login');
    expect(body).toMatchObject({ provider: 'apple', idToken: 'jwt', authorizationCode: 'code', udid: 'udid-1' });
    expect(signIn).toHaveBeenCalledWith('apple', await sha256Hex(body.nonce));
    expect(m.cacheClear).toHaveBeenCalled();
    expect(m.setIsAuthenticated).toHaveBeenCalledWith(true);
    expect(m.trackFunnel).toHaveBeenCalledWith('auth_done');
    expect(onError).not.toHaveBeenCalled();
  });

  it('user cancel: no request, no error', async () => {
    signIn.mockResolvedValue(null);
    const { onSuccess, onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(signIn).toHaveBeenCalled());
    expect(m.post).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it('native failure shows our own message, not the SDK string', async () => {
    signIn.mockRejectedValue(new Error('GIDSignIn keychain error -34018'));
    const { onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(onError).toHaveBeenCalledWith(i18n.t('auth:auth.socialSignInFailed')));
    expect(m.post).not.toHaveBeenCalled();
  });

  it('server rejection surfaces the mapped error and does not authenticate', async () => {
    signIn.mockResolvedValue({ idToken: 'jwt' });
    m.post.mockResolvedValue({ code: 400014, message: 'invalid' });
    const { onSuccess, onError } = setup();

    fireEvent.click(await appleButton());
    await waitFor(() => expect(onError).toHaveBeenCalledWith(i18n.t('auth:auth.socialTokenInvalid')));
    expect(onSuccess).not.toHaveBeenCalled();
    expect(m.setIsAuthenticated).not.toHaveBeenCalled();
  });
});
