import { useRef, useEffect } from 'react';
import { screen, waitFor, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList, type CloudTunnelListHandle } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';
import { AUTO_TUNNEL_DOMAIN, AUTO_TUNNEL_SENTINEL, useConnectionStore } from '../../stores/connection.store';

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
const mockCacheDelete = vi.fn();
vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    get: (...args: unknown[]) => mockCacheGet(...args),
    set: (...args: unknown[]) => mockCacheSet(...args),
    delete: (...args: unknown[]) => mockCacheDelete(...args),
  },
}));

const mockCloudApiGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    get: (...args: unknown[]) => mockCloudApiGet(...args),
  },
}));

vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => code,
  getFlagIcon: (code: string) => code,
}));

vi.mock('../RecommendBar', () => ({
  RecommendBar: () => <div data-testid="recommend-bar" />,
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
    // Reset shared (real) connection store flag so a 402 test doesn't bleed
    // its revoked state into the success-path tests.
    useConnectionStore.setState({ cloudAccessRevoked: false });
  });

  describe('SWR: cache hit → immediate render', () => {
    it('should render tunnel list immediately from cache without showing skeleton', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      expect(screen.getByText('Singapore-01')).toBeInTheDocument();
      expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    });

    it('should update list when background revalidate succeeds', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();

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
      expect(mockCacheSet).not.toHaveBeenCalledWith('api:tunnels', freshResponse, expect.anything());
    });
  });

  describe('SWR: cache hit + background failure → list stays, no user-visible error', () => {
    it('should keep cached tunnels visible when background refresh returns error code (silent)', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'Network error' });

      render(<CloudTunnelList {...defaultProps} />);

      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      expect(screen.getByText('Singapore-01')).toBeInTheDocument();

      // No red "刷新失败" caption — background failures stay silent so
      // users aren't alarmed by transient network blips when their list
      // is still usable from cache.
      await waitFor(() => {
        expect(mockCloudApiGet).toHaveBeenCalled();
      });
      expect(screen.queryByText('刷新失败')).not.toBeInTheDocument();
    });

    it('should keep cached tunnels visible when background refresh throws (silent)', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockRejectedValue(new Error('fetch failed'));

      render(<CloudTunnelList {...defaultProps} />);

      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();

      await waitFor(() => {
        expect(mockCloudApiGet).toHaveBeenCalled();
      });
      expect(screen.queryByText('刷新失败')).not.toBeInTheDocument();
    });
  });

  describe('No cache + loading → skeleton UI', () => {
    it('should render skeleton items while loading with no cache', () => {
      mockCacheGet.mockReturnValue(null);
      mockCloudApiGet.mockReturnValue(new Promise(() => {}));

      render(<CloudTunnelList {...defaultProps} />);

      const skeletons = screen.getAllByRole('listitem');
      expect(skeletons).toHaveLength(3);

      expect(screen.queryByText('Tokyo-01')).not.toBeInTheDocument();
    });
  });

  describe('No cache + API failure → friendly empty state', () => {
    it('shows friendly "cloud nodes unavailable" copy, not alarming "failed" wording', async () => {
      mockCacheGet.mockReturnValue(null);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'Network error' });

      render(<CloudTunnelList {...defaultProps} />);

      await waitFor(() => {
        expect(screen.getByText('暂时无法获取云端节点')).toBeInTheDocument();
      });
      expect(screen.getByText('重试加载')).toBeInTheDocument();

      // Old alarming copy should be gone.
      expect(screen.queryByText('加载节点列表失败')).not.toBeInTheDocument();
    });
  });

  describe('Tunnel ordering', () => {
    it('sorts tunnels alphabetically by country code', async () => {
      const tunnels: TunnelListResponse = {
        items: [
          makeTunnel(1, 'Singapore-01', 'SG'),
          makeTunnel(2, 'Japan-01', 'JP'),
          makeTunnel(3, 'USA-01', 'US'),
        ] as any,
        echConfigList: 'ech',
      };
      mockCloudApiGet.mockResolvedValue({ code: 0, data: tunnels });

      render(<CloudTunnelList {...defaultProps} />);
      await waitFor(() => expect(screen.getByText('Japan-01')).toBeInTheDocument());

      const items = screen.getAllByRole('listitem');
      // items[0] is the Auto virtual row; concrete tunnels follow in alpha order
      expect(items[1]).toHaveTextContent('Japan-01');
      expect(items[2]).toHaveTextContent('Singapore-01');
      expect(items[3]).toHaveTextContent('USA-01');
    });
  });

  describe('Imperative handle: refresh({force})', () => {
    function HarnessRefresh({ onReady }: { onReady: (h: CloudTunnelListHandle) => void }) {
      const ref = useRef<CloudTunnelListHandle>(null);
      useEffect(() => {
        if (ref.current) onReady(ref.current);
      }, [onReady]);
      return <CloudTunnelList ref={ref} {...defaultProps} />;
    }

    it('force=true bypasses cache-hit fast-path and issues a network fetch', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      let handle: CloudTunnelListHandle | null = null;
      render(<HarnessRefresh onReady={(h) => { handle = h; }} />);

      // Initial mount hits cache — no blocking fetch required for render.
      expect(screen.getByText('Tokyo-01')).toBeInTheDocument();
      await waitFor(() => expect(handle).not.toBeNull());

      mockCloudApiGet.mockClear();
      await handle!.refresh({ force: true });

      // Forced call goes through cloudApi even though cache is available.
      expect(mockCloudApiGet).toHaveBeenCalledWith('/api/v20260717/tunnels');
    });

    it('force=true rethrows on non-zero response code', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'oops' });

      let handle: CloudTunnelListHandle | null = null;
      render(<HarnessRefresh onReady={(h) => { handle = h; }} />);
      await waitFor(() => expect(handle).not.toBeNull());

      await expect(handle!.refresh({ force: true })).rejects.toThrow(/code=-1/);
    });

    it('force=true rethrows on thrown fetch error', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockRejectedValue(new Error('network down'));

      let handle: CloudTunnelListHandle | null = null;
      render(<HarnessRefresh onReady={(h) => { handle = h; }} />);
      await waitFor(() => expect(handle).not.toBeNull());

      await expect(handle!.refresh({ force: true })).rejects.toThrow('network down');
    });

    it('default (non-forced) refresh is silent on cache-hit background failure', async () => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: -1, message: 'bg fail' });

      let handle: CloudTunnelListHandle | null = null;
      render(<HarnessRefresh onReady={(h) => { handle = h; }} />);
      await waitFor(() => expect(handle).not.toBeNull());

      // Must not throw — SWR path swallows background errors.
      await expect(handle!.refresh()).resolves.toBeUndefined();
    });
  });

  describe('Auto virtual row', () => {
    beforeEach(() => {
      mockCacheGet.mockReturnValue(cachedResponse);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });
    });

    it('renders Auto row as the first list item', async () => {
      render(<CloudTunnelList {...defaultProps} selectedDomain={AUTO_TUNNEL_DOMAIN} />);
      await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());

      const items = screen.getAllByRole('listitem');
      // Auto row must be first — before any concrete tunnel
      expect(items[0]).toHaveTextContent('自动选择');
    });

    it('marks Auto row radio as checked when selectedDomain === AUTO_TUNNEL_DOMAIN', async () => {
      render(<CloudTunnelList {...defaultProps} selectedDomain={AUTO_TUNNEL_DOMAIN} />);
      await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());

      const radios = screen.getAllByRole('radio') as HTMLInputElement[];
      // First radio belongs to the Auto row
      expect(radios[0].checked).toBe(true);
      // Concrete tunnel radios are unchecked
      expect(radios[1].checked).toBe(false);
    });

    it('does not mark Auto radio when selectedDomain points to a concrete tunnel', async () => {
      const concreteDomain = 'tokyo-01.example.com';
      render(<CloudTunnelList {...defaultProps} selectedDomain={concreteDomain} />);
      await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());

      const radios = screen.getAllByRole('radio') as HTMLInputElement[];
      // Auto row radio (index 0) must NOT be checked
      expect(radios[0].checked).toBe(false);
      // The matching concrete tunnel radio IS checked
      const checkedRadios = radios.filter((r) => r.checked);
      expect(checkedRadios).toHaveLength(1);
      expect(checkedRadios[0].value).toBe(concreteDomain);
    });

    it('calls onSelect with the AUTO_TUNNEL_SENTINEL when Auto row is clicked', async () => {
      const onSelect = vi.fn();
      render(<CloudTunnelList {...defaultProps} onSelect={onSelect} selectedDomain={null} />);
      await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());

      const items = screen.getAllByRole('listitem');
      fireEvent.click(items[0]);

      expect(onSelect).toHaveBeenCalledTimes(1);
      const [tunnelArg] = onSelect.mock.calls[0] as [typeof AUTO_TUNNEL_SENTINEL, string?];
      expect(tunnelArg.domain).toBe(AUTO_TUNNEL_DOMAIN);
      // sentinel identity
      expect(tunnelArg).toBe(AUTO_TUNNEL_SENTINEL);
    });
  });

  describe('402 membership expired → clear tunnels + revoke cloud access', () => {
    it('revokes cloud access and renders the renew CTA, without scheduling a retry', async () => {
      mockCacheGet.mockReturnValue(null); // no cache → blocking fetch path
      mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });

      const onTunnelsLoaded = vi.fn();
      render(<CloudTunnelList {...defaultProps} onTunnelsLoaded={onTunnelsLoaded} />);

      await waitFor(() => {
        expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true);
      });

      // Tunnel cache must be purged so Auto-pick can't connect to a revoked node.
      expect(mockCacheDelete).toHaveBeenCalledWith('api:tunnels');
      // No stale tunnels rendered.
      expect(screen.queryByText('Tokyo-01')).not.toBeInTheDocument();
      // Membership-expired empty state is shown (unique title) with a renew CTA.
      expect(screen.getByText(/会员已过期|Membership expired/i)).toBeInTheDocument();
      expect(screen.getByText(/续费会员|Renew membership/i)).toBeInTheDocument();
    });

    it('on iOS WITHOUT IAP, shows the expired state WITHOUT a renew CTA (Apple 3.1.1: /purchase unregistered)', async () => {
      (window as any)._platform = { os: 'ios' };
      try {
        mockCacheGet.mockReturnValue(null);
        mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });

        render(<CloudTunnelList {...defaultProps} />);

        await waitFor(() => {
          expect(screen.getByText(/会员已过期|Membership expired/i)).toBeInTheDocument();
        });
        // The renew button must not exist on iOS without IAP — it would navigate to a dead route.
        expect(screen.queryByText(/续费会员|Renew membership/i)).not.toBeInTheDocument();
      } finally {
        delete (window as any)._platform;
      }
    });

    it('on iOS WITH StoreKit IAP, DOES show the renew CTA (/purchase registered → IAP panel)', async () => {
      // After the IAP merge, /purchase is registered on iOS when the native
      // StoreKit bridge is injected, so renew is a live path (→ IosSubscribePanel),
      // not a dead route. Gating must match App.tsx/SideNavigation: !(ios && !iap).
      (window as any)._platform = { os: 'ios', iap: {} };
      try {
        mockCacheGet.mockReturnValue(null);
        mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });

        render(<CloudTunnelList {...defaultProps} />);

        await waitFor(() => {
          expect(screen.getByText(/会员已过期|Membership expired/i)).toBeInTheDocument();
        });
        expect(screen.getByText(/续费会员|Renew membership/i)).toBeInTheDocument();
      } finally {
        delete (window as any)._platform;
      }
    });

    it('clears the revoked flag when a later refresh succeeds (membership restored)', async () => {
      useConnectionStore.setState({ cloudAccessRevoked: true });
      mockCacheGet.mockReturnValue(null);
      mockCloudApiGet.mockResolvedValue({ code: 0, data: freshResponse });

      render(<CloudTunnelList {...defaultProps} />);

      await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());
      expect(useConnectionStore.getState().cloudAccessRevoked).toBe(false);
    });
  });
});
