/**
 * Tests for the 住宅IP chip in CloudTunnelList.
 *
 * Verifies:
 *   - A tunnel with ipType='residential' shows the 住宅IP label
 *   - Tunnels with ipType='non_residential', 'unknown', or undefined do NOT show it
 */
import { screen, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';
import { useConnectionStore } from '../../stores/connection.store';

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

const makeTunnel = (id: number, name: string, country: string, ipType?: string) => ({
  id,
  domain: `${name.toLowerCase().replace(/\s+/g, '-')}.example.com`,
  name,
  protocol: 'k2s',
  port: 443,
  serverUrl: 'k2v5://user:token@server.example.com:443',
  node: { country, ipv4: '1.2.3.4', ipv6: '', isAlive: true, load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0, name, region: country },
  recommendScore: 0.5,
  ...(ipType !== undefined ? { ipType } : {}),
});

const defaultProps = {
  selectedDomain: null,
  onSelect: vi.fn(),
};

// --- Tests ---

describe('CloudTunnelList — 住宅IP chip', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthenticated = true;
    mockVPNState.state = 'idle';
    mockCacheGet.mockReturnValue(null);
    useConnectionStore.setState({ cloudAccessRevoked: false });
  });

  it('renders 住宅IP chip when tunnel ipType is "residential"', async () => {
    const response: TunnelListResponse = {
      items: [makeTunnel(1, 'Tokyo-01', 'JP', 'residential')] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());
    // i18n renders the key from zh-CN locale: 住宅IP
    expect(screen.getByText('住宅IP')).toBeInTheDocument();
  });

  it('does NOT render 住宅IP chip when ipType is "non_residential"', async () => {
    const response: TunnelListResponse = {
      items: [makeTunnel(1, 'Singapore-01', 'SG', 'non_residential')] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(screen.getByText('Singapore-01')).toBeInTheDocument());
    expect(screen.queryByText('住宅IP')).not.toBeInTheDocument();
  });

  it('does NOT render 住宅IP chip when ipType is "unknown"', async () => {
    const response: TunnelListResponse = {
      items: [makeTunnel(1, 'LA-01', 'US', 'unknown')] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(screen.getByText('LA-01')).toBeInTheDocument());
    expect(screen.queryByText('住宅IP')).not.toBeInTheDocument();
  });

  it('does NOT render 住宅IP chip when ipType is absent (undefined)', async () => {
    const response: TunnelListResponse = {
      items: [makeTunnel(1, 'Seoul-01', 'KR')] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(screen.getByText('Seoul-01')).toBeInTheDocument());
    expect(screen.queryByText('住宅IP')).not.toBeInTheDocument();
  });

  it('renders chip only for residential tunnel when mixed list is shown', async () => {
    const response: TunnelListResponse = {
      items: [
        makeTunnel(1, 'Tokyo-01', 'JP', 'residential'),
        makeTunnel(2, 'Singapore-01', 'SG', 'non_residential'),
        makeTunnel(3, 'Seoul-01', 'KR', 'unknown'),
        makeTunnel(4, 'LA-01', 'US'),
      ] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(screen.getByText('Tokyo-01')).toBeInTheDocument());
    // Only one residential chip should appear
    const chips = screen.getAllByText('住宅IP');
    expect(chips).toHaveLength(1);
    // Other tunnels present
    expect(screen.getByText('Singapore-01')).toBeInTheDocument();
    expect(screen.getByText('Seoul-01')).toBeInTheDocument();
    expect(screen.getByText('LA-01')).toBeInTheDocument();
  });

  it('uses the new /api/v20260717/tunnels endpoint', async () => {
    const response: TunnelListResponse = {
      items: [makeTunnel(1, 'Tokyo-01', 'JP')] as any,
    };
    mockCloudApiGet.mockResolvedValue({ code: 0, data: response });

    render(<CloudTunnelList {...defaultProps} />);

    await waitFor(() => expect(mockCloudApiGet).toHaveBeenCalled());
    expect(mockCloudApiGet).toHaveBeenCalledWith('/api/v20260717/tunnels');
    expect(mockCloudApiGet).not.toHaveBeenCalledWith('/api/tunnels/k2v4');
  });
});
