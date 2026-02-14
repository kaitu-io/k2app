import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
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
      };
      return map[key] || key;
    },
  }),
}));

// Mock useVpnStore with controllable state
const mockConnect = vi.fn();
const mockDisconnect = vi.fn();

let mockStoreState = {
  state: 'stopped' as string,
  error: null as string | null,
  connect: mockConnect,
  disconnect: mockDisconnect,
};

vi.mock('../../stores/vpn.store', () => ({
  useVpnStore: () => mockStoreState,
}));

// Import after mocks are set up
import { Dashboard } from '../Dashboard';

describe('Dashboard', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  beforeEach(() => {
    mockStoreState = {
      state: 'stopped',
      error: null,
      connect: mockConnect,
      disconnect: mockDisconnect,
    };
  });

  it('shows Connect button in stopped state', () => {
    render(<Dashboard />);
    expect(screen.getByRole('button')).toHaveTextContent('Connect');
  });

  it('shows Disconnected status text in stopped state', () => {
    render(<Dashboard />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
  });

  it('shows Dashboard title', () => {
    render(<Dashboard />);
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
  });

  it('shows error message when error is set', () => {
    mockStoreState = {
      ...mockStoreState,
      state: 'stopped',
      error: 'Network timeout',
    };
    render(<Dashboard />);
    expect(screen.getByText(/Connection Error/)).toBeInTheDocument();
    expect(screen.getByText(/Network timeout/)).toBeInTheDocument();
  });

  it('shows Connected status and uptime when connected', () => {
    mockStoreState = {
      ...mockStoreState,
      state: 'connected',
    };
    render(<Dashboard />);
    expect(screen.getAllByText('Connected').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Uptime')).toBeInTheDocument();
    expect(screen.getByText('00:00:00')).toBeInTheDocument();
  });

  it('does not show uptime when disconnected', () => {
    render(<Dashboard />);
    expect(screen.queryByText('Uptime')).not.toBeInTheDocument();
  });

  it('calls connect with placeholder URL on button click', async () => {
    render(<Dashboard />);
    await userEvent.click(screen.getByRole('button'));
    expect(mockConnect).toHaveBeenCalledWith('k2v5://connect');
  });

  it('calls disconnect when connected and button clicked', async () => {
    mockStoreState = {
      ...mockStoreState,
      state: 'connected',
    };
    render(<Dashboard />);
    await userEvent.click(screen.getByRole('button'));
    expect(mockDisconnect).toHaveBeenCalled();
  });
});
