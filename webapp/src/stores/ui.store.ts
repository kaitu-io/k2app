import { create } from 'zustand';
import type { AppConfig } from '../api/types';

export interface Alert {
  id: string;
  type: 'info' | 'warning' | 'error' | 'success';
  message: string;
}

export interface UiStore {
  alerts: Alert[];
  appConfig: AppConfig | null;
  isLoading: boolean;

  addAlert: (alert: Alert) => void;
  removeAlert: (id: string) => void;
  getFeatureFlags: () => Record<string, boolean>;
  loadAppConfig: () => Promise<void>;
}

export const useUiStore = create<UiStore>(() => ({
  alerts: [],
  appConfig: null,
  isLoading: false,

  addAlert: () => { throw new Error('Not implemented'); },
  removeAlert: () => { throw new Error('Not implemented'); },
  getFeatureFlags: () => { throw new Error('Not implemented'); },
  loadAppConfig: async () => { throw new Error('Not implemented'); },
}));
