/**
 * CloudTunnelList — pinned / recent sections.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/CloudTunnelList.pinned.test.tsx
 */
import { screen, waitFor, fireEvent, within } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';
import { useConnectionStore } from '../../stores/connection.store';
import { usePinnedTunnelsStore, PIN_LIMIT } from '../../stores/pinned-tunnels.store';

// --- mocks: 与 CloudTunnelList.test.tsx 完全一致 (auth.store / vpn-machine.store /
// cache-store / cloud-api / utils/country / RecommendBar) ---

const mockAuthState = { isAuthenticated: true };
const mockVPNState = { state: 'idle' as string };

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

// name-/flag- prefixes (not identity) so the dialog's country-list rows
// (lowercased via buildCountryList) never collide textually with the
// tunnel-row icon/secondary text rendered from raw (non-lowercased) codes.
vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => `name-${code}`,
  getFlagIcon: (code: string) => `flag-${code}`,
}));

vi.mock('../RecommendBar', () => ({
  RecommendBar: () => <div data-testid="recommend-bar" />,
}));

// CountryFilterDialog renders a real MUI Dialog; jsdom crashes on
// ModalManager/getComputedStyle for the real Modal shell. Mock only the
// Dialog/Portal shell — same pattern as CountryFilterDialog.test.tsx /
// LoginDialog.test.tsx — List/Checkbox/Button etc. stay real.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

// --- Helpers ---

const makeTunnel = (id: number, name: string, country: string) => ({
  id,
  domain: `${name.toLowerCase()}.example.com`,
  name,
  serverUrl: 'https://server.example.com',
  node: { country },
  recommendScore: 0.5,
});

const tokyo = makeTunnel(1, 'Tokyo-01', 'JP');
const osaka = makeTunnel(2, 'Osaka-01', 'JP');
const hk = makeTunnel(3, 'HongKong-01', 'HK');

const cachedResponse: TunnelListResponse = {
  items: [tokyo, osaka, hk] as any,
  echConfigList: 'ech',
};

const entry = (t: { domain: string; name: string; node: { country: string } }) =>
  ({ key: 'domain:' + t.domain, domain: t.domain, name: t.name, country: t.node.country, at: 1 });
const gone = { key: '203.0.113.9', domain: 'gone.example.com', name: 'Gone-01', country: 'SG', at: 1 };

const defaultProps = { selectedDomain: null, onSelect: vi.fn(), hideHeader: true };

