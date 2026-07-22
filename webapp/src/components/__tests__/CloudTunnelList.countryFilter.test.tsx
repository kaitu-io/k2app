/**
 * CloudTunnelList — Auto-pick country exclusion filter integration.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/CloudTunnelList.countryFilter.test.tsx
 */
import { screen, waitFor, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { CloudTunnelList } from '../CloudTunnelList';
import type { TunnelListResponse } from '../../services/api-types';
import { useConnectionStore } from '../../stores/connection.store';

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

const cachedResponse: TunnelListResponse = {
  items: [makeTunnel(1, 'Tokyo-01', 'JP'), makeTunnel(2, 'HongKong-01', 'HK')] as any,
  echConfigList: 'ech',
};

const defaultProps = { selectedDomain: null, onSelect: vi.fn(), hideHeader: true };

describe('CloudTunnelList country filter', () => {
  beforeEach(() => {
    mockCacheGet.mockReturnValue(cachedResponse);
    mockCloudApiGet.mockResolvedValue({ code: 0, data: cachedResponse });
    useConnectionStore.setState({ excludedCountries: [], cloudAccessRevoked: false });
    defaultProps.onSelect.mockReset();
  });

  it('renders filter button on Auto row; opening dialog does not select Auto', async () => {
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByTestId('auto-country-filter-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('auto-country-filter-btn'));
    expect(defaultProps.onSelect).not.toHaveBeenCalled(); // stopPropagation
    expect(await screen.findByTestId('country-filter-done')).toBeInTheDocument(); // dialog open
  });

  it('shows excluded chip on rows of excluded countries only', async () => {
    useConnectionStore.setState({ excludedCountries: ['hk'] });
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByText('HongKong-01')).toBeInTheDocument());
    const chips = screen.getAllByTestId('auto-excluded-chip');
    expect(chips).toHaveLength(1);
  });

  it('subtitle switches to excludedCount when filter active', async () => {
    useConnectionStore.setState({ excludedCountries: ['hk'] });
    render(<CloudTunnelList {...defaultProps} />);
    // render helper uses the real i18n instance (not test-key passthrough) —
    // zh-CN default resource resolves `auto.excludedCount` to "已排除 {{count}} 个国家/地区".
    await waitFor(() => expect(screen.getByTestId('auto-row-secondary')).toHaveTextContent('已排除 1 个国家/地区'));
  });

  it('toggling a country in the dialog updates the store', async () => {
    render(<CloudTunnelList {...defaultProps} />);
    await waitFor(() => expect(screen.getByTestId('auto-country-filter-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('auto-country-filter-btn'));
    // CountryFilterDialog lowercases codes via buildCountryList; the mocked
    // getCountryName renders 'name-hk' for the HK row inside the dialog.
    fireEvent.click(await screen.findByText('name-hk'));
    expect(useConnectionStore.getState().excludedCountries).toEqual(['hk']);
  });
});
