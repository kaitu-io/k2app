import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useAuthStore } from '../auth.store';
import { resetVpnClient, createVpnClient } from '../../vpn-client';
import { MockVpnClient } from '../../vpn-client/mock-client';

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getAuthCode: vi.fn(),
    login: vi.fn(),
    refreshToken: vi.fn(),
    getUserInfo: vi.fn(),
    logout: vi.fn(),
  },
  setAuthToken: vi.fn(),
}));

import { cloudApi, setAuthToken } from '../../api/cloud';

const TOKEN_KEY = 'k2_auth_token';
const REFRESH_KEY = 'k2_refresh_token';

function createLocalStorageStub() {
  const store = new Map<string, string>();
  return {
    getItem: vi.fn((key: string) => store.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => { store.set(key, value); }),
    removeItem: vi.fn((key: string) => { store.delete(key); }),
    clear: vi.fn(() => { store.clear(); }),
    get length() { return store.size; },
    key: vi.fn((index: number) => [...store.keys()][index] ?? null),
  };
}

describe('useAuthStore — logout', () => {
  let storage: ReturnType<typeof createLocalStorageStub>;
  let mock: MockVpnClient;

  beforeEach(() => {
    storage = createLocalStorageStub();
    vi.stubGlobal('localStorage', storage);

    resetVpnClient();
    mock = new MockVpnClient();
    createVpnClient(mock);

    // Set up logged-in state
    storage.setItem(TOKEN_KEY, 'active-token');
    storage.setItem(REFRESH_KEY, 'active-refresh');
    useAuthStore.setState({
      token: 'active-token',
      refreshToken: 'active-refresh',
      user: { email: 'user@example.com', plan: 'pro' },
      isLoggedIn: true,
      isLoading: false,
    });

    vi.clearAllMocks();
  });

  it('test_auth_logout_stops_vpn_clears_session — logout() calls vpnClient.disconnect() and clears tokens', async () => {
    // The enhanced logout should disconnect VPN and clear session
    useAuthStore.getState().logout();

    // Should clear localStorage tokens
    expect(storage.removeItem).toHaveBeenCalledWith(TOKEN_KEY);
    expect(storage.removeItem).toHaveBeenCalledWith(REFRESH_KEY);

    // Should clear auth token
    expect(setAuthToken).toHaveBeenCalledWith(null);

    // Should reset store state
    const state = useAuthStore.getState();
    expect(state.token).toBeNull();
    expect(state.refreshToken).toBeNull();
    expect(state.user).toBeNull();
    expect(state.isLoggedIn).toBe(false);

    // In the enhanced version, logout should also disconnect VPN
    // This will fail because current logout() doesn't call vpnClient.disconnect()
    expect(mock.disconnectCalls).toBeGreaterThanOrEqual(1);
  });

  it('test_auth_logout_calls_api — logout() calls cloudApi.logout()', async () => {
    vi.mocked(cloudApi.logout).mockResolvedValue({
      code: 0,
      message: 'ok',
    });

    useAuthStore.getState().logout();

    // Enhanced logout should call the API endpoint to invalidate server-side session
    expect(cloudApi.logout).toHaveBeenCalledOnce();
  });
});
