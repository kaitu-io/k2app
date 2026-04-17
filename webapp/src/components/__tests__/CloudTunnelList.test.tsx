import { screen, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';
import { useProbeStore } from '../../stores/probe.store';
import { sortTunnelsByRecommendation } from '../../utils/tunnel-sort';

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
  sortTunnelsByRecommendation: vi.fn((ts: unknown[]) => ts),
}));

// probe-service.runProbe is a no-op under test — we drive probe.store directly
// so sort/render behaviour is deterministic without needing a daemon bridge.
vi.mock('../../services/probe-service', () => ({
  runProbe: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => code,
  getFlagIcon: (code: string) => code,
}));

vi.mock('../RecommendDot', () => ({
  RecommendDot: () => <div data-testid="recommend-dot" />,
}));

// --- Helpers ---

const makeTunnel = (id: number, name: string, country: string) => ({
  id,
  domain: `${name.toLowerCase()}.example.com`,
  name,
  serverUrl: 'https://server.example.com',
  node: { country },
  instance: { budgetScore: 0, recommendScore: 0.5 },
  recommendScore: 0.5,
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

describe('CloudTunnelList with probe data', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthenticated = true;
    mockVPNState.state = 'idle';
    mockCacheGet.mockReturnValue(null);
    useProbeStore.setState({ results: new Map(), inFlight: new Set(), lastUpdated: 0 });
    // Restore identity behavior by default (vi.clearAllMocks clears implementations).
    vi.mocked(sortTunnelsByRecommendation).mockImplementation((ts: any) => ts);
  });

  it('renders ProbeChip with RTT when store has fresh result', async () => {
    const tunnels: TunnelListResponse = {
      items: [makeTunnel(1, 'Tokyo-01', 'JP')] as any,
      echConfigList: 'ech',
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: tunnels });
    useProbeStore.getState().record([{
      url: 'k2v5://u:t@tokyo-01.example.com:443',
      avgRttMs: 42, minRttMs: 40, maxRttMs: 50, jitterMs: 10, lossRate: 0,
      reachable: true, echoSupported: true, probeScore: 0.7,
      measuredAt: new Date().toISOString(),
    }]);

    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => {
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      expect(screen.getByText(/42\s*ms/)).toBeInTheDocument();
    });
  });

  it('sorts tunnels so higher-probeScore comes first', async () => {
    const tunnels: TunnelListResponse = {
      items: [
        makeTunnel(1, 'A-slow', 'JP'),
        makeTunnel(2, 'B-fast', 'SG'),
      ] as any,
      echConfigList: 'ech',
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: tunnels });

    const now = new Date().toISOString();
    useProbeStore.getState().record([
      { url: 'k2v5://u:t@a-slow.example.com:443',
        avgRttMs: 300, minRttMs: 290, maxRttMs: 330, jitterMs: 40, lossRate: 0,
        reachable: true, echoSupported: true, probeScore: 0.2, measuredAt: now },
      { url: 'k2v5://u:t@b-fast.example.com:443',
        avgRttMs: 20, minRttMs: 18, maxRttMs: 25, jitterMs: 7, lossRate: 0,
        reachable: true, echoSupported: true, probeScore: 0.9, measuredAt: now },
    ]);

    vi.mocked(sortTunnelsByRecommendation).mockImplementation((ts: any, provider: any) =>
      [...ts].sort((a, b) =>
        provider.getRouteQuality(b.domain.toLowerCase()) -
        provider.getRouteQuality(a.domain.toLowerCase())
      )
    );

    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('A-slow')).toBeInTheDocument());

    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('B-fast');
    expect(items[1]).toHaveTextContent('A-slow');
  });
});