describe('CloudTunnelList pinned / recent', () => {
  beforeEach(() => {
    (window as any)._platform = { storage: { get: vi.fn(), set: vi.fn().mockResolvedValue(undefined), remove: vi.fn().mockResolvedValue(undefined) } };
    mockCacheGet.mockReturnValue(cachedResponse);
    mockCloudApiGet.mockResolvedValue({ code: 0, data: cachedResponse });
    useConnectionStore.setState({ excludedCountries: [], cloudAccessRevoked: false });
    usePinnedTunnelsStore.setState({ pinned: [], recent: [], loaded: true });
    defaultProps.onSelect.mockReset();
  });

  it('each tunnel renders once: pinned / recent rows are not repeated in the full list', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo)], recent: [entry(hk)] });
    render(<CloudTunnelList {...defaultProps} selectedDomain="tokyo-01.example.com" />);
    await screen.findByTestId('pinned-section');
    expect(screen.getAllByText('Tokyo-01')).toHaveLength(1);
    expect(screen.getAllByText('HongKong-01')).toHaveLength(1);
    expect(screen.getAllByText('Osaka-01')).toHaveLength(1);
    // exactly one checked radio for the selected tunnel
    const checked = screen.getAllByRole('radio').filter(r => (r as HTMLInputElement).checked);
    expect(checked).toHaveLength(1);
    expect((checked[0] as HTMLInputElement).value).toBe('tokyo-01.example.com');
  });

  it('no pins and no recent → no sections', async () => {
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('HongKong-01')).toBeInTheDocument());
    expect(screen.queryByTestId('pinned-section')).not.toBeInTheDocument();
    expect(screen.queryByTestId('recent-section')).not.toBeInTheDocument();
    expect(screen.queryByTestId('all-section-header')).not.toBeInTheDocument();
  });

  it('one pin → section with the single-pin hint', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo)] });
    render(<CloudTunnelList {...defaultProps} />);
    const hint = await screen.findByTestId('pinned-hint');
    expect(hint).toHaveTextContent('只固定了 1 个节点');
    expect(screen.getByTestId('pinned-section')).toHaveTextContent('Tokyo-01');
  });

  it('two pins → multi hint with the count', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo), entry(osaka)] });
    render(<CloudTunnelList {...defaultProps} />);
    expect(await screen.findByTestId('pinned-hint')).toHaveTextContent('只在这 2 个节点之间切换');
  });

  it('star click pins without selecting the row', async () => {
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('HongKong-01')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('pin-btn-hongkong-01.example.com'));
    expect(defaultProps.onSelect).not.toHaveBeenCalled();
    expect(usePinnedTunnelsStore.getState().pinned.map(p => p.domain)).toEqual(['hongkong-01.example.com']);
    expect(await screen.findByTestId('pinned-section')).toHaveTextContent('HongKong-01');
  });

  it('star on a pinned row unpins', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo), entry(osaka)] });
    render(<CloudTunnelList {...defaultProps} />);
    await screen.findByTestId('pinned-section');
    fireEvent.click(screen.getAllByTestId('pin-btn-tokyo-01.example.com')[0]);
    expect(usePinnedTunnelsStore.getState().pinned.map(p => p.domain)).toEqual(['osaka-01.example.com']);
  });

  it('pinned row selects its tunnel on click', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(osaka)] });
    render(<CloudTunnelList {...defaultProps} />);
    const section = await screen.findByTestId('pinned-section');
    fireEvent.click(within(section).getByText('Osaka-01'));
    expect(defaultProps.onSelect).toHaveBeenCalledWith(expect.objectContaining({ domain: 'osaka-01.example.com' }), 'ech');
  });

  it('tunnels sharing a node IP read as one pinned node', async () => {
    const a = { ...makeTunnel(10, 'Seoul-01', 'KR'), node: { country: 'KR', ipv4: '198.51.100.7' } };
    const b = { ...makeTunnel(11, 'Seoul-01b', 'KR'), node: { country: 'KR', ipv4: '198.51.100.7' } };
    const resp = { items: [a, b], echConfigList: 'ech' } as any;
    mockCacheGet.mockReturnValue(resp);
    mockCloudApiGet.mockResolvedValue({ code: 0, data: resp });
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('Seoul-01b')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('pin-btn-seoul-01.example.com'));
    expect(usePinnedTunnelsStore.getState().pinned.map(p => p.key)).toEqual(['198.51.100.7']);
    const stars = screen.getAllByTestId('pin-btn-seoul-01b.example.com');
    expect(stars.every(s => s.getAttribute('aria-pressed') === 'true')).toBe(true);
  });

  it('a pin missing from the list renders unavailable and is not selectable', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo), gone] });
    render(<CloudTunnelList {...defaultProps} />);
    const row = await screen.findByTestId('pinned-unavailable-gone.example.com');
    expect(row).toHaveTextContent('暂不可用');
    fireEvent.click(within(row).getByText('Gone-01'));
    expect(defaultProps.onSelect).not.toHaveBeenCalled();
  });

  it('two pins with one offline → hint says this one waits (no switch possible)', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo), gone] });
    render(<CloudTunnelList {...defaultProps} />);
    expect(await screen.findByTestId('pinned-hint')).toHaveTextContent('其他固定节点暂不可用');
  });

  it('sections present → "all servers" header above Auto; absent otherwise', async () => {
    usePinnedTunnelsStore.setState({ pinned: [entry(tokyo)] });
    render(<CloudTunnelList {...defaultProps} />);
    expect(await screen.findByTestId('all-section-header')).toHaveTextContent('全部节点');
  });

  it('all pins missing → allUnavailable warning replaces the hint', async () => {
    usePinnedTunnelsStore.setState({ pinned: [gone] });
    render(<CloudTunnelList {...defaultProps} />);
    expect(await screen.findByTestId('pinned-hint')).toHaveTextContent('你固定的节点暂时都不可用');
  });

  it('recent shows only tunnels still in the list; its star pins', async () => {
    usePinnedTunnelsStore.setState({ recent: [entry(hk), gone] });
    render(<CloudTunnelList {...defaultProps} />);
    const section = await screen.findByTestId('recent-section');
    expect(section).toHaveTextContent('HongKong-01');
    expect(section).not.toHaveTextContent('Gone-01');
    fireEvent.click(within(section).getByTestId('pin-btn-hongkong-01.example.com'));
    expect(usePinnedTunnelsStore.getState().pinned.map(p => p.domain)).toEqual(['hongkong-01.example.com']);
  });

  it('at the pin limit, unpinned stars are disabled', async () => {
    usePinnedTunnelsStore.setState({
      pinned: Array.from({ length: PIN_LIMIT }, (_, i) => ({ key: `10.0.0.${i}`, domain: `p${i}.example.com`, name: `P${i}`, country: 'JP', at: i })),
    });
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('HongKong-01')).toBeInTheDocument());
    const star = screen.getByTestId('pin-btn-hongkong-01.example.com');
    expect(star).toBeDisabled();
    // pointer-events:none on the disabled button → the tap hits its wrapper span
    fireEvent.click(star.parentElement!);
    expect(defaultProps.onSelect).not.toHaveBeenCalled();
    expect(usePinnedTunnelsStore.getState().pinned).toHaveLength(PIN_LIMIT);
  });
});
