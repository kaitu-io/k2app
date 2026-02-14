import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useServersStore } from '../servers.store';

// Mock cloudApi
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getTunnels: vi.fn(),
  },
}));

import { cloudApi } from '../../api/cloud';

const mockServersData = [
  {
    id: 'sv-1',
    name: 'Tokyo #1',
    country: 'Japan',
    countryCode: 'JP',
    city: 'Tokyo',
    wireUrl: 'wg://tokyo-1.example.com',
    load: 42,
  },
  {
    id: 'sv-2',
    name: 'Singapore #1',
    country: 'Singapore',
    countryCode: 'SG',
    wireUrl: 'wg://sg-1.example.com',
    load: 78,
  },
  {
    id: 'sv-3',
    name: 'US West #1',
    country: 'United States',
    countryCode: 'US',
    city: 'Los Angeles',
    wireUrl: 'wg://us-west-1.example.com',
  },
];

describe('useServersStore', () => {
  beforeEach(() => {
    useServersStore.setState({
      servers: [],
      selectedServerId: null,
      isLoading: false,
      error: null,
    });
    vi.clearAllMocks();
  });

  describe('fetchServers', () => {
    it('loads and stores server list', async () => {
      vi.mocked(cloudApi.getTunnels).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockServersData,
      });

      await useServersStore.getState().fetchServers();

      const state = useServersStore.getState();
      expect(state.servers).toHaveLength(3);
      expect(state.servers[0]).toEqual({
        id: 'sv-1',
        name: 'Tokyo #1',
        country: 'Japan',
        countryCode: 'JP',
        city: 'Tokyo',
        wireUrl: 'wg://tokyo-1.example.com',
        load: 42,
      });
      expect(state.isLoading).toBe(false);
      expect(state.error).toBeNull();
    });

    it('auto-selects first server if none selected', async () => {
      vi.mocked(cloudApi.getTunnels).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockServersData,
      });

      await useServersStore.getState().fetchServers();

      expect(useServersStore.getState().selectedServerId).toBe('sv-1');
    });

    it('does not override existing selection on fetch', async () => {
      useServersStore.setState({ selectedServerId: 'sv-2' });

      vi.mocked(cloudApi.getTunnels).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockServersData,
      });

      await useServersStore.getState().fetchServers();

      expect(useServersStore.getState().selectedServerId).toBe('sv-2');
    });

    it('handles error', async () => {
      vi.mocked(cloudApi.getTunnels).mockRejectedValue(
        new Error('Network error')
      );

      await useServersStore.getState().fetchServers();

      const state = useServersStore.getState();
      expect(state.servers).toHaveLength(0);
      expect(state.isLoading).toBe(false);
      expect(state.error).toBe('Network error');
    });

    it('handles non-Error thrown values', async () => {
      vi.mocked(cloudApi.getTunnels).mockRejectedValue('unknown');

      await useServersStore.getState().fetchServers();

      expect(useServersStore.getState().error).toBe('Failed to load servers');
    });

    it('sets isLoading true during fetch', async () => {
      let resolvePromise: (value: unknown) => void;
      const pending = new Promise((resolve) => {
        resolvePromise = resolve;
      });

      vi.mocked(cloudApi.getTunnels).mockReturnValue(pending as never);

      const fetchPromise = useServersStore.getState().fetchServers();
      expect(useServersStore.getState().isLoading).toBe(true);

      resolvePromise!({ code: 0, message: 'ok', data: [] });
      await fetchPromise;

      expect(useServersStore.getState().isLoading).toBe(false);
    });
  });

  describe('selectServer', () => {
    it('updates selectedServerId', () => {
      useServersStore.getState().selectServer('sv-3');
      expect(useServersStore.getState().selectedServerId).toBe('sv-3');
    });
  });

  describe('getSelectedServer', () => {
    it('returns the correct server', () => {
      useServersStore.setState({
        servers: mockServersData.map((s) => ({ ...s, latency: undefined })),
        selectedServerId: 'sv-2',
      });

      const selected = useServersStore.getState().getSelectedServer();
      expect(selected).toBeDefined();
      expect(selected!.id).toBe('sv-2');
      expect(selected!.name).toBe('Singapore #1');
    });

    it('returns undefined when no server selected', () => {
      useServersStore.setState({ servers: mockServersData.map((s) => ({ ...s, latency: undefined })), selectedServerId: null });

      const selected = useServersStore.getState().getSelectedServer();
      expect(selected).toBeUndefined();
    });
  });
});
