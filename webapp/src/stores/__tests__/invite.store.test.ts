import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useInviteStore } from '../invite.store';

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getLatestInviteCode: vi.fn(),
    createInviteCode: vi.fn(),
    updateInviteCodeRemark: vi.fn(),
    getInviteCodes: vi.fn(),
  },
}));

import { cloudApi } from '../../api/cloud';

const mockInviteCode = {
  id: 'inv-1',
  code: 'ABC123',
  remark: 'For friend',
  used: false,
  usedBy: null,
  createdAt: '2026-02-15T00:00:00Z',
};

const mockInviteCodes = [
  mockInviteCode,
  {
    id: 'inv-2',
    code: 'DEF456',
    remark: 'For colleague',
    used: true,
    usedBy: 'someone@example.com',
    createdAt: '2026-02-14T00:00:00Z',
  },
  {
    id: 'inv-3',
    code: 'GHI789',
    remark: '',
    used: false,
    usedBy: null,
    createdAt: '2026-02-13T00:00:00Z',
  },
];

describe('useInviteStore', () => {
  beforeEach(() => {
    useInviteStore.setState({
      latestCode: null,
      codes: [],
      isLoading: false,
      error: null,
    });
    vi.clearAllMocks();
  });

  describe('loadLatest', () => {
    it('test_invite_store_load_latest — loadLatest() calls cloudApi.getLatestInviteCode()', async () => {
      vi.mocked(cloudApi.getLatestInviteCode).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockInviteCode,
      });

      await useInviteStore.getState().loadLatest();

      expect(cloudApi.getLatestInviteCode).toHaveBeenCalledOnce();

      const state = useInviteStore.getState();
      expect(state.latestCode).toEqual(mockInviteCode);
      expect(state.isLoading).toBe(false);
    });
  });

  describe('generateCode', () => {
    it('test_invite_store_generate_code — generateCode() calls cloudApi.createInviteCode()', async () => {
      const newCode = {
        id: 'inv-new',
        code: 'NEW999',
        remark: '',
        used: false,
        usedBy: null,
        createdAt: '2026-02-16T00:00:00Z',
      };

      vi.mocked(cloudApi.createInviteCode).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: newCode,
      });

      await useInviteStore.getState().generateCode();

      expect(cloudApi.createInviteCode).toHaveBeenCalledOnce();

      const state = useInviteStore.getState();
      expect(state.latestCode).toEqual(newCode);
    });
  });

  describe('updateRemark', () => {
    it('test_invite_store_update_remark — updateRemark(id, remark) calls cloudApi.updateInviteCodeRemark()', async () => {
      useInviteStore.setState({ codes: mockInviteCodes });

      vi.mocked(cloudApi.updateInviteCodeRemark).mockResolvedValue({
        code: 0,
        message: 'ok',
      });

      await useInviteStore.getState().updateRemark('inv-1', 'Updated remark');

      expect(cloudApi.updateInviteCodeRemark).toHaveBeenCalledWith('inv-1', 'Updated remark');
    });
  });

  describe('loadAllCodes', () => {
    it('test_invite_store_load_all_codes — loadAllCodes() calls cloudApi.getInviteCodes()', async () => {
      vi.mocked(cloudApi.getInviteCodes).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockInviteCodes,
      });

      await useInviteStore.getState().loadAllCodes();

      expect(cloudApi.getInviteCodes).toHaveBeenCalledOnce();

      const state = useInviteStore.getState();
      expect(state.codes).toHaveLength(3);
      expect(state.codes[0]).toEqual(mockInviteCode);
      expect(state.codes[1]!.used).toBe(true);
    });
  });
});
