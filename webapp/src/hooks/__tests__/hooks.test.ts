import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useUser } from '../useUser';
import { useShareLink } from '../useShareLink';
import { useInviteCodeActions } from '../useInviteCodeActions';

// Mock the stores
vi.mock('../../stores/user.store', () => ({
  useUserStore: vi.fn(),
}));

vi.mock('../../stores/invite.store', () => ({
  useInviteStore: vi.fn(),
}));

// Mock cloudApi for useShareLink
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    createShareLink: vi.fn(),
    createInviteCode: vi.fn(),
    updateInviteCodeRemark: vi.fn(),
    getInviteCodes: vi.fn(),
    getLatestInviteCode: vi.fn(),
  },
}));

import { useUserStore } from '../../stores/user.store';
import { useInviteStore } from '../../stores/invite.store';
import { cloudApi } from '../../api/cloud';

const mockUserProfile = {
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

describe('hooks', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('useUser', () => {
    it('test_use_user_returns_profile — useUser() returns user profile from user.store', () => {
      vi.mocked(useUserStore).mockReturnValue({
        user: mockUserProfile,
        isLoading: false,
        error: null,
      });

      const { result } = renderHook(() => useUser());

      expect(result.current.user).toEqual(mockUserProfile);
      expect(result.current.user!.email).toBe('test@example.com');
      expect(result.current.isLoading).toBe(false);
    });

    it('returns null user when not loaded', () => {
      vi.mocked(useUserStore).mockReturnValue({
        user: null,
        isLoading: true,
        error: null,
      });

      const { result } = renderHook(() => useUser());

      expect(result.current.user).toBeNull();
      expect(result.current.isLoading).toBe(true);
    });
  });

  describe('useShareLink', () => {
    it('test_use_share_link_generates — useShareLink() generates share link via cloudApi', async () => {
      vi.mocked(cloudApi.createShareLink).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: { url: 'https://kaitu.io/share/ABC123' },
      });

      const { result } = renderHook(() => useShareLink());

      const link = await result.current.generateShareLink('ABC123');

      expect(cloudApi.createShareLink).toHaveBeenCalledWith('ABC123');
      expect(link).toBe('https://kaitu.io/share/ABC123');
    });
  });

  describe('useInviteCodeActions', () => {
    it('test_use_invite_code_actions — useInviteCodeActions() provides code CRUD', () => {
      const mockLoadLatest = vi.fn();
      const mockGenerateCode = vi.fn();
      const mockUpdateRemark = vi.fn();
      const mockLoadAllCodes = vi.fn();

      vi.mocked(useInviteStore).mockReturnValue({
        latestCode: {
          id: 'inv-1',
          code: 'ABC123',
          remark: '',
          used: false,
          usedBy: null,
          createdAt: '2026-02-15T00:00:00Z',
        },
        codes: [],
        isLoading: false,
        error: null,
        loadLatest: mockLoadLatest,
        generateCode: mockGenerateCode,
        updateRemark: mockUpdateRemark,
        loadAllCodes: mockLoadAllCodes,
      });

      const { result } = renderHook(() => useInviteCodeActions());

      // Should expose CRUD actions
      expect(result.current.latestCode).toBeDefined();
      expect(result.current.latestCode!.code).toBe('ABC123');
      expect(typeof result.current.generateCode).toBe('function');
      expect(typeof result.current.updateRemark).toBe('function');
      expect(typeof result.current.loadAllCodes).toBe('function');
    });
  });
});
