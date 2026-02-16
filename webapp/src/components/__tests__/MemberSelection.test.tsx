import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Member } from '../../api/types';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock cloud API — use vi.hoisted to avoid hoisting issue
const { mockGetMembers } = vi.hoisted(() => {
  return { mockGetMembers: vi.fn() };
});

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getMembers: mockGetMembers,
  },
}));

// Import after mocks
import { MemberSelection } from '../MemberSelection';

const testMembers: Member[] = [
  { id: 'm1', email: 'alice@test.com', role: 'member', status: 'active', createdAt: '2026-01-01' },
  { id: 'm2', email: 'bob@test.com', role: 'member', status: 'active', createdAt: '2026-01-02' },
];

describe('MemberSelection', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_member_selection_buy_for_members — Member selection allows buying for team members', async () => {
    mockGetMembers.mockResolvedValue({ code: 0, message: 'ok', data: testMembers });

    const onSelect = vi.fn();
    const user = userEvent.setup();
    render(<MemberSelection onSelect={onSelect} />);

    // Should load and display members
    await waitFor(() => {
      expect(screen.getByText('alice@test.com')).toBeInTheDocument();
      expect(screen.getByText('bob@test.com')).toBeInTheDocument();
    });

    // Click on a member to select them
    await user.click(screen.getByText('alice@test.com'));

    expect(onSelect).toHaveBeenCalledWith('m1');
  });

  it('test_member_selection_shows_empty — Shows empty state when no members', async () => {
    mockGetMembers.mockResolvedValue({ code: 0, message: 'ok', data: [] });

    render(<MemberSelection onSelect={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText('noMembers')).toBeInTheDocument();
    });
  });

  it('test_member_selection_buy_for_self_option — Shows buy for self option', async () => {
    mockGetMembers.mockResolvedValue({ code: 0, message: 'ok', data: testMembers });

    const onSelect = vi.fn();
    render(<MemberSelection onSelect={onSelect} currentUserId="u1" />);

    await waitFor(() => {
      expect(screen.getByText('buyForSelf')).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByText('buyForSelf'));

    expect(onSelect).toHaveBeenCalledWith('u1');
  });
});
