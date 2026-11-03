import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

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

const api = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), request: vi.fn() }));
vi.mock('../../services/cloud-api', () => ({ cloudApi: api }));

import PushCoordinator from '../PushCoordinator';
import { useAuthStore } from '../../stores/auth.store';
import { useVPNMachineStore } from '../../stores/vpn-machine.store';
import { _resetPushSyncState } from '../../services/push-service';
import type { IPushBridge } from '../../types/kaitu-core';

function setup(permission: 'prompt' | 'granted' | 'denied' = 'prompt') {
  let tap: ((d: Record<string, unknown>) => void) | null = null;
  const bridge: IPushBridge = {
    checkPermission: vi.fn().mockResolvedValue(permission),
    requestPermission: vi.fn().mockResolvedValue('granted'),
    register: vi.fn().mockResolvedValue('tok-1'),
    appId: vi.fn().mockResolvedValue('io.overleap'),
    onTap: vi.fn((cb) => {
      tap = cb;
    }),
    openSettings: vi.fn(),
  };
  (window as any)._platform = { os: 'ios', version: '1.0.0', push: bridge };
  return { bridge, tap: (d: Record<string, unknown>) => tap?.(d) };
}

function renderApp() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <PushCoordinator />
      <Routes>
        <Route path="/" element={<div>home</div>} />
        <Route path="/purchase" element={<div>purchase-page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function setVpn(state: string) {
  act(() => {
    useVPNMachineStore.setState({ state } as any);
  });
}

describe('PushCoordinator', () => {
  let original: unknown;
  beforeEach(() => {
    original = window._platform;
    _resetPushSyncState();
    localStorage.clear();
    api.post.mockResolvedValue({ code: 0 });
    api.request.mockResolvedValue({ code: 0 });
    useAuthStore.setState({ isAuthenticated: true, sessionEpoch: 1 });
    useVPNMachineStore.setState({ state: 'idle' } as any);
  });
  afterEach(() => {
    (window as any)._platform = original;
    vi.clearAllMocks();
  });

  it('renders nothing and touches nothing without the push bridge', () => {
    (window as any)._platform = { os: 'ios' };
    const { container } = renderApp();
    setVpn('connected');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(container.textContent).toBe('home');
    expect(api.post).not.toHaveBeenCalled();
  });

  it('offers the explainer only after the first connect, never on launch', async () => {
    setup('prompt');
    renderApp();
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    setVpn('connecting');
    expect(screen.queryByRole('dialog')).toBeNull();
    setVpn('connected');
    await screen.findByRole('dialog');
  });

  it('does not offer it once the system permission is decided', async () => {
    const { bridge } = setup('granted');
    renderApp();
    setVpn('connected');
    await waitFor(() => expect(bridge.checkPermission).toHaveBeenCalled());
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('marketing checkbox is unchecked by default; enabling without it never writes consent', async () => {
    const { bridge } = setup('prompt');
    renderApp();
    setVpn('connected');
    await screen.findByRole('dialog');
    const box = screen.getByRole('checkbox') as HTMLInputElement;
    expect(box.checked).toBe(false);
    fireEvent.click(screen.getByText('push.enable'));
    await waitFor(() => expect(bridge.requestPermission).toHaveBeenCalled());
    expect(api.request).not.toHaveBeenCalledWith('PUT', '/api/push/preferences', expect.anything());
  });

  it('checking the box opts in, then registers after the grant', async () => {
    const { bridge } = setup('prompt');
    // 系统框点「允许」之后，权限状态随之变为 granted
    (bridge.requestPermission as any).mockImplementation(async () => {
      (bridge.checkPermission as any).mockResolvedValue('granted');
      return 'granted';
    });
    renderApp();
    setVpn('connected');
    await screen.findByRole('dialog');
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(screen.getByText('push.enable'));
    await waitFor(() =>
      expect(api.request).toHaveBeenCalledWith('PUT', '/api/push/preferences', { marketingOptIn: true }),
    );
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/api/push/token', expect.objectContaining({ token: 'tok-1' })));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('"later" closes without the system sheet and is remembered', async () => {
    const { bridge } = setup('prompt');
    renderApp();
    setVpn('connected');
    await screen.findByRole('dialog');
    fireEvent.click(screen.getByText('push.later'));
    expect(bridge.requestPermission).not.toHaveBeenCalled();
    expect(screen.queryByRole('dialog')).toBeNull();
    setVpn('disconnected');
    setVpn('connected');
    await waitFor(() => expect((bridge.checkPermission as any).mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('a tap on a push opens its page', async () => {
    const { tap } = setup('granted');
    const { wirePushTaps } = await import('../../services/push-service');
    wirePushTaps();
    renderApp();
    act(() => tap({ route: '/purchase' }));
    await screen.findByText('purchase-page');
  });

  it('reports the token for each new login session', async () => {
    setup('granted');
    renderApp();
    await waitFor(() => expect(api.post).toHaveBeenCalledTimes(1));
    act(() => useAuthStore.setState({ sessionEpoch: 2 }));
    await waitFor(() => expect(api.post).toHaveBeenCalledTimes(2));
  });
});
