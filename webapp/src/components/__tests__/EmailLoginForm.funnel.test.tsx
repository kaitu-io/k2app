/**
 * Funnel: the inline login form (Purchase page, IosSubscribePanel) reports the
 * same auth events as LoginDialog — otherwise everyone who signs in on the
 * paywall itself reads as "never authenticated".
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/EmailLoginForm.funnel.test.tsx
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';

vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackFunnelDaily: vi.fn(),
    trackAppOpen: vi.fn(), trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));
vi.mock('../../services/cloud-api', () => ({ cloudApi: { post: vi.fn() } }));
vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid'),
}));
vi.mock('../../services/cache-store', () => ({
  cacheStore: { clear: vi.fn(), get: vi.fn(() => null), subscribe: vi.fn(() => vi.fn()) },
}));
vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: any) => any) =>
    selector({ isAuthenticated: false, setIsAuthenticated: vi.fn() }),
}));
vi.mock('../../hooks/useAppConfig', () => ({
  useAppConfig: () => ({ appConfig: null }),
}));

import EmailLoginForm from '../EmailLoginForm';
import { statsService } from '../../services/stats';
import { cloudApi } from '../../services/cloud-api';

const onLoginSuccess = vi.fn();

function renderForm() {
  return render(
    <I18nextProvider i18n={i18n}>
      <EmailLoginForm onLoginSuccess={onLoginSuccess} />
    </I18nextProvider>,
  );
}

// Load-tolerant waits. Negative assertions first wait for the rendered error
// alert — the positive sign that the failed handler ran to completion — so
// "nothing reported" is not asserted before anything could have been.
const WAIT = { timeout: 5000 };
const errorShown = (container: HTMLElement) =>
  waitFor(() => expect(container.querySelector('[role="alert"]')).not.toBeNull(), WAIT);

const calls = (event: string) =>
  (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === event);

function typeEmail(container: HTMLElement) {
  const input = container.querySelector('input[autocomplete="email"]') as HTMLInputElement;
  fireEvent.change(input, { target: { value: 'a@b.com' } });
  return input;
}

describe('EmailLoginForm funnel events', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (cloudApi.post as any).mockResolvedValue({
      code: 0,
      data: { userExists: true, isActivated: true, isFirstOrderDone: false },
    });
  });

  it('rendering the form reports nothing', () => {
    renderForm();
    expect(statsService.trackFunnel).not.toHaveBeenCalled();
  });

  it('a successful code request reports auth_code_sent', async () => {
    const { container } = renderForm();
    const input = typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/code', expect.anything()), WAIT);
    await waitFor(() => expect(calls('auth_code_sent')).toEqual([['auth_code_sent']]), WAIT);
  });

  it('a failed code request reports nothing', async () => {
    (cloudApi.post as any).mockResolvedValue({ code: 400001, message: 'bad', data: null });
    const { container } = renderForm();
    const input = typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/code', expect.anything()), WAIT);
    await errorShown(container);
    expect(statsService.trackFunnel).not.toHaveBeenCalled();
  });

  it('a successful verification-code login reports auth_done; a failed one does not', async () => {
    const { container } = renderForm();
    const input = typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(calls('auth_code_sent')).toHaveLength(1), WAIT);

    const code = await waitFor(() => {
      const el = container.querySelector('input[autocomplete="one-time-code"]') as HTMLInputElement;
      expect(el).not.toBeNull();
      return el;
    });
    fireEvent.change(code, { target: { value: '123456' } });

    (cloudApi.post as any).mockResolvedValueOnce({ code: 400003, message: 'wrong code', data: null });
    fireEvent.keyDown(code, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/login', expect.anything()), WAIT);
    await errorShown(container);
    expect(calls('auth_done')).toHaveLength(0);
    expect(onLoginSuccess).not.toHaveBeenCalled();

    (cloudApi.post as any).mockResolvedValueOnce({ code: 0, data: { accessToken: 'x', refreshToken: 'y' } });
    fireEvent.change(code, { target: { value: '654321' } });
    fireEvent.keyDown(code, { key: 'Enter' });
    await waitFor(() => expect(calls('auth_done')).toEqual([['auth_done']]), WAIT);
    expect(onLoginSuccess).toHaveBeenCalledTimes(1);
  });

  it('a successful password login reports auth_done; a failed one does not', async () => {
    const { container } = renderForm();
    fireEvent.click(container.querySelectorAll('[role="tab"]')[1] as HTMLElement);
    const email = container.querySelector('input[autocomplete="email"]') as HTMLInputElement;
    const pw = container.querySelector('input[type="password"]') as HTMLInputElement;
    fireEvent.change(email, { target: { value: 'a@b.com' } });
    fireEvent.change(pw, { target: { value: 'k7N#mq2P!xT9' } });

    (cloudApi.post as any).mockResolvedValueOnce({ code: 400003, message: 'nope', data: null });
    fireEvent.keyDown(pw, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/login/password', expect.anything()), WAIT);
    await errorShown(container);
    expect(calls('auth_done')).toHaveLength(0);

    (cloudApi.post as any).mockResolvedValueOnce({ code: 0, data: { accessToken: 'x', refreshToken: 'y' } });
    fireEvent.keyDown(pw, { key: 'Enter' });
    await waitFor(() => expect(calls('auth_done')).toEqual([['auth_done']]), WAIT);
    expect(onLoginSuccess).toHaveBeenCalledTimes(1);
  });

  it('a throwing tracker never breaks login', async () => {
    (statsService.trackFunnel as any).mockImplementation(() => { throw new Error('analytics down'); });
    const { container } = renderForm();
    fireEvent.click(container.querySelectorAll('[role="tab"]')[1] as HTMLElement);
    const email = container.querySelector('input[autocomplete="email"]') as HTMLInputElement;
    const pw = container.querySelector('input[type="password"]') as HTMLInputElement;
    fireEvent.change(email, { target: { value: 'a@b.com' } });
    fireEvent.change(pw, { target: { value: 'k7N#mq2P!xT9' } });
    (cloudApi.post as any).mockResolvedValueOnce({ code: 0, data: { accessToken: 'x', refreshToken: 'y' } });
    fireEvent.keyDown(pw, { key: 'Enter' });
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1), WAIT);
  });
});
