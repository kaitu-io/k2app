import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useUiStore } from '../ui.store';

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getAppConfig: vi.fn(),
  },
}));

import { cloudApi } from '../../api/cloud';

const mockAppConfig = {
  version: '0.4.0',
  downloadUrl: 'https://example.com/download',
  features: {
    invite: true,
    purchase: true,
    darkMode: false,
  },
};

describe('useUiStore', () => {
  beforeEach(() => {
    useUiStore.setState({
      alerts: [],
      appConfig: null,
      isLoading: false,
    });
    vi.clearAllMocks();
  });

  describe('alerts', () => {
    it('test_ui_store_alerts_queue — addAlert(alert) pushes to queue, removeAlert(id) removes', () => {
      const alert1 = { id: 'a1', type: 'info' as const, message: 'Info message' };
      const alert2 = { id: 'a2', type: 'error' as const, message: 'Error message' };

      useUiStore.getState().addAlert(alert1);
      expect(useUiStore.getState().alerts).toHaveLength(1);
      expect(useUiStore.getState().alerts[0]).toEqual(alert1);

      useUiStore.getState().addAlert(alert2);
      expect(useUiStore.getState().alerts).toHaveLength(2);

      useUiStore.getState().removeAlert('a1');
      expect(useUiStore.getState().alerts).toHaveLength(1);
      expect(useUiStore.getState().alerts[0]!.id).toBe('a2');
    });
  });

  describe('feature flags', () => {
    it('test_ui_store_feature_flags — feature flags from app config', () => {
      useUiStore.setState({ appConfig: mockAppConfig });

      const flags = useUiStore.getState().getFeatureFlags();
      expect(flags).toEqual({
        invite: true,
        purchase: true,
        darkMode: false,
      });
    });

    it('returns empty object when no config loaded', () => {
      const flags = useUiStore.getState().getFeatureFlags();
      expect(flags).toEqual({});
    });
  });

  describe('loadAppConfig', () => {
    it('test_ui_store_load_app_config — loadAppConfig() calls cloudApi.getAppConfig()', async () => {
      vi.mocked(cloudApi.getAppConfig).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockAppConfig,
      });

      await useUiStore.getState().loadAppConfig();

      expect(cloudApi.getAppConfig).toHaveBeenCalledOnce();

      const state = useUiStore.getState();
      expect(state.appConfig).toEqual(mockAppConfig);
      expect(state.isLoading).toBe(false);
    });
  });
});
