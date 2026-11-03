import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

const api = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), request: vi.fn() }));
vi.mock('../../services/cloud-api', () => ({ cloudApi: api }));

import NotificationSettings from '../NotificationSettings';
import { useAuthStore } from '../../stores/auth.store';
import { _resetPushSyncState } from '../../services/push-service';
import type { IPushBridge } from '../../types/kaitu-core';

function withBridge(permission: 'prompt' | 'granted' | 'denied') {
  const bridge: IPushBridge = {
    checkPermission: vi.fn().mockResolvedValue(permission),
    requestPermission: vi.fn().mockResolvedValue('granted'),
    register: vi.fn().mockResolvedValue('tok'),
    appId: vi.fn().mockResolvedValue('io.overleap'),
    onTap: vi.fn(),
    openSettings: vi.fn().mockResolvedValue(undefined),
  };
  (window as any)._platform = { os: 'ios', push: bridge };
  return bridge;
}

describe('NotificationSettings', () => {
  let original: unknown;
  beforeEach(() => {
    original = window._platform;
    _resetPushSyncState();
    api.get.mockResolvedValue({ code: 0, data: { marketingOptIn: false } });
    api.request.mockResolvedValue({ code: 0 });
    api.post.mockResolvedValue({ code: 0 });
    useAuthStore.setState({ isAuthenticated: true });
  });
  afterEach(() => {
    (window as any)._platform = original;
    vi.clearAllMocks();
  });

  it('is hidden without push support', () => {
    (window as any)._platform = { os: 'android' };
    const { container } = render(<NotificationSettings />);
    expect(container.innerHTML).toBe('');
  });

  it('denied → offers the Settings shortcut', async () => {
    const bridge = withBridge('denied');
    render(<NotificationSettings />);
    fireEvent.click(await screen.findByText('push.openSettings'));
    await waitFor(() => expect(bridge.openSettings).toHaveBeenCalled());
  });

  it('undecided → enable asks the system and registers on grant', async () => {
    const bridge = withBridge('prompt');
    (bridge.requestPermission as any).mockImplementation(async () => {
      (bridge.checkPermission as any).mockResolvedValue('granted');
      return 'granted';
    });
    render(<NotificationSettings />);
    fireEvent.click(await screen.findByText('push.enable'));
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/api/push/token', expect.objectContaining({ token: 'tok' })));
    await screen.findByText('push.statusOn');
  });

  it('marketing switch reflects and writes the server preference, reverting on failure', async () => {
    withBridge('granted');
    render(<NotificationSettings />);
    const sw = (await screen.findByTestId('account-push-marketing')).querySelector('input') as HTMLInputElement;
    await waitFor(() => expect(sw.disabled).toBe(false));
    expect(sw.checked).toBe(false);

    fireEvent.click(sw);
    await waitFor(() => expect(api.request).toHaveBeenCalledWith('PUT', '/api/push/preferences', { marketingOptIn: true }));
    await waitFor(() => expect(sw.checked).toBe(true));

    api.request.mockResolvedValue({ code: 500 });
    fireEvent.click(sw);
    await waitFor(() => expect(sw.checked).toBe(true));
  });

  it('marketing switch is hidden when logged out', async () => {
    withBridge('granted');
    useAuthStore.setState({ isAuthenticated: false });
    render(<NotificationSettings />);
    await screen.findByText('push.settingTitle');
    expect(screen.queryByTestId('account-push-marketing')).toBeNull();
  });
});
