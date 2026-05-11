/**
 * DisconnectFeedbackDialog Tests
 *
 * Tests the post-disconnect 5-star rating dialog.
 * Run: cd webapp && npx vitest run src/components/__tests__/DisconnectFeedbackDialog.test.tsx
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// Mock MUI Dialog wrapper to bypass jsdom ModalManager issues.
// Keep all other MUI primitives (Rating, Chip, Button, etc.) as the real ones.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => open ? <div data-testid="mock-dialog">{children}</div> : null,
    DialogTitle: ({ children, ...props }: any) => <div {...props}>{children}</div>,
    DialogActions: ({ children, ...props }: any) => <div {...props}>{children}</div>,
    DialogContent: ({ children, ...props }: any) => <div {...props}>{children}</div>,
  };
});

// Mock react-i18next.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const translations: Record<string, string> = {
        'feedback:feedback.disconnectFeedback.title': 'How was your connection?',
        'feedback:feedback.disconnectFeedback.thankYou': 'Thanks',
        'feedback:feedback.disconnectFeedback.detailTitle': 'What went wrong?',
        'feedback:feedback.disconnectFeedback.submit': 'Submit',
        'feedback:feedback.disconnectFeedback.tags.slow': 'Slow',
        'feedback:feedback.disconnectFeedback.tags.cantConnect': "Can't connect",
        'feedback:feedback.disconnectFeedback.tags.frequentDrops': 'Frequent drops',
        'feedback:feedback.disconnectFeedback.tags.contentBlocked': 'Sites blocked',
        'feedback:feedback.disconnectFeedback.tags.other': 'Other',
      };
      return translations[key] || key;
    },
  }),
}));

// Mock connection store.
const mockClearPendingFeedback = vi.fn();
let mockStoreState = {
  pendingFeedback: false,
  lastConnectionInfo: null as any,
  clearPendingFeedback: mockClearPendingFeedback,
};

vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (selector: any) => selector(mockStoreState),
}));

// Mock alert store.
const mockShowAlert = vi.fn();
vi.mock('../../stores/alert.store', () => ({
  useAlertStore: (selector: any) => selector({ showAlert: mockShowAlert }),
}));

// Mock cloud-api.
const mockPost = vi.fn().mockResolvedValue({ code: 0 });
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: (...args: any[]) => mockPost(...args) },
}));

// Mock device-udid.
vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: () => Promise.resolve('test-udid'),
}));

// Mock network-env.
vi.mock('../../services/network-env', () => ({
  refreshNetworkEnv: () => Promise.resolve({
    publicIP: '1.2.3.4',
    isp: 'Test ISP',
    city: 'Shanghai',
    country: 'CN',
    networkType: 'wifi',
  }),
}));

import { createMockCSSStyleDeclaration } from '../../test/setup-dom';
import { DisconnectFeedbackDialog } from '../DisconnectFeedbackDialog';

const mockConnectionInfo = {
  domain: 'test.example.com',
  name: 'Tokyo-01',
  country: 'JP',
  source: 'cloud' as const,
  durationSec: 120,
  ruleMode: 'global',
  os: 'macos',
  appVersion: '0.4.0',
  commit: 'abc1234',
};

function clickStar(value: number) {
  const star = screen.getByRole('radio', { name: `${value} Star${value > 1 ? 's' : ''}` });
  fireEvent.click(star);
}

function getTicketCalls() {
  return mockPost.mock.calls.filter((c) => c[0] === '/api/user/ticket');
}

function getRatingCalls() {
  return mockPost.mock.calls.filter((c) => c[0] === '/api/user/connection-rating');
}

describe('DisconnectFeedbackDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPost.mockResolvedValue({ code: 0 });
    mockStoreState = {
      pendingFeedback: false,
      lastConnectionInfo: null,
      clearPendingFeedback: mockClearPendingFeedback,
    };
    (window as any)._platform = {
      os: 'macos',
      version: '0.4.0',
      uploadLogs: vi.fn().mockResolvedValue({ success: true, s3Keys: [{ name: 'k2.log', s3Key: 'test/k2.log' }] }),
      updater: { channel: 'beta' },
    };
    // Re-patch getComputedStyle after vi.restoreAllMocks() (run in global afterEach)
    // may have restored the bare jsdom implementation which returns undefined for properties.
    // Uses the shared helper from setup-dom to avoid drift with the canonical mock.
    window.getComputedStyle = (_el: Element) => createMockCSSStyleDeclaration() as CSSStyleDeclaration;
  });

  afterEach(() => {
    delete (window as any)._platform;
  });

  it('renders nothing when pendingFeedback is false', () => {
    render(<DisconnectFeedbackDialog />);
    expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
  });

  it('opens and consumes pendingFeedback flag', () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);

    expect(screen.getByText('How was your connection?')).toBeInTheDocument();
    expect(mockClearPendingFeedback).toHaveBeenCalledTimes(1);
  });

  it('5 stars: instant submits good and closes with toast', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(5);

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    expect(mockShowAlert).toHaveBeenCalledWith('Thanks', 'info');
    await waitFor(() => {
      expect(getRatingCalls()).toHaveLength(1);
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'good' });
    });
    expect(getTicketCalls()).toHaveLength(0);
  });

  it('4 stars: instant submits good and closes', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(4);

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'good' });
    });
    expect(getTicketCalls()).toHaveLength(0);
  });

  it('3 stars: instant submits bad, no detail step, no negative machinery', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(3);

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'bad' });
    });
    expect(screen.queryByText('What went wrong?')).not.toBeInTheDocument();
    expect(getTicketCalls()).toHaveLength(0);
    expect(window._platform!.uploadLogs).not.toHaveBeenCalled();
    const notifyCalls = mockPost.mock.calls.filter((c) => c[0] === '/api/user/feedback-notify');
    expect(notifyCalls).toHaveLength(0);
  });

  it('2 stars: enters detail step, no submit fired yet', () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(2);

    expect(screen.getByText('What went wrong?')).toBeInTheDocument();
    expect(screen.getByText('Submit')).toBeInTheDocument();
    expect(getRatingCalls()).toHaveLength(0);
    expect(getTicketCalls()).toHaveLength(0);
  });

  it('1 star: enters detail step, no submit fired yet', () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(1);

    expect(screen.getByText('What went wrong?')).toBeInTheDocument();
    expect(getRatingCalls()).toHaveLength(0);
  });

  it('2 stars + no chips + Submit: rating=bad, ticket body has "Tags: 无"', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(2);
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => {
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'bad' });
    });
    await waitFor(() => {
      expect(getTicketCalls()).toHaveLength(1);
    });
    const ticketBody: string = getTicketCalls()[0][1].content;
    expect(ticketBody).toContain('Tags: 无');
    expect(ticketBody).toContain('2★');
  });

  it('1 star + chips + Submit: ticket body has zh-CN labels regardless of locale mock', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(1);

    fireEvent.click(screen.getByText('Slow'));
    fireEvent.click(screen.getByText('Frequent drops'));
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => {
      expect(getTicketCalls()).toHaveLength(1);
    });
    const ticketBody: string = getTicketCalls()[0][1].content;
    expect(ticketBody).toContain('速度慢');
    expect(ticketBody).toContain('经常断开');
    expect(ticketBody).toContain('1★');
  });

  it('1 star: triggers full negative machinery (uploadLogs + ticket + device-log + feedback-notify)', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(1);
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => {
      expect(window._platform!.uploadLogs).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(getTicketCalls()).toHaveLength(1);
      expect(getTicketCalls()[0][1]).toMatchObject({ auto_generated: true });
    });
    await waitFor(() => {
      const deviceLogCalls = mockPost.mock.calls.filter((c) => c[0] === '/api/user/device-log');
      const notifyCalls = mockPost.mock.calls.filter((c) => c[0] === '/api/user/feedback-notify');
      expect(deviceLogCalls).toHaveLength(1);
      expect(notifyCalls).toHaveLength(1);
    });
  });

  it('2 stars: triggers full negative machinery (uploadLogs + ticket + device-log + feedback-notify)', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(2);
    fireEvent.click(screen.getByText('Submit'));

    await waitFor(() => {
      expect(window._platform!.uploadLogs).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(getTicketCalls()).toHaveLength(1);
      expect(getTicketCalls()[0][1]).toMatchObject({ auto_generated: true });
    });
    await waitFor(() => {
      const deviceLogCalls = mockPost.mock.calls.filter((c) => c[0] === '/api/user/device-log');
      const notifyCalls = mockPost.mock.calls.filter((c) => c[0] === '/api/user/feedback-notify');
      expect(deviceLogCalls).toHaveLength(1);
      expect(notifyCalls).toHaveLength(1);
    });
  });

  it('1 star -> 5 stars: leaves detail, submits good, no negative machinery', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(1);
    expect(screen.getByText('What went wrong?')).toBeInTheDocument();
    clickStar(5);

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'good' });
    });
    expect(getTicketCalls()).toHaveLength(0);
    expect(window._platform!.uploadLogs).not.toHaveBeenCalled();
  });

  it('1 star -> 3 stars: leaves detail, submits bad, no negative machinery', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    clickStar(1);
    clickStar(3);

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(getRatingCalls()[0][1]).toMatchObject({ rating: 'bad' });
    });
    expect(getTicketCalls()).toHaveLength(0);
  });

  it('rating submission failure is swallowed (no thrown error, dialog still closes)', async () => {
    mockPost.mockRejectedValue(new Error('network down'));
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);

    expect(() => clickStar(5)).not.toThrow();

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
  });
});
