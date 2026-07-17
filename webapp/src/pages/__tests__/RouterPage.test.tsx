import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';

const mockState: any = {
  phase: 'online',
  router: { name: 'r1', version: '0.4.7', configured: true },
  status: { state: 'connected' },
  discovering: false,
  setupError: null,
  runDiscovery: vi.fn(),
  startPolling: vi.fn(),
  stopPolling: vi.fn(),
  connectRouter: vi.fn(),
  disconnectRouter: vi.fn(),
  setupRouter: vi.fn(),
  unbindRouter: vi.fn(),
};
vi.mock('../../stores/router.store', () => ({
  useRouterStore: (sel: any) => (sel ? sel(mockState) : mockState),
  isRouterTakeover: () => false,
}));
vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (sel: any) => sel({ connectedTunnel: null }),
}));
vi.mock('../../stores/vpn.store', () => ({
  useVpnStore: (sel: any) => sel({ status: { state: 'disconnected' } }),
}));

import RouterPage from '../RouterPage';

beforeEach(() => {
  vi.clearAllMocks();
});

describe('RouterPage', () => {
  it('online: renders connection card + devices + settings', () => {
    mockState.phase = 'online';
    render(<RouterPage />);
    expect(screen.getByTestId('router-connection-card')).toBeInTheDocument();
    expect(screen.getByTestId('router-devices-section')).toBeInTheDocument();
    expect(screen.getByTestId('router-settings-section')).toBeInTheDocument();
  });
  it('unconfigured: renders setup card only', () => {
    mockState.phase = 'unconfigured';
    render(<RouterPage />);
    expect(screen.getByTestId('router-setup-card')).toBeInTheDocument();
    expect(screen.queryByTestId('router-connection-card')).toBeNull();
  });
  it('offline: renders offline state with retry', () => {
    mockState.phase = 'offline';
    render(<RouterPage />);
    expect(screen.getByTestId('router-offline')).toBeInTheDocument();
  });
  it('starts polling on mount, stops on unmount', () => {
    mockState.phase = 'online';
    const { unmount } = render(<RouterPage />);
    expect(mockState.startPolling).toHaveBeenCalled();
    unmount();
    expect(mockState.stopPolling).toHaveBeenCalled();
  });
});
