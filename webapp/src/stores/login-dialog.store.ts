import { create } from 'zustand';

export interface LoginDialogStore {
  isOpen: boolean;
  trigger: string | null;
  message: string | null;

  open: (trigger?: string, message?: string) => void;
  close: () => void;
}

export const useLoginDialogStore = create<LoginDialogStore>((set) => ({
  isOpen: false,
  trigger: null,
  message: null,

  open: (trigger?: string, message?: string) => {
    set({
      isOpen: true,
      trigger: trigger ?? null,
      message: message ?? null,
    });
  },

  close: () => {
    set({
      isOpen: false,
      trigger: null,
      message: null,
    });
  },
}));
