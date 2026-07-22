import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, waitFor } from '@testing-library/react';
import { render, screen } from '../../test/utils/render';
import type { RouterSlot } from '../../services/vpn-types';

// Mock MUI Dialog/Modal subtree to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom). Mirrors the
// pattern used in LoginDialog.test.tsx / PasswordDialog.test.tsx.
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

import RouterSlotList, { groupDevicesBySlot } from '../RouterSlotList';

const setSsidMock = vi.fn();
const setPasswordMock = vi.fn();
vi.mock('../../services/gateway-core', () => ({
  gatewaySetSlotSsid: (slot: number, ssid: string) => setSsidMock(slot, ssid),
  gatewaySetSlotPassword: (slot: number, password: string) => setPasswordMock(slot, password),
}));

const SLOTS: RouterSlot[] = [
  { slot: 1, ssid: 'overleap-ae-1', country: 'ae', index: 1, state: 'running' },
  { slot: 2, ssid: 'overleap-ae-2', country: 'ae', index: 2, state: 'failClosed', downSince: '2026-07-22T02:00:00Z' },
  { slot: 3, ssid: '', country: '', index: 0, state: 'disabled' },
];

beforeEach(() => {
  vi.clearAllMocks();
  setSsidMock.mockResolvedValue({ code: 0 });
  setPasswordMock.mockResolvedValue({ code: 0 });
});

describe('RouterSlotList', () => {
  it('renders one row per slot with state indicator', () => {
    render(<RouterSlotList slots={SLOTS} />);
    expect(screen.getByText('overleap-ae-1')).toBeInTheDocument();
    expect(screen.getByTestId('slot-2-alarm')).toBeInTheDocument();
    expect(screen.getByTestId('slot-3-disabled')).toBeInTheDocument();
  });

  it('rename flow calls gatewaySetSlotSsid', async () => {
    render(<RouterSlotList slots={SLOTS} />);
    fireEvent.click(screen.getByTestId('slot-1-rename'));
    const input = await screen.findByTestId('slot-rename-input');
    fireEvent.change(input.querySelector('input')!, { target: { value: 'Studio A' } });
    fireEvent.click(screen.getByTestId('slot-rename-confirm'));
    await waitFor(() => expect(setSsidMock).toHaveBeenCalledWith(1, 'Studio A'));
  });

  it('password flow calls gatewaySetSlotPassword', async () => {
    render(<RouterSlotList slots={SLOTS} />);
    fireEvent.click(screen.getByTestId('slot-1-password'));
    const input = await screen.findByTestId('slot-password-input');
    fireEvent.change(input.querySelector('input')!, { target: { value: 'newpass123' } });
    fireEvent.click(screen.getByTestId('slot-password-confirm'));
    await waitFor(() => expect(setPasswordMock).toHaveBeenCalledWith(1, 'newpass123'));
  });

  it('disabled slot has no actions', () => {
    render(<RouterSlotList slots={SLOTS} />);
    expect(screen.queryByTestId('slot-3-rename')).toBeNull();
  });
});

describe('groupDevicesBySlot', () => {
  it('groups a device by its subnet third octet, others fall to management', () => {
    const devices = [{ ip: '10.81.2.55' }, { ip: '192.168.1.9' }];
    const groups = groupDevicesBySlot(devices, SLOTS);
    const slot2Group = groups.find((g) => g.slot?.slot === 2);
    const mgmtGroup = groups.find((g) => g.slot === null);
    expect(slot2Group?.devices).toEqual([{ ip: '10.81.2.55' }]);
    expect(mgmtGroup?.devices).toEqual([{ ip: '192.168.1.9' }]);
  });
});
