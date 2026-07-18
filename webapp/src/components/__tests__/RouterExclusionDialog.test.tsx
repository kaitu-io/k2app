import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom after the global
// beforeEach's vi.clearAllMocks() strips window.getComputedStyle's mockImplementation).
// Mirrors the pattern used in LoginDialog.test.tsx / PasswordDialog.test.tsx.
// The stub also renders a close affordance that invokes the real `onClose` prop,
// so tests can exercise MUI's backdrop-click/Escape path (→ resolveChoice('cancel'))
// without a real ModalManager.
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

// Spy on the actual store actions the 'proceed' branch dispatches, so the
// no-disconnect assertions on the keep/cancel paths are meaningful (not just
// "the real store happened to no-op because vpnState was idle").
const mockDisconnect = vi.fn();
const mockDisconnectRouter = vi.fn();
vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (sel: any) => sel({ disconnect: mockDisconnect }),
}));
vi.mock('../../stores/router.store', () => ({
  useRouterStore: (sel: any) => sel({ disconnectRouter: mockDisconnectRouter }),
}));

import { useExclusionGuard, RouterExclusionDialog } from '../RouterExclusionDialog';

function Harness({ onResult }: { onResult: (v: boolean) => void }) {
  const exclusion = useExclusionGuard('router-connect');
  return (
    <>
      <button data-testid="trigger" onClick={() => void exclusion.guard(true).then(onResult)}>go</button>
      <button data-testid="trigger-nowarn" onClick={() => void exclusion.guard(false).then(onResult)}>go2</button>
      <RouterExclusionDialog controller={exclusion} />
    </>
  );
}

describe('useExclusionGuard', () => {
  beforeEach(() => {
    mockDisconnect.mockClear();
    mockDisconnectRouter.mockClear();
  });

  it('shouldWarn=false resolves true without dialog', async () => {
    const onResult = vi.fn();
    render(<Harness onResult={onResult} />);
    fireEvent.click(screen.getByTestId('trigger-nowarn'));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
    expect(screen.queryByTestId('router-exclusion-dialog')).toBeNull();
  });

  it('shouldWarn=true shows dialog; proceed resolves true', async () => {
    const onResult = vi.fn();
    render(<Harness onResult={onResult} />);
    fireEvent.click(screen.getByTestId('trigger'));
    await screen.findByTestId('router-exclusion-dialog');
    fireEvent.click(screen.getByTestId('exclusion-proceed'));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
  });

  it('keep-both resolves true without disconnecting either side', async () => {
    const onResult = vi.fn();
    render(<Harness onResult={onResult} />);
    fireEvent.click(screen.getByTestId('trigger'));
    await screen.findByTestId('router-exclusion-dialog');
    fireEvent.click(screen.getByTestId('exclusion-keep'));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
    expect(mockDisconnect).not.toHaveBeenCalled();
    expect(mockDisconnectRouter).not.toHaveBeenCalled();
  });

  it('cancel (onClose backdrop/escape) resolves false without disconnecting either side', async () => {
    const onResult = vi.fn();
    render(<Harness onResult={onResult} />);
    fireEvent.click(screen.getByTestId('trigger'));
    await screen.findByTestId('router-exclusion-dialog');
    fireEvent.click(screen.getByTestId('dialog-mock-close'));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(false));
    expect(mockDisconnect).not.toHaveBeenCalled();
    expect(mockDisconnectRouter).not.toHaveBeenCalled();
  });
});
