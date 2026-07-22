import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { useNavigate, useLocation } from 'react-router-dom';
import { render } from '../../test/utils/render';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom after the global
// beforeEach's vi.clearAllMocks() strips window.getComputedStyle's
// mockImplementation). Same pattern as RouterExclusionDialog.test.tsx —
// only the unbind-confirm Dialog (rendered by the real RouterExclusionDialog
// inside RouterPage) needs this for the M4 tests below.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children, onClose, ...props }: any) => (
      open ? (
        <div role="dialog" {...props}>
          {children}
          <button data-testid="dialog-mock-close" onClick={() => onClose?.({}, 'backdropClick')} />
        </div>
      ) : null
    ),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogContentText: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

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
  unauthorized: false,
};
vi.mock('../../stores/router.store', () => ({
  useRouterStore: (sel: any) => (sel ? sel(mockState) : mockState),
  isRouterTakeover: () => false,
  // Real selector semantics (status.slots pass-through) so RouterPage's
  // enterprise dispatch stays consumer-mode unless a test sets status.slots.
  routerSlots: (s: any) => {
    const slots = s?.status?.slots;
    return slots && slots.length > 0 ? slots : null;
  },
  hasSlotAlarm: () => false,
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
    // I2 fix: polling is gated on the route actually being /router (the same
    // signal Layout.tsx uses for isActive), not merely mounted — Layout's
    // keep-alive tabs are hidden by CSS and never unmount, so a mount-only
    // effect never actually stopped the 2s poll after the first visit.
    const { unmount } = render(<RouterPage />, {
      useMemoryRouter: true,
      initialEntries: ['/router'],
    });
    expect(mockState.startPolling).toHaveBeenCalled();
    unmount();
    expect(mockState.stopPolling).toHaveBeenCalled();
  });

  it('I2: stops polling when the Router tab becomes inactive without unmounting (keep-alive)', () => {
    mockState.phase = 'online';
    // Harness mirrors Layout's keep-alive behaviour: RouterPage stays mounted
    // across a route change, only `location.pathname` changes underneath it.
    function Harness() {
      const navigate = useNavigate();
      return (
        <>
          <button data-testid="nav-away" onClick={() => navigate('/')}>away</button>
          <RouterPage />
        </>
      );
    }
    render(<Harness />, { useMemoryRouter: true, initialEntries: ['/router'] });
    expect(mockState.startPolling).toHaveBeenCalledTimes(1);
    expect(mockState.stopPolling).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId('nav-away'));
    // RouterPage is still mounted (Harness didn't unmount it) — only the
    // active-route gate flipped, and that alone must stop the poll.
    expect(mockState.stopPolling).toHaveBeenCalled();
  });

  it('M4: navigates to / once unbind succeeds, instead of stranding a blank /router page', async () => {
    mockState.phase = 'online';
    mockState.unbindRouter.mockResolvedValue(true);
    function LocationProbe() {
      const location = useLocation();
      return <div data-testid="loc">{location.pathname}</div>;
    }
    render(
      <>
        <RouterPage />
        <LocationProbe />
      </>,
      { useMemoryRouter: true, initialEntries: ['/router'] },
    );
    expect(screen.getByTestId('loc')).toHaveTextContent('/router');

    fireEvent.click(screen.getByTestId('router-unbind'));
    fireEvent.click(await screen.findByTestId('router-unbind-confirm'));

    await waitFor(() => expect(mockState.unbindRouter).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/'));
  });

  it('M4: does not navigate away when unbind fails', async () => {
    mockState.phase = 'online';
    mockState.unbindRouter.mockResolvedValue(false);
    function LocationProbe() {
      const location = useLocation();
      return <div data-testid="loc">{location.pathname}</div>;
    }
    render(
      <>
        <RouterPage />
        <LocationProbe />
      </>,
      { useMemoryRouter: true, initialEntries: ['/router'] },
    );

    fireEvent.click(screen.getByTestId('router-unbind'));
    fireEvent.click(await screen.findByTestId('router-unbind-confirm'));

    await waitFor(() => expect(mockState.unbindRouter).toHaveBeenCalled());
    expect(screen.getByTestId('loc')).toHaveTextContent('/router');
  });
});
