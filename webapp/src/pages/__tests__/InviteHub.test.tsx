import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        title: 'Invite',
        inviteCode: 'Invite Code',
        copyCode: 'Copy Invite Code',
        copied: 'Copied',
        share: 'Share',
        shareLink: 'Share Link',
        generateNew: 'Generate New Code',
        generating: 'Generating...',
        remark: 'Remark',
        remarkPlaceholder: 'Add a remark',
        editRemark: 'Edit Remark',
        saveRemark: 'Save',
        qrCode: 'QR Code',
        expiration: 'Expiration',
        'stats.registered': 'Registered',
        'stats.purchased': 'Purchased',
        'retailerStats.title': 'Retailer Stats',
        'retailerStats.totalInvites': 'Total Invites',
        'retailerStats.registered': 'Registered',
        'retailerStats.purchased': 'Purchased',
        'rules.title': 'Invite Rules',
        'rules.rule1': 'Each invite code can only be used once',
        'rules.rule2': 'You will receive a reward when invitees register',
        'rules.rule3': 'Invite codes can have an expiration period',
      };
      return map[key] || key;
    },
  }),
}));

// Mock stores
const mockLoadLatest = vi.fn();
const mockGenerateCode = vi.fn();
const mockUpdateRemark = vi.fn();
const mockLoadAllCodes = vi.fn();
const mockAddAlert = vi.fn();
const mockWriteClipboard = vi.fn().mockResolvedValue(undefined);
const mockCreateShareLink = vi.fn().mockResolvedValue({ data: { url: 'https://kaitu.io/invite/ABC123' } });

let mockInviteStoreState: Record<string, unknown> = {};
let mockUserStoreState: Record<string, unknown> = {};

vi.mock('../../stores/invite.store', () => ({
  useInviteStore: () => mockInviteStoreState,
}));

vi.mock('../../stores/user.store', () => ({
  useUserStore: () => mockUserStoreState,
}));

vi.mock('../../stores/ui.store', () => ({
  useUiStore: () => ({ addAlert: mockAddAlert }),
}));

vi.mock('../../platform', () => ({
  getPlatform: () => ({
    writeClipboard: mockWriteClipboard,
    isMobile: false,
    platformName: 'desktop',
  }),
}));

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    createShareLink: mockCreateShareLink,
  },
}));

// Import after mocks
import { InviteHub } from '../InviteHub';

describe('InviteHub', () => {
  beforeEach(() => {
    mockInviteStoreState = {
      latestCode: {
        id: 'inv-001',
        code: 'ABC123',
        remark: '',
        used: false,
        usedBy: null,
        createdAt: '2026-01-01T00:00:00Z',
        registeredCount: 5,
        purchasedCount: 2,
      },
      codes: [],
      isLoading: false,
      error: null,
      loadLatest: mockLoadLatest,
      generateCode: mockGenerateCode,
      updateRemark: mockUpdateRemark,
      loadAllCodes: mockLoadAllCodes,
    };

    mockUserStoreState = {
      user: { id: 'u1', email: 'test@example.com', role: 'user' },
    };
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_invite_shows_latest_code — Shows latest invite code in monospace display', () => {
    render(<InviteHub />);
    const codeEl = screen.getByTestId('invite-code-display');
    expect(codeEl).toHaveTextContent('ABC123');
    expect(codeEl.className).toMatch(/font-mono/);
  });

  it('test_invite_code_copy_to_clipboard — Clicking code copies to clipboard', async () => {
    render(<InviteHub />);
    const codeEl = screen.getByTestId('invite-code-display');
    await userEvent.click(codeEl);
    expect(mockWriteClipboard).toHaveBeenCalledWith('ABC123');
  });

  it('test_invite_stats_display — Shows registered and purchased counts', () => {
    render(<InviteHub />);
    expect(screen.getByTestId('stat-registered')).toHaveTextContent('5');
    expect(screen.getByTestId('stat-purchased')).toHaveTextContent('2');
  });

  it('test_invite_qr_code_desktop — QR code from share link shown on desktop', () => {
    render(<InviteHub />);
    // QR code section should be visible on desktop
    const qrSection = screen.getByTestId('qr-code-section');
    expect(qrSection).toBeInTheDocument();
  });

  it('test_share_opens_expiration_popover — Share button opens expiration selector', async () => {
    render(<InviteHub />);
    const shareBtn = screen.getByTestId('share-button');
    await userEvent.click(shareBtn);
    // Expiration popover should appear
    await waitFor(() => {
      expect(screen.getByTestId('expiration-popover')).toBeInTheDocument();
    });
  });

  it('test_generate_new_invite_code — Generate button creates new code via store', async () => {
    render(<InviteHub />);
    const generateBtn = screen.getByTestId('generate-button');
    await userEvent.click(generateBtn);
    expect(mockGenerateCode).toHaveBeenCalled();
  });

  it('test_invite_code_remark_editable — Remark text is editable inline', async () => {
    render(<InviteHub />);
    // Click the edit remark button
    const editBtn = screen.getByTestId('edit-remark-button');
    await userEvent.click(editBtn);
    // Input should appear
    const remarkInput = screen.getByTestId('remark-input');
    expect(remarkInput).toBeInTheDocument();
    await userEvent.clear(remarkInput);
    await userEvent.type(remarkInput, 'My friend');
    // Save
    const saveBtn = screen.getByTestId('save-remark-button');
    await userEvent.click(saveBtn);
    expect(mockUpdateRemark).toHaveBeenCalledWith('inv-001', 'My friend');
  });

  it('test_retailer_mode_shows_stats — Retailer users see stats overview', () => {
    mockUserStoreState = {
      user: { id: 'u1', email: 'retailer@example.com', role: 'retailer' },
    };
    mockInviteStoreState = {
      ...mockInviteStoreState,
      codes: [
        { id: '1', code: 'A', remark: '', used: true, usedBy: 'x', createdAt: '', registeredCount: 3, purchasedCount: 1 },
        { id: '2', code: 'B', remark: '', used: false, usedBy: null, createdAt: '', registeredCount: 2, purchasedCount: 1 },
      ],
    };
    render(<InviteHub />);
    expect(screen.getByTestId('retailer-stats-overview')).toBeInTheDocument();
    expect(screen.getByText('Retailer Stats')).toBeInTheDocument();
  });

  it('test_non_retailer_shows_invite_rules — Non-retailer users see invite rules', () => {
    mockUserStoreState = {
      user: { id: 'u1', email: 'test@example.com', role: 'user' },
    };
    render(<InviteHub />);
    expect(screen.getByTestId('invite-rules')).toBeInTheDocument();
    expect(screen.getByText('Invite Rules')).toBeInTheDocument();
    expect(screen.getByText('Each invite code can only be used once')).toBeInTheDocument();
  });

  it('calls loadLatest on mount', () => {
    render(<InviteHub />);
    expect(mockLoadLatest).toHaveBeenCalled();
  });
});
