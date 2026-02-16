import { create } from 'zustand';
import { cloudApi } from '../api/cloud';

export interface UserInfo {
  id: string;
  email: string;
  nickname?: string;
  avatar?: string;
  membership?: {
    plan: string;
    status: string;
    expireAt: string;
  };
}

export interface UserStore {
  user: UserInfo | null;
  isLoading: boolean;
  error: string | null;

  init: () => Promise<void>;
  refresh: () => Promise<void>;
  getMembershipStatus: () => string | null;
}

export const useUserStore = create<UserStore>((set, get) => ({
  user: null,
  isLoading: false,
  error: null,

  init: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getUserInfo();
      const user = resp.data as UserInfo;
      set({ user, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to load user info',
      });
    }
  },

  refresh: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getUserInfo();
      const user = resp.data as UserInfo;
      set({ user, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to refresh user info',
      });
    }
  },

  getMembershipStatus: () => {
    const { user } = get();
    if (!user || !user.membership) return null;
    return user.membership.status;
  },
}));
