import { create } from 'zustand';

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

export const useUserStore = create<UserStore>(() => ({
  user: null,
  isLoading: false,
  error: null,

  init: async () => { throw new Error('Not implemented'); },
  refresh: async () => { throw new Error('Not implemented'); },
  getMembershipStatus: () => { throw new Error('Not implemented'); },
}));
