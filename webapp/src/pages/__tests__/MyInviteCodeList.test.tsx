// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MyInviteCodeList } from '../MyInviteCodeList';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'invite.myCodesTitle': 'My Invite Codes',
        'invite.code': 'Code',
        'invite.used': 'Used',
        'invite.unused': 'Unused',
        'invite.remark': 'Remark',
        'invite.editRemark': 'Edit',
        'invite.saveRemark': 'Save',
        'invite.noRemark': 'No remark',
        'invite.loading': 'Loading...',
        'invite.noCodes': 'No invite codes',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock invite store
const mockLoadAllCodes = vi.fn();
const mockUpdateRemark = vi.fn();

vi.mock('../../stores/invite.store', () => ({
  useInviteStore: vi.fn(),
}));

import { useInviteStore } from '../../stores/invite.store';

describe('MyInviteCodeList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('test_invite_codes_list_loads — renders list of invite codes after loading', () => {
    mockLoadAllCodes.mockResolvedValue(undefined);

    (useInviteStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      codes: [
        {
          id: '1',
          code: 'ABC-DEF-123',
          remark: 'For friend',
          used: true,
          usedBy: 'user@test.com',
          createdAt: '2024-01-01T00:00:00Z',
        },
        {
          id: '2',
          code: 'XYZ-789-QRS',
          remark: '',
          used: false,
          usedBy: null,
          createdAt: '2024-01-02T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
      loadAllCodes: mockLoadAllCodes,
      updateRemark: mockUpdateRemark,
    });

    render(<MyInviteCodeList />);

    // Title
    expect(screen.getByText('My Invite Codes')).toBeInTheDocument();

    // Code values displayed (monospace)
    expect(screen.getByText('ABC-DEF-123')).toBeInTheDocument();
    expect(screen.getByText('XYZ-789-QRS')).toBeInTheDocument();

    // Status labels
    expect(screen.getByText('Used')).toBeInTheDocument();
    expect(screen.getByText('Unused')).toBeInTheDocument();

    // Remark for first code
    expect(screen.getByText('For friend')).toBeInTheDocument();

    // loadAllCodes should have been called on mount
    expect(mockLoadAllCodes).toHaveBeenCalled();
  });

  it('test_invite_codes_remark_editable — can edit remark on an invite code', async () => {
    mockLoadAllCodes.mockResolvedValue(undefined);
    mockUpdateRemark.mockResolvedValue(undefined);

    (useInviteStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      codes: [
        {
          id: '1',
          code: 'ABC-DEF-123',
          remark: 'Old remark',
          used: false,
          usedBy: null,
          createdAt: '2024-01-01T00:00:00Z',
        },
      ],
      isLoading: false,
      error: null,
      loadAllCodes: mockLoadAllCodes,
      updateRemark: mockUpdateRemark,
    });

    const user = userEvent.setup();
    render(<MyInviteCodeList />);

    // Should show the existing remark
    expect(screen.getByText('Old remark')).toBeInTheDocument();

    // Click edit button
    const editButton = screen.getByText('Edit');
    await user.click(editButton);

    // Should show an input with the current remark value
    const remarkInput = screen.getByDisplayValue('Old remark');
    expect(remarkInput).toBeInTheDocument();

    // Clear and type new remark
    await user.clear(remarkInput);
    await user.type(remarkInput, 'New remark');

    // Click save
    await user.click(screen.getByText('Save'));

    // Should call updateRemark with correct args
    expect(mockUpdateRemark).toHaveBeenCalledWith('1', 'New remark');
  });
});
