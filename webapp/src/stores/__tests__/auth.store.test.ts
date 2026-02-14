// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useAuthStore } from '../auth.store';

// Mock cloudApi and setAuthToken
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getAuthCode: vi.fn(),
    login: vi.fn(),
    refreshToken: vi.fn(),
    getUserInfo: vi.fn(),
  },
  setAuthToken: vi.fn(),
}));

// Mock getVpnClient
vi.mock('../../vpn-client', () => ({
  getVpnClient: vi.fn(() => ({
    getUDID: vi.fn().mockResolvedValue('mock-udid-123'),
  })),
}));

import { cloudApi, setAuthToken } from '../../api/cloud';
import { getVpnClient } from '../../vpn-client';

const TOKEN_KEY = 'k2_auth_token';
const REFRESH_KEY = 'k2_refresh_token';

// In-memory localStorage stub for Node 25 compatibility
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

describe('useAuthStore', () => {
  let storage: ReturnType<typeof createLocalStorageStub>;

  beforeEach(() => {
    // Stub localStorage globally (Node 25 does not provide full Web Storage API)
    storage = createLocalStorageStub();
    vi.stubGlobal('localStorage', storage);

    // Reset zustand store state
    useAuthStore.setState({
      token: null,
      refreshToken: null,
      user: null,
      isLoggedIn: false,
      isLoading: false,
    });

    // Clear all mocks
    vi.clearAllMocks();
  });

  describe('getAuthCode', () => {
    it('calls cloudApi.getAuthCode with the email', async () => {
      vi.mocked(cloudApi.getAuthCode).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: null,
      });

      await useAuthStore.getState().getAuthCode('user@example.com');

      expect(cloudApi.getAuthCode).toHaveBeenCalledWith('user@example.com');
    });

    it('propagates errors from cloudApi', async () => {
      vi.mocked(cloudApi.getAuthCode).mockRejectedValue(new Error('Rate limited'));

      await expect(
        useAuthStore.getState().getAuthCode('user@example.com')
      ).rejects.toThrow('Rate limited');
    });
  });

  describe('login', () => {
    it('calls cloudApi.login with email, code, and udid, then stores token', async () => {
      vi.mocked(cloudApi.login).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: { token: 'access-token-abc', refreshToken: 'refresh-token-xyz' },
      });

      await useAuthStore.getState().login('user@example.com', '123456');

      // Should have called getVpnClient to get UDID
      expect(getVpnClient).toHaveBeenCalled();

      // Should call login with correct args (including UDID from mock)
      expect(cloudApi.login).toHaveBeenCalledWith(
        'user@example.com',
        '123456',
        'mock-udid-123'
      );

      // Should set auth token
      expect(setAuthToken).toHaveBeenCalledWith('access-token-abc');

      // Should store in localStorage
      expect(storage.setItem).toHaveBeenCalledWith(TOKEN_KEY, 'access-token-abc');
      expect(storage.setItem).toHaveBeenCalledWith(REFRESH_KEY, 'refresh-token-xyz');

      // Should update store state
      const state = useAuthStore.getState();
      expect(state.token).toBe('access-token-abc');
      expect(state.refreshToken).toBe('refresh-token-xyz');
      expect(state.user).toEqual({ email: 'user@example.com' });
      expect(state.isLoggedIn).toBe(true);
      expect(state.isLoading).toBe(false);
    });

    it('sets isLoading during login and clears on error', async () => {
      vi.mocked(cloudApi.login).mockRejectedValue(new Error('Invalid code'));

      await expect(
        useAuthStore.getState().login('user@example.com', 'bad')
      ).rejects.toThrow('Invalid code');

      expect(useAuthStore.getState().isLoading).toBe(false);
    });
  });

  describe('logout', () => {
    it('clears token, localStorage, and resets state', () => {
      // Set up logged-in state
      storage.setItem(TOKEN_KEY, 'token');
      storage.setItem(REFRESH_KEY, 'refresh');
      useAuthStore.setState({
        token: 'token',
        refreshToken: 'refresh',
        user: { email: 'user@example.com' },
        isLoggedIn: true,
      });

      useAuthStore.getState().logout();

      expect(storage.removeItem).toHaveBeenCalledWith(TOKEN_KEY);
      expect(storage.removeItem).toHaveBeenCalledWith(REFRESH_KEY);
      expect(setAuthToken).toHaveBeenCalledWith(null);

      const state = useAuthStore.getState();
      expect(state.token).toBeNull();
      expect(state.refreshToken).toBeNull();
      expect(state.user).toBeNull();
      expect(state.isLoggedIn).toBe(false);
    });
  });

  describe('restoreSession', () => {
    it('does nothing if no tokens in localStorage', async () => {
      await useAuthStore.getState().restoreSession();

      expect(cloudApi.getUserInfo).not.toHaveBeenCalled();
      expect(useAuthStore.getState().isLoggedIn).toBe(false);
    });

    it('restores session from localStorage and validates with getUserInfo', async () => {
      storage.setItem(TOKEN_KEY, 'saved-token');
      storage.setItem(REFRESH_KEY, 'saved-refresh');

      vi.mocked(cloudApi.getUserInfo).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: { email: 'user@example.com', plan: 'pro' },
      });

      await useAuthStore.getState().restoreSession();

      expect(setAuthToken).toHaveBeenCalledWith('saved-token');
      expect(cloudApi.getUserInfo).toHaveBeenCalled();

      const state = useAuthStore.getState();
      expect(state.token).toBe('saved-token');
      expect(state.refreshToken).toBe('saved-refresh');
      expect(state.user).toEqual({ email: 'user@example.com', plan: 'pro' });
      expect(state.isLoggedIn).toBe(true);
    });

    it('refreshes token when getUserInfo fails, then retries', async () => {
      storage.setItem(TOKEN_KEY, 'expired-token');
      storage.setItem(REFRESH_KEY, 'valid-refresh');

      vi.mocked(cloudApi.getUserInfo)
        .mockRejectedValueOnce(new Error('Unauthorized'))
        .mockResolvedValueOnce({
          code: 0,
          message: 'ok',
          data: { email: 'user@example.com' },
        });

      vi.mocked(cloudApi.refreshToken).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: { token: 'new-token', refreshToken: 'new-refresh' },
      });

      await useAuthStore.getState().restoreSession();

      expect(cloudApi.refreshToken).toHaveBeenCalledWith('valid-refresh');
      expect(storage.setItem).toHaveBeenCalledWith(TOKEN_KEY, 'new-token');
      expect(storage.setItem).toHaveBeenCalledWith(REFRESH_KEY, 'new-refresh');

      const state = useAuthStore.getState();
      expect(state.token).toBe('new-token');
      expect(state.refreshToken).toBe('new-refresh');
      expect(state.isLoggedIn).toBe(true);
    });

    it('calls logout when both getUserInfo and refresh fail', async () => {
      storage.setItem(TOKEN_KEY, 'expired-token');
      storage.setItem(REFRESH_KEY, 'expired-refresh');

      vi.mocked(cloudApi.getUserInfo).mockRejectedValue(new Error('Unauthorized'));
      vi.mocked(cloudApi.refreshToken).mockRejectedValue(new Error('Refresh expired'));

      await useAuthStore.getState().restoreSession();

      expect(storage.removeItem).toHaveBeenCalledWith(TOKEN_KEY);
      expect(storage.removeItem).toHaveBeenCalledWith(REFRESH_KEY);
      expect(useAuthStore.getState().isLoggedIn).toBe(false);
    });
  });
});
