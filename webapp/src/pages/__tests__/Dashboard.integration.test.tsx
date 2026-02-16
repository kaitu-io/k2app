import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        title: 'Dashboard',
        connect: 'Connect',
        disconnect: 'Disconnect',
        connecting: 'Connecting...',
        connected: 'Connected',
        disconnected: 'Disconnected',
        uptime: 'Uptime',
        error: 'Connection Error',
        'common:servers': 'Servers',
        'common:loading': 'Loading...',
        serverPanel: 'Server Selection',
        selectedServer: 'Selected Server',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock vpn store
const mockConnect = vi.fn();
const mockDisconnect = vi.fn();

let mockVpnState = {
  state: 'stopped' as string,
  error: null as string | null,
  connect: mockConnect,
  disconnect: mockDisconnect,
};

vi.mock('../../stores/vpn.store', () => ({
  useVpnStore: () => mockVpnState,
}));

// Mock servers store
const mockFetchServers = vi.fn();
const mockSelectServer = vi.fn();

const testServers = [
  {
    id: 'sv-1',
    name: 'Tokyo Node',
    country: 'Japan',
    countryCode: 'JP',
    city: 'Tokyo',
    wireUrl: 'k2v5://tokyo.example.com',
    load: 45,
  },
  {
    id: 'sv-2',
    name: 'Singapore Node',
    country: 'Singapore',
    countryCode: 'SG',
    wireUrl: 'k2v5://sg.example.com',
    load: 78,
  },
  {
    id: 'sv-3',
    name: 'US West Node',
    country: 'United States',
    countryCode: 'US',
    city: 'Los Angeles',
    wireUrl: 'k2v5://us-west.example.com',
    load: 92,
  },
];

let mockServersState = {
  servers: testServers,
  selectedServerId: 'sv-1' as string | null,
  isLoading: false,
  error: null as string | null,
  fetchServers: mockFetchServers,
  selectServer: mockSelectServer,
  getSelectedServer: () => testServers[0],
};

vi.mock('../../stores/servers.store', () => ({
  useServersStore: () => mockServersState,
}));

// Import after mocks
import { Dashboard } from '../Dashboard';

describe('Dashboard Integration — Server Selection', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  beforeEach(() => {
    mockVpnState = {
      state: 'stopped',
      error: null,
      connect: mockConnect,
      disconnect: mockDisconnect,
    };

    mockServersState = {
      servers: testServers,
      selectedServerId: 'sv-1',
      isLoading: false,
      error: null,
      fetchServers: mockFetchServers,
      selectServer: mockSelectServer,
      getSelectedServer: () => testServers[0],
    };
  });

  it('test_dashboard_shows_server_list — Dashboard renders server list from servers store', () => {
    render(<Dashboard />);

    // All server names should be visible in the server list
    // Tokyo Node appears twice: once in the selected server info card, once in the list
    expect(screen.getAllByText('Tokyo Node').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Singapore Node')).toBeInTheDocument();
    expect(screen.getByText('US West Node')).toBeInTheDocument();

    // Country info should be visible (Tokyo, Japan appears in both card and list)
    expect(screen.getAllByText('Tokyo, Japan').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Singapore')).toBeInTheDocument();
    expect(screen.getByText('Los Angeles, United States')).toBeInTheDocument();

    // fetchServers should be called on mount
    expect(mockFetchServers).toHaveBeenCalled();
  });

  it('test_dashboard_server_selection_connects — Selecting a server and clicking connect triggers VPN connection', async () => {
    const user = userEvent.setup();
    render(<Dashboard />);

    // Click on Singapore Node
    await user.click(screen.getByText('Singapore Node'));

    // selectServer should be called with the server id
    expect(mockSelectServer).toHaveBeenCalledWith('sv-2');

    // Now click the connect button
    const connectButton = screen.getByRole('button', { name: /connect/i });
    await user.click(connectButton);

    // connect should be called with a ClientConfig object (not a plain string)
    expect(mockConnect).toHaveBeenCalled();
    const callArg = mockConnect.mock.calls[0][0];
    expect(typeof callArg).toBe('object');
    expect(callArg.server).toBeDefined();
  });

  it('test_dashboard_vpn_status_display — VPN status (connected/disconnected/connecting) is displayed with correct styling', () => {
    // Test disconnected state
    const { unmount } = render(<Dashboard />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
    unmount();

    // Test connected state
    mockVpnState = { ...mockVpnState, state: 'connected' };
    const { unmount: unmount2 } = render(<Dashboard />);
    // Should show "Connected" status text
    expect(screen.getAllByText('Connected').length).toBeGreaterThanOrEqual(1);
    // Connected state should show uptime
    expect(screen.getByText('Uptime')).toBeInTheDocument();
    unmount2();

    // Test connecting state
    mockVpnState = { ...mockVpnState, state: 'connecting' };
    render(<Dashboard />);
    // "Connecting..." appears in both status text and button label
    expect(screen.getAllByText('Connecting...').length).toBeGreaterThanOrEqual(1);
    // The connect button should be disabled during connecting
    const connectingBtn = screen.getByRole('button', { name: /connecting/i });
    expect(connectingBtn).toBeDisabled();
  });

  it('test_dashboard_shows_selected_server_info — Selected server info shows name and country', () => {
    render(<Dashboard />);

    // The selected server card should show the selected server's details
    const serverInfoSection = screen.getByTestId('selected-server-info');
    expect(serverInfoSection).toBeInTheDocument();

    // Should show selected server name and country
    expect(within(serverInfoSection).getByText('Tokyo Node')).toBeInTheDocument();
    expect(within(serverInfoSection).getByText(/Japan/)).toBeInTheDocument();
  });
});
