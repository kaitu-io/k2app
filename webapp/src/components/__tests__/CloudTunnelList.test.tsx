import { screen, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';

// --- Mock state objects ---

const mockAuthState = { isAuthenticated: true };
const mockVPNState = { state: 'idle' as string };

// --- Mocks ---

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: typeof mockAuthState) => unknown) => selector(mockAuthState),
}));

vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachineStore: (selector: (s: typeof mockVPNState) => unknown) => selector(mockVPNState),
}));

const mockCacheGet = vi.fn();
const mockCacheSet = vi.fn();
vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    get: (...args: unknown[]) => mockCacheGet(...args),
    set: (...args: unknown[]) => mockCacheSet(...args),
  },
}));

const mockCloudApiGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    get: (...args: unknown[]) => mockCloudApiGet(...args),
  },
}));

vi.mock('../../utils/tunnel-sort', () => ({
  sortTunnelsByRecommendation: (tunnels: unknown[]) => tunnels,
}));

vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => code,
  getFlagIcon: (code: string) => code,
}));

vi.mock('../VerticalLoadBar', () => ({
  VerticalLoadBar: () => <div data-testid="load-bar" />,
}));

// --- Helpers ---

const makeTunnel = (id: number, name: string, country: string) => ({
  id,
  domain: `${name.toLowerCase()}.example.com`,
  name,
  serverUrl: 'https://server.example.com',
  node: { country },
  instance: { budgetScore: 0 },
});

const cachedResponse: TunnelListResponse = {
  items: [
    makeTunnel(1, 'Tokyo-01', 'JP'),
    makeTunnel(2, 'Singapore-01', 'SG'),
  ] as any,
  echConfigList: 'ech-base64',
};

const freshResponse: TunnelListResponse = {
  items: [
    makeTunnel(1, 'Tokyo-01', 'JP'),
    makeTunnel(2, 'Singapore-01', 'SG'),
    makeTunnel(3, 'Los Angeles-01', 'US'),
  ] as any,
  echConfigList: 'ech-base64-fresh',
};

const defaultProps = {
  selectedDomain: null,
  onSelect: vi.fn(),
};

// --- Tests ---

describe('CloudTunnelList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthenticated = true;
    mockVPNState.state = 'idle';
    mockCacheGet.mockReturnValue(null);
    mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });
  });

  describe('SWR: cache hit → immediate render', () => {
    it('should render tunnel list immediately from cache without showing skeleton', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      // Background revalidate resolves later
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      // Tunnels from cache should be visible immediately (no skeleton)
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      expect(screen.getByText('Singapore-01')).toBeInTheDocument();
      // No skeleton should be present
      expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    });

    it('should update list when background revalidate succeeds', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      // Initially 2 tunnels from cache
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();

      // After background refresh, new tunnel appears
      await waitFor(() => {
        expect(screen.getByText('Los Angeles-01')).toBeInTheDocument();
      });
    });

    it('should store cache without TTL', async () => {
      mockCacheGet.mockReturnValue(null);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      await waitFor(() => {
        expect(mockCacheSet).toHaveBeenCalledWith('api:tunnels', freshResponse);
      });
      // Verify no TTL option was passed
      expect(mockCacheSet).not.toHaveBeenCalledWith('api:tunnels', freshResponse, expect.anything());
    });
  });

  describe('SWR: cache hit + background failure → list stays with error indicator', () => {
    it('should keep cached tunnels visible when background refresh returns error code', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'Network error' });

      render(<CloudTunnelList {...defaultProps} />);

      // Cached tunnels should remain
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      expect(screen.getByText('Singapore-01')).toBeInTheDocument();

      // Header should show refresh failed indicator
      await waitFor(() => {
        expect(screen.getByText('刷新失败')).toBeInTheDocument();
      });
    });

    it('should keep cached tunnels visible when background refresh throws network error', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockRejectedValue(new Error('fetch failed'));

      render(<CloudTunnelList {...defaultProps} />);

      // Cached tunnels should remain
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();

      // Header should show refresh failed indicator
      await waitFor(() => {
        expect(screen.getByText('刷新失败')).toBeInTheDocument();
      });
    });
  });

  describe('No cache + loading → skeleton UI', () => {
    it('should render skeleton items while loading with no cache', () => {
      mockCacheGet.mockReturnValue(null);
      // Never resolves — keeps loading state
      mockCloudApiGet.mockReturnValue(new Promise(() => {}));

      render(<CloudTunnelList {...defaultProps} />);

      // Should have 3 skeleton list items
      const skeletons = screen.getAllByRole('listitem');
      expect(skeletons).toHaveLength(3);

      // Should NOT show any real tunnel names
      expect(screen.queryByText('Tokyo-01')).not.toBeInTheDocument();
    });
  });

  describe('No cache + API failure → error UI with retry', () => {
    it('should show error UI when API fails and no cache exists', async () => {
      mockCacheGet.mockReturnValue(null);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'Network error' });

      render(<CloudTunnelList {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('加载节点列表失败')).toBeInTheDocument();
        expect(screen.getByText('重试加载')).toBeInTheDocument();
      });
    });
  });
});
