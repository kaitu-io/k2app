import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'members:title': 'Member Management',
        'members:add_member': 'Add',
        'members:email_placeholder': 'Enter member email',
        'members:delete': 'Delete',
        'members:delete_confirm_title': 'Remove Member',
        'members:delete_confirm_message': 'Are you sure you want to remove this member?',
        'members:confirm': 'Confirm',
        'members:cancel': 'Cancel',
        'members:status_active': 'Active',
        'members:status_expired': 'Expired',
        'members:status_not_activated': 'Not Activated',
        'members:no_members': 'No members',
        'common:loading': 'Loading...',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US' },
  }),
}));

// Mock cloudApi
const mockGetMembers = vi.fn();
const mockAddMember = vi.fn();
const mockDeleteMember = vi.fn();

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getMembers: (...args: unknown[]) => mockGetMembers(...args),
    addMember: (...args: unknown[]) => mockAddMember(...args),
    deleteMember: (...args: unknown[]) => mockDeleteMember(...args),
  },
}));

import { MemberManagement } from '../MemberManagement';

const mockMembers = [
  {
    id: 'mem-1',
    email: 'alice@example.com',
    role: 'member',
    status: 'active',
    createdAt: '2026-01-01T00:00:00Z',
  },
  {
    id: 'mem-2',
    email: 'bob@example.com',
    role: 'member',
    status: 'expired',
    createdAt: '2026-01-10T00:00:00Z',
  },
  {
    id: 'mem-3',
    email: 'carol@example.com',
    role: 'member',
    status: 'not_activated',
    createdAt: '2026-02-01T00:00:00Z',
  },
];

describe('MemberManagement', () => {
  beforeEach(() => {
    mockGetMembers.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: mockMembers,
    });
    mockAddMember.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: { id: 'mem-4', email: 'dave@example.com', role: 'member', status: 'not_activated', createdAt: '2026-02-16T00:00:00Z' },
    });
    mockDeleteMember.mockResolvedValue({ code: 0, message: 'ok' });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_member_add_delete — lists members, adds new member, deletes member', async () => {
    render(<MemberManagement />);

    // Wait for members to load
    await waitFor(() => {
      expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    });

    // All members should be listed
    expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    expect(screen.getByText('bob@example.com')).toBeInTheDocument();
    expect(screen.getByText('carol@example.com')).toBeInTheDocument();

    // Status chips should be displayed
    expect(screen.getByText('Active')).toBeInTheDocument();
    expect(screen.getByText('Expired')).toBeInTheDocument();
    expect(screen.getByText('Not Activated')).toBeInTheDocument();
  });

  it('test_member_add_by_email — adds a member by email', async () => {
    const user = userEvent.setup();
    render(<MemberManagement />);

    await waitFor(() => {
      expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    });

    // Find the email input and add button
    const emailInput = screen.getByPlaceholderText('Enter member email');
    await user.type(emailInput, 'dave@example.com');
    await user.click(screen.getByText('Add'));

    // Should call addMember with the email
    await waitFor(() => {
      expect(mockAddMember).toHaveBeenCalledWith('dave@example.com');
    });
  });

  it('test_member_delete_with_confirmation — deletes a member with confirmation dialog', async () => {
    const user = userEvent.setup();
    render(<MemberManagement />);

    await waitFor(() => {
      expect(screen.getByText('bob@example.com')).toBeInTheDocument();
    });

    // Click delete on a member
    const deleteButtons = screen.getAllByText('Delete');
    await user.click(deleteButtons[0]!);

    // Should show confirmation dialog
    await waitFor(() => {
      expect(screen.getByText('Remove Member')).toBeInTheDocument();
      expect(screen.getByText('Are you sure you want to remove this member?')).toBeInTheDocument();
    });

    // Click confirm
    await user.click(screen.getByText('Confirm'));

    // Should call deleteMember
    await waitFor(() => {
      expect(mockDeleteMember).toHaveBeenCalled();
    });
  });

  it('shows member avatar initials', async () => {
    render(<MemberManagement />);

    await waitFor(() => {
      expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    });

    // Member avatar should show first letter of email (uppercase)
    // alice -> A, bob -> B, carol -> C
    expect(screen.getByText('A')).toBeInTheDocument();
    expect(screen.getByText('B')).toBeInTheDocument();
    expect(screen.getByText('C')).toBeInTheDocument();
  });
});
