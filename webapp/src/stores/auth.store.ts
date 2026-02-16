import { create } from 'zustand';
import { cloudApi, setAuthToken } from '../api/cloud';
import { getVpnClient } from '../vpn-client';

interface AuthStore {
  token: string | null;
  refreshToken: string | null;
  user: { email: string; plan?: string } | null;
  isLoggedIn: boolean;
  isLoading: boolean;

  getAuthCode: (email: string) => Promise<void>;
  login: (email: string, code: string) => Promise<void>;
  logout: () => void;
  restoreSession: () => Promise<void>;
}

const TOKEN_KEY = 'k2_auth_token';
const REFRESH_KEY = 'k2_refresh_token';

export const useAuthStore = create<AuthStore>((set, get) => ({
  token: null,
  refreshToken: null,
  user: null,
  isLoggedIn: false,
  isLoading: false,

  getAuthCode: async (email: string) => {
    await cloudApi.getAuthCode(email);
  },

  login: async (email: string, code: string) => {
    set({ isLoading: true });
    try {
      const client = getVpnClient();
      const udid = await client.getUDID();
      const resp = await cloudApi.login(email, code, udid);
      const data = resp.data as { token: string; refreshToken: string };

      localStorage.setItem(TOKEN_KEY, data.token);
      localStorage.setItem(REFRESH_KEY, data.refreshToken);
      setAuthToken(data.token);

      set({
        token: data.token,
        refreshToken: data.refreshToken,
        user: { email },
        isLoggedIn: true,
        isLoading: false,
      });
    } catch (e) {
      set({ isLoading: false });
      throw e;
    }
  },

  logout: () => {
    // Disconnect VPN
    try {
      const client = getVpnClient();
      client.disconnect().catch(() => {});
    } catch {
      // VPN client may not be initialized, ignore
    }

    // Invalidate server-side session (fire-and-forget)
    try {
      const logoutPromise = cloudApi.logout();
      if (logoutPromise && typeof logoutPromise.catch === 'function') {
        logoutPromise.catch(() => {});
      }
    } catch {
      // cloudApi.logout may not be available in some test contexts
    }

    // Clear local tokens
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(REFRESH_KEY);
    setAuthToken(null);
    set({ token: null, refreshToken: null, user: null, isLoggedIn: false });
  },

  restoreSession: async () => {
    const token = localStorage.getItem(TOKEN_KEY);
    const refresh = localStorage.getItem(REFRESH_KEY);
    if (!token || !refresh) return;

    setAuthToken(token);
    try {
      const userResp = await cloudApi.getUserInfo();
      const userData = userResp.data as { email: string; plan?: string };
      set({ token, refreshToken: refresh, user: userData, isLoggedIn: true });
    } catch {
      // Token expired, try refresh
      try {
        const refreshResp = await cloudApi.refreshToken(refresh);
        const data = refreshResp.data as { token: string; refreshToken: string };
        localStorage.setItem(TOKEN_KEY, data.token);
        localStorage.setItem(REFRESH_KEY, data.refreshToken);
        setAuthToken(data.token);

        const userResp = await cloudApi.getUserInfo();
        const userData = userResp.data as { email: string; plan?: string };
        set({ token: data.token, refreshToken: data.refreshToken, user: userData, isLoggedIn: true });
      } catch {
        // Refresh also failed, clear session
        get().logout();
      }
    }
  },
}));
