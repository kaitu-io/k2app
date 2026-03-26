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
        'feedback:feedback.disconnectFeedback.thankYou': 'Thanks for your feedback',
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

import { DisconnectFeedbackDialog } from '../DisconnectFeedbackDialog';

describe('DisconnectFeedbackDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
    mockStoreState.lastConnectionInfo = {
      domain: 'test.example.com',
      name: 'Tokyo-01',
      country: 'JP',
      source: 'cloud',
      durationSec: 120,
      ruleMode: 'global',
      os: 'macos',
      appVersion: '0.4.0',
    };

    render(<DisconnectFeedbackDialog />);

    expect(screen.getByText('How was your connection?')).toBeInTheDocument();
    expect(screen.getByText('Good')).toBeInTheDocument();
    expect(screen.getByText('Bad')).toBeInTheDocument();
    expect(mockClearPendingFeedback).toHaveBeenCalledTimes(1);
  });

  it('点击"好"关闭 dialog 无 API 调用', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = {
      domain: 'test.example.com',
      name: 'Tokyo-01',
      country: 'JP',
      source: 'cloud',
      durationSec: 60,
      ruleMode: 'global',
      os: 'macos',
      appVersion: '0.4.0',
    };

    render(<DisconnectFeedbackDialog />);

    fireEvent.click(screen.getByText('Good'));

    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });
    expect(mockShowAlert).not.toHaveBeenCalled();
    expect(mockPost).not.toHaveBeenCalled();
  });

  it('点击"不好"关闭 dialog 并触发提交', async () => {
    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = {
      domain: 'test.example.com',
      name: 'Tokyo-01',
      country: 'JP',
      source: 'cloud',
      durationSec: 300,
      ruleMode: 'chnroute',
      os: 'macos',
      appVersion: '0.4.0',
    };

    render(<DisconnectFeedbackDialog />);

    fireEvent.click(screen.getByText('Bad'));

    // Dialog closes
    await waitFor(() => {
      expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
    });

    // Toast shown
    expect(mockShowAlert).toHaveBeenCalledWith('Thanks for your feedback', 'info');

    // uploadLogs called
    await waitFor(() => {
      expect(window._platform!.uploadLogs).toHaveBeenCalledTimes(1);
    });

    // Ticket submitted with connection info
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith(
        '/api/user/ticket',
        expect.objectContaining({
          feedbackId: expect.any(String),
          os: 'macos',
          app_version: '0.4.0',
        }),
      );
    });

    // device-log and feedback-notify also called
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/device-log', expect.any(Object));
      expect(mockPost).toHaveBeenCalledWith('/api/user/feedback-notify', expect.any(Object));
    });
  });

  it('"不好"路径在 uploadLogs 失败时仍提交 ticket', async () => {
    (window as any)._platform.uploadLogs = vi.fn().mockRejectedValue(new Error('upload failed'));

    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = {
      domain: 'test.example.com',
      name: 'Tokyo-01',
      country: 'JP',
      source: 'cloud',
      durationSec: 60,
      ruleMode: 'global',
      os: 'macos',
      appVersion: '0.4.0',
    };

    render(<DisconnectFeedbackDialog />);

    fireEvent.click(screen.getByText('Bad'));

    // Ticket still submitted even though logs failed
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.any(Object));
    });

    // device-log NOT called (no s3Keys)
    const deviceLogCalls = mockPost.mock.calls.filter(
      (call: any[]) => call[0] === '/api/user/device-log',
    );
    expect(deviceLogCalls).toHaveLength(0);
  });

  it('standalone 模式 (无 uploadLogs) 只提交 ticket', async () => {
    delete (window as any)._platform.uploadLogs;

    mockStoreState.pendingFeedback = true;
    mockStoreState.lastConnectionInfo = {
      domain: 'test.example.com',
      name: 'Tokyo-01',
      country: 'JP',
      source: 'cloud',
      durationSec: 60,
      ruleMode: 'global',
      os: 'macos',
      appVersion: '0.4.0',
    };

    render(<DisconnectFeedbackDialog />);

    fireEvent.click(screen.getByText('Bad'));

    // Ticket submitted
    await waitFor(() => {
      expect(mockPost).toHaveBeenCalledWith('/api/user/ticket', expect.any(Object));
    });

    // No device-log (no logs uploaded)
    const deviceLogCalls = mockPost.mock.calls.filter(
      (call: any[]) => call[0] === '/api/user/device-log',
    );
    expect(deviceLogCalls).toHaveLength(0);
  });
});
