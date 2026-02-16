import { create } from 'zustand';
import { cloudApi } from '../api/cloud';
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

export const useUiStore = create<UiStore>((set, get) => ({
  alerts: [],
  appConfig: null,
  isLoading: false,

  addAlert: (alert: Alert) => {
    set((state) => ({ alerts: [...state.alerts, alert] }));
  },

  removeAlert: (id: string) => {
    set((state) => ({ alerts: state.alerts.filter((a) => a.id !== id) }));
  },

  getFeatureFlags: () => {
    const { appConfig } = get();
    if (!appConfig) return {};
    return appConfig.features;
  },

  loadAppConfig: async () => {
    set({ isLoading: true });
    try {
      const resp = await cloudApi.getAppConfig();
      const config = resp.data as AppConfig;
      set({ appConfig: config, isLoading: false });
    } catch (e) {
      set({ isLoading: false });
    }
  },
}));
