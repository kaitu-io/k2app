import { create } from 'zustand';
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

export const useInviteStore = create<InviteStore>(() => ({
  latestCode: null,
  codes: [],
  isLoading: false,
  error: null,

  loadLatest: async () => { throw new Error('Not implemented'); },
  generateCode: async () => { throw new Error('Not implemented'); },
  updateRemark: async () => { throw new Error('Not implemented'); },
  loadAllCodes: async () => { throw new Error('Not implemented'); },
}));
