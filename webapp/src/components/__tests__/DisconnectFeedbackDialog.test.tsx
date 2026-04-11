/**
 * DisconnectFeedbackDialog Tests
 *
 * Tests the mandatory post-disconnect quality feedback dialog.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';

// Mock MUI Dialog to avoid jsdom ModalManager issues
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => open ? <div data-testid="mock-dialog">{children}</div> : null,
    DialogTitle: ({ children, ...props }: any) => <div {...props}>{children}</div>,
    DialogActions: ({ children, ...props }: any) => <div {...props}>{children}</div>,
  };
});

// Mock react-i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const translations: Record<string, string> = {
        'feedback:feedback.disconnectFeedback.title': 'How was your connection?',
        'feedback:feedback.disconnectFeedback.good': 'Good',
        'feedback:feedback.disconnectFeedback.bad': 'Bad',
        'feedback:feedback.disconnectFeedback.thankYou': 'Thanks',
      };
      return translations[key] || key;
    },
  }),
}));

// Mock connection store
const mockClearPendingFeedback = vi.fn();
let mockStoreState = {
  pendingFeedback: false,
  lastConnectionInfo: null as any,
  clearPendingFeedback: mockClearPendingFeedback,
};

vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (selector: any) => selector(mockStoreState),
}));

// Mock alert store
const mockShowAlert = vi.fn();
vi.mock('../../stores/alert.store', () => ({
  useAlertStore: (selector: any) => selector({ showAlert: mockShowAlert }),
}));

// Mock cloud-api
const mockPost = vi.fn().mockResolvedValue({ code: 0 });
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { post: (...args: any[]) => mockPost(...args) },
}));

// Mock device-udid
vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: () => Promise.resolve('test-udid'),
}));

// Mock network-env
vi.mock('../../services/network-env', () => ({
  refreshNetworkEnv: () => Promise.resolve({
    publicIP: '1.2.3.4',
    isp: 'Test ISP',
    city: 'Shanghai',
    country: 'CN',
    networkType: 'wifi',
  }),
}));

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
  });

  afterEach(() => {
    delete (window as any)._platform;
  });

  it('不渲染 dialog 当 pendingFeedback=false', () => {
    render(<DisconnectFeedbackDialog />);
    expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
  });

  it('弹出 dialog 当 pendingFeedback=true 并立刻消费 flag', () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);

    expect(screen.getByText('How was your connection?')).toBeInTheDocument();
    expect(screen.getByText('Good')).toBeInTheDocument();
    expect(screen.getByText('Bad')).toBeInTheDocument();
    expect(mockClearPendingFeedback).toHaveBeenCalledTimes(1);
  });

  it('点击"好"关闭 dialog 并提交 good rating', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    fireEvent.click(screen.getByText('Good'));

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    // No toast for good
    expect(mockShowAlert).not.toHaveBeenCalled();
    // Rating submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith(
        '/api/user/connection-rating',
        expect.objectContaining({ rating: 'good' }),
      );
    });
    // No ticket created
    const ticketCalls = mockPost.mock.calls.filter(
      (call: any[]) => call[0] === '/api/user/ticket',
    );
    expect(ticketCalls).toHaveLength(0);
  });

  it('点击"不好"提交 ticket (auto_generated) + rating', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = { ...mockConnectionInfo, durationSec: 300, ruleMode: 'chnroute' };

    render(<DisconnectFeedbackDialog />);
    fireEvent.click(screen.getByText('Bad'));

    // Dialog closes
    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });

    // Toast shown
    expect(mockShowAlert).toHaveBeenCalledWith('Thanks', 'info');

    // uploadLogs called
    await waitFor(() => {
      expect(window._platform!.uploadLogs).toHaveBeenCalledTimes(1);
    });

    // Ticket submitted with auto_generated flag
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith(
        '/api/user/ticket',
        expect.objectContaining({
          feedbackId: expect.any(String),
          os: 'macos',
          app_version: '0.4.0',
          auto_generated: true,
        }),
      );
    });

    // device-log and feedback-notify also called
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/device-log', expect.any(Object));
      expect(mockPost).toHaveBeenCalledWith('/api/user/feedback-notify', expect.any(Object));
    });

    // Rating also submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith(
        '/api/user/connection-rating',
        expect.objectContaining({ rating: 'bad' }),
      );
    });
  });

  it('"不好"路径在 uploadLogs 失败时仍提交 ticket + rating', async () => {
    (window as any)._platform.uploadLogs = vi.fn().mockRejectedValue(new Error('upload failed'));

    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    fireEvent.click(screen.getByText('Bad'));

    // Ticket still submitted even though logs failed
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.objectContaining({ auto_generated: true }));
    });

    // Rating submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/connection-rating', expect.objectContaining({ rating: 'bad' }));
    });

    // device-log NOT called (no s3Keys)
    const deviceLogCalls = mockPost.mock.calls.filter(
      (call: any[]) => call[0] === '/api/user/device-log',
    );
    expect(deviceLogCalls).toHaveLength(0);
  });

  it('standalone 模式 (无 uploadLogs) 只提交 ticket + rating', async () => {
    delete (window as any)._platform.uploadLogs;

    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = mockConnectionInfo;

    render(<DisconnectFeedbackDialog />);
    fireEvent.click(screen.getByText('Bad'));

    // Ticket submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.objectContaining({ auto_generated: true }));
    });

    // Rating submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/connection-rating', expect.objectContaining({ rating: 'bad' }));
    });

    // No device-log (no logs uploaded)
    const deviceLogCalls = mockPost.mock.calls.filter(
      (call: any[]) => call[0] === '/api/user/device-log',
    );
    expect(deviceLogCalls).toHaveLength(0);
  });
});
