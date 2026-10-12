/**
 * ServiceAlert — macOS "Allow in the Background" blocker guidance (#3911).
 *
 * Real useServiceBlocker + services/service-blocker; only window._platform
 * (the bridge) and the VPN machine store are faked.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ServiceAlert, { __resetServiceAlertGuideForTests } from '../ServiceAlert';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback || key,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

// Repo convention (BetaChannelToggle.test.tsx): MUI's ModalManager breaks in
// jsdom once the global setup's restoreAllMocks clears getComputedStyle.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate, useLocation: () => ({ pathname: '/' }) };
});

const machine = { isServiceDown: true, error: null as { code: number; message: string } | null };
vi.mock('../../stores/vpn-machine.store', () => ({
  useVPNMachine: () => machine,
  useVPNMachineStore: (selector: any) => selector({ initialization: null }),
}));

const K = 'dashboard:dashboard.serviceFailure.backgroundBlocked';
const ITEM = 'ALL NATION CONNECT TECHNOLOGY PTE. LTD.';
const BLOCKED = { kind: 'background-disallowed', itemName: ITEM };

let platform: Record<string, any>;
const renderAlert = () =>
  render(
    <MemoryRouter>
      <ServiceAlert />
    </MemoryRouter>,
  );

beforeEach(() => {
  __resetServiceAlertGuideForTests();
  machine.isServiceDown = true;
  machine.error = null;
  mockNavigate.mockReset();
  platform = {
    getServiceBlocker: vi.fn().mockResolvedValue(BLOCKED),
    openServiceSettings: vi.fn().mockResolvedValue(undefined),
    reinstallService: vi.fn().mockResolvedValue(undefined),
  };
  (window as any)._platform = platform;
});

afterEach(() => {
  vi.useRealTimers();
  delete (window as any)._platform;
});

describe('ServiceAlert blocker guidance', () => {
  it('auto-opens the guide with the switch name and relabels the banner', async () => {
    renderAlert();
    expect(await screen.findByText(`${K}.dialogTitle`)).toBeInTheDocument();
    expect(screen.getByTestId('background-blocked-item-name')).toHaveTextContent(ITEM);
    expect(screen.getByText(`${K}.bannerTitle`)).toBeInTheDocument();
    expect(screen.getByText(`${K}.howToEnable`)).toBeInTheDocument();
    expect(screen.getByText(`${K}.statusWaiting`)).toBeInTheDocument();
  });

  it('banner button reopens the guide instead of reinstalling', async () => {
    renderAlert();
    fireEvent.click(await screen.findByText(`${K}.later`));
    await waitFor(() => expect(screen.queryByText(`${K}.dialogTitle`)).not.toBeInTheDocument());

    fireEvent.click(screen.getByText(`${K}.howToEnable`));
    expect(await screen.findByText(`${K}.dialogTitle`)).toBeInTheDocument();
    expect(platform.reinstallService).not.toHaveBeenCalled();
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('"Open System Settings" goes through the bridge', async () => {
    renderAlert();
    fireEvent.click(await screen.findByText(`${K}.openSettings`));
    expect(platform.openServiceSettings).toHaveBeenCalledTimes(1);
  });

  it('auto-opens only once per app run', async () => {
    const first = renderAlert();
    fireEvent.click(await screen.findByText(`${K}.later`));
    first.unmount();

    renderAlert();
    expect(await screen.findByText(`${K}.bannerTitle`)).toBeInTheDocument();
    expect(screen.queryByText(`${K}.dialogTitle`)).not.toBeInTheDocument();
  });

  it('switch turned on → "recovering"; service back → guide and banner gone', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const view = renderAlert();
    expect(await screen.findByText(`${K}.statusWaiting`)).toBeInTheDocument();

    platform.getServiceBlocker.mockResolvedValue(null); // user flipped the switch
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });
    expect(screen.getByText(`${K}.statusRecovering`)).toBeInTheDocument();
    // Still in guidance mode while the daemon is coming up — not back to "Resolve".
    expect(screen.getByText(`${K}.bannerTitle`)).toBeInTheDocument();

    machine.isServiceDown = false; // daemon reconnected
    view.rerender(
      <MemoryRouter>
        <ServiceAlert />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.queryByText(`${K}.dialogTitle`)).not.toBeInTheDocument());
    expect(screen.queryByText(`${K}.bannerTitle`)).not.toBeInTheDocument();
  });

  it('stops polling once the service is back', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const view = renderAlert();
    await screen.findByText(`${K}.dialogTitle`);
    machine.isServiceDown = false;
    view.rerender(
      <MemoryRouter>
        <ServiceAlert />
      </MemoryRouter>,
    );
    const calls = platform.getServiceBlocker.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(platform.getServiceBlocker.mock.calls.length).toBe(calls);
  });
});

describe('ServiceAlert without a blocker (old shells, Windows, other causes)', () => {
  it('older shell without the capability keeps the reinstall path', async () => {
    delete platform.getServiceBlocker;
    renderAlert();
    fireEvent.click(screen.getByText('Resolve'));
    await waitFor(() => expect(platform.reinstallService).toHaveBeenCalledTimes(1));
    expect(screen.queryByText(`${K}.dialogTitle`)).not.toBeInTheDocument();
  });

  it('older desktop shell whose invoke rejects reads as no blocker', async () => {
    platform.getServiceBlocker.mockRejectedValue(new Error('command get_service_blocker not found'));
    renderAlert();
    await waitFor(() => expect(platform.getServiceBlocker).toHaveBeenCalled());
    expect(screen.getByText('dashboard:dashboard.serviceFailure.title')).toBeInTheDocument();
    expect(screen.queryByText(`${K}.dialogTitle`)).not.toBeInTheDocument();
  });

  it('service up → never asks for a blocker', () => {
    machine.isServiceDown = false;
    renderAlert();
    expect(platform.getServiceBlocker).not.toHaveBeenCalled();
  });
});
