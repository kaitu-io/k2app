/**
 * 回归：新账号首次购买成功后，隧道列表不重拉，cloudAccessRevoked 一直不放下，
 * 点连接被反复弹回 /purchase（工单 3997、4042）。
 *
 * 用真实 CloudTunnelList + 真实 connection.store，只 mock 网络与缓存；
 * harness 与 Dashboard 的接线一致（同一个 ref + 同一个 hook）。
 */
import { useRef } from 'react';
import { waitFor, act } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList, type CloudTunnelListHandle } from '../../components/CloudTunnelList';
import { useRefreshTunnelsOnMembership } from '../useRefreshTunnelsOnMembership';
import { useConnectionStore } from '../../stores/connection.store';

const mockAuthState = { isAuthenticated: true, sessionEpoch: 1 };
const mockVPNState = { state: 'idle' as string };

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: typeof mockAuthState) => unknown) => selector(mockAuthState),
}));
vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachineStore: (selector: (s: typeof mockVPNState) => unknown) => selector(mockVPNState),
}));

const mockCacheGet = vi.fn();
vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    get: (...a: unknown[]) => mockCacheGet(...a),
    set: vi.fn(),
    delete: vi.fn(),
  },
}));

const mockCloudApiGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: (...a: unknown[]) => mockCloudApiGet(...a) },
}));

vi.mock('../../utils/country', () => ({
  getCountryName: (c: string) => c,
  getFlagIcon: (c: string) => c,
}));
vi.mock('../../components/RecommendBar', () => ({ RecommendBar: () => null }));

const okResponse = {
  items: [{
    id: 1, domain: 'tokyo.example.com', name: 'Tokyo-01', serverUrl: 'https://s.example.com',
    node: { country: 'JP' }, instance: { budgetScore: 0, recommendScore: 0.5 }, recommendScore: 0.5,
  }],
  echConfigList: 'ech',
};

const FUTURE = Math.floor(Date.now() / 1000) + 7 * 86400;
const PAST = Math.floor(Date.now() / 1000) - 86400;

function Harness({ expiredAt }: { expiredAt: number | undefined }) {
  const ref = useRef<CloudTunnelListHandle>(null);
  useRefreshTunnelsOnMembership(ref, expiredAt);
  return <CloudTunnelList ref={ref} selectedDomain={null} onSelect={vi.fn()} />;
}

const tunnelCalls = () =>
  mockCloudApiGet.mock.calls.filter((c) => c[0] === '/api/v20260717/tunnels').length;

describe('useRefreshTunnelsOnMembership', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthenticated = true;
    mockAuthState.sessionEpoch = 1;
    mockVPNState.state = 'idle';
    mockCacheGet.mockReturnValue(null);
    useConnectionStore.setState({ cloudAccessRevoked: false });
  });

  it('402 → 会员变有效 → 再发一次 tunnels 请求并放下 cloudAccessRevoked', async () => {
    mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });
    const { rerender } = render(<Harness expiredAt={undefined} />);

    await waitFor(() => expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true));
    const before = tunnelCalls();

    // 购买入账成功：服务端现在放行
    mockCloudApiGet.mockResolvedValue({ code: 0, data: okResponse });
    await act(async () => {
      rerender(<Harness expiredAt={FUTURE} />);
    });

    await waitFor(() => expect(tunnelCalls()).toBe(before + 1));
    await waitFor(() => expect(useConnectionStore.getState().cloudAccessRevoked).toBe(false));
  });

  it('会员已过期（expiredAt 在过去）→ 不重拉', async () => {
    mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });
    const { rerender } = render(<Harness expiredAt={undefined} />);
    await waitFor(() => expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true));
    const before = tunnelCalls();

    await act(async () => {
      rerender(<Harness expiredAt={PAST} />);
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(tunnelCalls()).toBe(before);
    expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true);
  });

  it('服务端仍返回 402 → 只重拉一次，不自激循环', async () => {
    mockCloudApiGet.mockResolvedValue({ code: 402, message: 'membership expired' });
    const { rerender } = render(<Harness expiredAt={undefined} />);
    await waitFor(() => expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true));
    const before = tunnelCalls();

    await act(async () => {
      rerender(<Harness expiredAt={FUTURE} />);
    });
    await waitFor(() => expect(tunnelCalls()).toBe(before + 1));
    await new Promise((r) => setTimeout(r, 100));
    expect(tunnelCalls()).toBe(before + 1);
    expect(useConnectionStore.getState().cloudAccessRevoked).toBe(true);
  });

  it('未被撤销（cloudAccessRevoked=false）时会员更新不触发额外请求', async () => {
    mockCloudApiGet.mockResolvedValue({ code: 0, data: okResponse });
    const { rerender } = render(<Harness expiredAt={FUTURE} />);
    await waitFor(() => expect(tunnelCalls()).toBeGreaterThan(0));
    const before = tunnelCalls();

    await act(async () => {
      rerender(<Harness expiredAt={FUTURE + 86400} />);
    });
    await new Promise((r) => setTimeout(r, 50));
    expect(tunnelCalls()).toBe(before);
  });
});
