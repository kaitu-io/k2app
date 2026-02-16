import { create } from 'zustand';
import { cloudApi } from '../api/cloud';
import type { InviteCode } from '../api/types';

export interface InviteStore {
  latestCode: InviteCode | null;
  codes: InviteCode[];
  isLoading: boolean;
  error: string | null;

  loadLatest: () => Promise<void>;
  generateCode: () => Promise<void>;
  updateRemark: (id: string, remark: string) => Promise<void>;
  loadAllCodes: () => Promise<void>;
}

export const useInviteStore = create<InviteStore>((set) => ({
  latestCode: null,
  codes: [],
  isLoading: false,
  error: null,

  loadLatest: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getLatestInviteCode();
      const code = resp.data as InviteCode;
      set({ latestCode: code, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to load latest invite code',
      });
    }
  },

  generateCode: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.createInviteCode();
      const code = resp.data as InviteCode;
      set({ latestCode: code, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to generate invite code',
      });
    }
  },

  updateRemark: async (id: string, remark: string) => {
    try {
      await cloudApi.updateInviteCodeRemark(id, remark);
    } catch (e) {
      set({
        error: e instanceof Error ? e.message : 'Failed to update remark',
      });
    }
  },

  loadAllCodes: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getInviteCodes();
      const codes = (resp.data ?? []) as InviteCode[];
      set({ codes, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to load invite codes',
      });
    }
  },
}));
