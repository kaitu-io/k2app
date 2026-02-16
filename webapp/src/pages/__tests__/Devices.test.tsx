import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'devices:title': 'My Devices',
        'devices:current_device': 'Current',
        'devices:remark': 'Remark',
        'devices:remark_placeholder': 'Enter remark',
        'devices:delete': 'Delete',
        'devices:delete_confirm_title': 'Delete Device',
        'devices:delete_confirm_message': 'Are you sure you want to delete this device?',
        'devices:confirm': 'Confirm',
        'devices:cancel': 'Cancel',
        'devices:save': 'Save',
        'devices:edit': 'Edit',
        'devices:no_devices': 'No devices',
        'common:loading': 'Loading...',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US' },
  }),
}));

// Mock cloudApi
const mockGetDevices = vi.fn();
const mockDeleteDevice = vi.fn();
const mockUpdateDeviceRemark = vi.fn();

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getDevices: (...args: unknown[]) => mockGetDevices(...args),
    deleteDevice: (...args: unknown[]) => mockDeleteDevice(...args),
    updateDeviceRemark: (...args: unknown[]) => mockUpdateDeviceRemark(...args),
  },
}));

// Mock vpn-client
const mockGetUDID = vi.fn();

vi.mock('../../vpn-client', () => ({
  getVpnClient: () => ({
    getUDID: mockGetUDID,
  }),
}));

import { Devices } from '../Devices';

const mockDevices = [
  {
    id: 'dev-1',
    name: 'MacBook Pro',
    remark: 'Work laptop',
    platform: 'macos',
    lastActiveAt: '2026-02-15T10:00:00Z',
    createdAt: '2026-01-01T00:00:00Z',
  },
  {
    id: 'dev-2',
    name: 'iPhone 15',
    remark: '',
    platform: 'ios',
    lastActiveAt: '2026-02-14T08:00:00Z',
    createdAt: '2026-01-15T00:00:00Z',
  },
  {
    id: 'dev-3',
    name: 'Windows Desktop',
    remark: 'Home PC',
    platform: 'windows',
    lastActiveAt: '2026-02-10T12:00:00Z',
    createdAt: '2025-12-01T00:00:00Z',
  },
];

describe('Devices', () => {
  beforeEach(() => {
    mockGetDevices.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: mockDevices,
    });
    mockGetUDID.mockResolvedValue('dev-1');
    mockDeleteDevice.mockResolvedValue({ code: 0, message: 'ok' });
    mockUpdateDeviceRemark.mockResolvedValue({ code: 0, message: 'ok' });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_devices_list_current_highlighted — lists devices and highlights current device', async () => {
    render(<Devices />);

    // Wait for devices to load
    await waitFor(() => {
      expect(screen.getByText('MacBook Pro')).toBeInTheDocument();
    });

    // All devices should be listed
    expect(screen.getByText('MacBook Pro')).toBeInTheDocument();
    expect(screen.getByText('iPhone 15')).toBeInTheDocument();
    expect(screen.getByText('Windows Desktop')).toBeInTheDocument();

    // Current device (dev-1) should have the "Current" chip
    expect(screen.getByText('Current')).toBeInTheDocument();

    // Current device card should have the highlighted styling
    const currentDeviceCard = screen.getByText('MacBook Pro').closest('[data-testid="device-card-dev-1"]');
    expect(currentDeviceCard).toBeInTheDocument();
    expect(currentDeviceCard!.className).toContain('border-');
    expect(currentDeviceCard!.className).toContain('bg-');
  });

  it('test_device_remark_edit_delete_confirm — can edit remark and delete device with confirmation', async () => {
    const user = userEvent.setup();
    render(<Devices />);

    // Wait for devices to load
    await waitFor(() => {
      expect(screen.getByText('MacBook Pro')).toBeInTheDocument();
    });

    // Device should show its remark
    expect(screen.getByText('Work laptop')).toBeInTheDocument();

    // Click edit on a device to edit its remark
    const editButtons = screen.getAllByText('Edit');
    await user.click(editButtons[0]!);

    // Should show input with current remark
    const remarkInput = screen.getByDisplayValue('Work laptop');
    expect(remarkInput).toBeInTheDocument();

    // Change the remark
    await user.clear(remarkInput);
    await user.type(remarkInput, 'Personal laptop');
    await user.click(screen.getByText('Save'));

    // Should call updateDeviceRemark
    await waitFor(() => {
      expect(mockUpdateDeviceRemark).toHaveBeenCalledWith('dev-1', 'Personal laptop');
    });
  });

  it('test_devices_delete_confirmation_dialog — shows confirmation dialog before deleting', async () => {
    const user = userEvent.setup();
    render(<Devices />);

    // Wait for devices to load
    await waitFor(() => {
      expect(screen.getByText('Windows Desktop')).toBeInTheDocument();
    });

    // Click delete on a non-current device (dev-3)
    const deleteButtons = screen.getAllByText('Delete');
    // dev-1 is current device, may not have delete. dev-2 and dev-3 should have delete buttons.
    await user.click(deleteButtons[deleteButtons.length - 1]!);

    // Should show confirmation dialog
    await waitFor(() => {
      expect(screen.getByText('Delete Device')).toBeInTheDocument();
      expect(screen.getByText('Are you sure you want to delete this device?')).toBeInTheDocument();
    });

    // Click confirm
    await user.click(screen.getByText('Confirm'));

    // Should call deleteDevice
    await waitFor(() => {
      expect(mockDeleteDevice).toHaveBeenCalled();
    });
  });

  it('test_devices_delete_confirmation_cancel — cancel does not delete', async () => {
    const user = userEvent.setup();
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('iPhone 15')).toBeInTheDocument();
    });

    const deleteButtons = screen.getAllByText('Delete');
    await user.click(deleteButtons[0]!);

    await waitFor(() => {
      expect(screen.getByText('Delete Device')).toBeInTheDocument();
    });

    // Click cancel
    await user.click(screen.getByText('Cancel'));

    // Should NOT call deleteDevice
    expect(mockDeleteDevice).not.toHaveBeenCalled();

    // Dialog should close
    await waitFor(() => {
      expect(screen.queryByText('Are you sure you want to delete this device?')).not.toBeInTheDocument();
    });
  });

  it('shows device remarks when present', async () => {
    render(<Devices />);

    await waitFor(() => {
      expect(screen.getByText('Work laptop')).toBeInTheDocument();
      expect(screen.getByText('Home PC')).toBeInTheDocument();
    });
  });
});
