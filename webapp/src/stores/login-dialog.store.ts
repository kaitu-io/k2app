import { create } from 'zustand';

export interface LoginDialogStore {
  isOpen: boolean;
  trigger: string | null;
  message: string | null;

  open: (trigger?: string, message?: string) => void;
  close: () => void;
}

export const useLoginDialogStore = create<LoginDialogStore>(() => ({
  isOpen: false,
  trigger: null,
  message: null,

  open: () => { throw new Error('Not implemented'); },
  close: () => { throw new Error('Not implemented'); },
}));
