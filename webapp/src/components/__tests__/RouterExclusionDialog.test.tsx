import { describe, it, expect, vi } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom after the global
// beforeEach's vi.clearAllMocks() strips window.getComputedStyle's mockImplementation).
// Mirrors the pattern used in LoginDialog.test.tsx / PasswordDialog.test.tsx.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children, ...props }: any) => (open ? <div role="dialog" {...props}>{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogContentText: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

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

  it('keep-both resolves true without disconnect; cancel resolves false', async () => {
    const onResult = vi.fn();
    render(<Harness onResult={onResult} />);
    fireEvent.click(screen.getByTestId('trigger'));
    await screen.findByTestId('router-exclusion-dialog');
    fireEvent.click(screen.getByTestId('exclusion-keep'));
    await waitFor(() => expect(onResult).toHaveBeenCalledWith(true));
  });
});
