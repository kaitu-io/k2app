import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useUserStore } from '../user.store';

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getUserInfo: vi.fn(),
  },
}));

import { cloudApi } from '../../api/cloud';

const mockUserInfo = {
  id: 'user-1',
  email: 'test@example.com',
  nickname: 'TestUser',
  avatar: 'https://example.com/avatar.png',
  membership: {
    plan: 'pro',
    status: 'active',
    expireAt: '2026-12-31T00:00:00Z',
  },
};

describe('useUserStore', () => {
  beforeEach(() => {
    useUserStore.setState({
      user: null,
      isLoading: false,
      error: null,
    });
    vi.clearAllMocks();
  });

  describe('init', () => {
    it('test_user_store_init_loads_profile — init() calls cloudApi.getUserInfo(), sets user data', async () => {
      vi.mocked(cloudApi.getUserInfo).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockUserInfo,
      });

      await useUserStore.getState().init();

      expect(cloudApi.getUserInfo).toHaveBeenCalledOnce();

      const state = useUserStore.getState();
      expect(state.user).toEqual(mockUserInfo);
      expect(state.isLoading).toBe(false);
      expect(state.error).toBeNull();
    });
  });

  describe('getMembershipStatus', () => {
    it('test_user_store_membership_status — membership status derived from user data', () => {
      useUserStore.setState({
        user: mockUserInfo,
        isLoading: false,
        error: null,
      });

      const status = useUserStore.getState().getMembershipStatus();
      expect(status).toBe('active');
    });

    it('returns null when no user loaded', () => {
      useUserStore.setState({
        user: null,
        isLoading: false,
        error: null,
      });

      const status = useUserStore.getState().getMembershipStatus();
      expect(status).toBeNull();
    });
  });

  describe('refresh', () => {
    it('test_user_store_refresh — refresh() reloads user data', async () => {
      // Set initial user data
      useUserStore.setState({
        user: mockUserInfo,
        isLoading: false,
        error: null,
      });

      const updatedUser = {
        ...mockUserInfo,
        nickname: 'UpdatedUser',
        membership: {
          ...mockUserInfo.membership,
          plan: 'enterprise',
        },
      };

      vi.mocked(cloudApi.getUserInfo).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: updatedUser,
      });

      await useUserStore.getState().refresh();

      expect(cloudApi.getUserInfo).toHaveBeenCalledOnce();

      const state = useUserStore.getState();
      expect(state.user).toEqual(updatedUser);
      expect(state.user!.nickname).toBe('UpdatedUser');
    });
  });
});
