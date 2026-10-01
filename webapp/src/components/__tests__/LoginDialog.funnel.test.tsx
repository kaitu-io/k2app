import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';

vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogContentText: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
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
vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({ links: { termsOfServiceUrl: 'https://example.test/terms' } }),
}));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

import LoginDialog from '../LoginDialog';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { statsService } from '../../services/stats';
import { cloudApi } from '../../services/cloud-api';

function renderDialog(trigger = 'guard:connect') {
  useLoginDialogStore.setState({ isOpen: true, message: '', trigger, redirectPath: undefined });
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <LoginDialog />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

const calls = (event: string) =>
  (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === event);

async function typeEmail(container: HTMLElement) {
  const input = container.querySelector('input[autocomplete="email"]') as HTMLInputElement;
  fireEvent.change(input, { target: { value: 'a@b.com' } });
  return input;
}

describe('LoginDialog funnel events', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (cloudApi.post as any).mockResolvedValue({
      code: 0,
      data: { userExists: true, isActivated: true, isFirstOrderDone: false },
    });
  });

  it('opening reports login_view with the trigger as source, once', () => {
    const { rerender } = renderDialog('guard:connect');
    expect(calls('login_view')).toEqual([['login_view', { source: 'guard:connect' }]]);
    rerender(
      <MemoryRouter>
        <I18nextProvider i18n={i18n}><LoginDialog /></I18nextProvider>
      </MemoryRouter>,
    );
    expect(calls('login_view')).toHaveLength(1);
  });

  it('truncates a long trigger to 32 chars', () => {
    renderDialog('x'.repeat(50));
    expect(calls('login_view')[0][1].source).toBe('x'.repeat(32));
  });

  it('a successful code request reports auth_code_sent', async () => {
    const { container } = renderDialog();
    const input = await typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/code', expect.anything()));
    await waitFor(() => expect(calls('auth_code_sent')).toHaveLength(1));
  });

  it('a failed code request reports nothing', async () => {
    (cloudApi.post as any).mockResolvedValue({ code: 400001, message: 'bad', data: null });
    const { container } = renderDialog();
    const input = await typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/code', expect.anything()));
    await new Promise((r) => setTimeout(r, 50));
    expect(calls('auth_code_sent')).toHaveLength(0);
  });

  it('a successful password login reports auth_done; a failed one does not', async () => {
    const { container } = renderDialog();
    fireEvent.click(container.querySelectorAll('[role="tab"]')[1] as HTMLElement);
    const email = container.querySelector('input[autocomplete="email"]') as HTMLInputElement;
    const pw = container.querySelector('input[type="password"]') as HTMLInputElement;
    fireEvent.change(email, { target: { value: 'a@b.com' } });
    fireEvent.change(pw, { target: { value: 'k7N#mq2P!xT9' } });

    (cloudApi.post as any).mockResolvedValueOnce({ code: 400003, message: 'nope', data: null });
    fireEvent.keyDown(pw, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/login/password', expect.anything()));
    await new Promise((r) => setTimeout(r, 50));
    expect(calls('auth_done')).toHaveLength(0);

    (cloudApi.post as any).mockResolvedValueOnce({ code: 0, data: { accessToken: 'x', refreshToken: 'y' } });
    fireEvent.keyDown(pw, { key: 'Enter' });
    await waitFor(() => expect(calls('auth_done')).toHaveLength(1));
  });

  it('a successful verification-code login reports auth_done', async () => {
    const { container } = renderDialog();
    const input = await typeEmail(container);
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(calls('auth_code_sent')).toHaveLength(1));

    (cloudApi.post as any).mockResolvedValue({ code: 0, data: { accessToken: 'x', refreshToken: 'y' } });
    const code = await waitFor(() => {
      const el = container.querySelector('input[inputmode="numeric"], input[autocomplete="one-time-code"]') as HTMLInputElement;
      expect(el).not.toBeNull();
      return el;
    });
    fireEvent.change(code, { target: { value: '123456' } });
    fireEvent.keyDown(code, { key: 'Enter' });
    await waitFor(() => expect(cloudApi.post).toHaveBeenCalledWith('/api/auth/login', expect.anything()));
    await waitFor(() => expect(calls('auth_done')).toHaveLength(1));
  });

  it('the Activate Service button navigates to /purchase with from=login_dialog', () => {
    const { getByText } = renderDialog();
    fireEvent.click(getByText(i18n.t('auth:auth.activateService')));
    expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'login_dialog' } });
  });
});
