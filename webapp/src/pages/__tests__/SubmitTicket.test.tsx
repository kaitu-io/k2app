/**
 * SubmitTicket 页面测试
 *
 * 验证：
 * 1. 布局：返回按钮和标题在同一行（header row），不在 Card 内
 * 2. Beta 警告在标题下方
 * 3. 进入页面不会自动上传日志
 * 4. 点击提交时才上传日志 + 提交工单
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor, within } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock stores
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: vi.fn(),
}));

vi.mock('../../hooks/useUser', () => ({
  useUser: vi.fn(),
}));

vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    post: vi.fn(),
  },
}));

import { useAuthStore } from '../../stores/auth.store';
import { useUser } from '../../hooks/useUser';
import { cloudApi } from '../../services/cloud-api';
import SubmitTicket from '../SubmitTicket';

const mockUseAuthStore = vi.mocked(useAuthStore);
const mockUseUser = vi.mocked(useUser);
const mockCloudApiPost = vi.mocked(cloudApi.post);

let mockUploadLogs: ReturnType<typeof vi.fn>;

// Fix MUI TextareaAutosize in jsdom
const mockComputedStyle = () => ({
  width: '100px', height: '100px', boxSizing: 'content-box',
  paddingTop: '0px', paddingBottom: '0px', paddingLeft: '0px', paddingRight: '0px',
  borderTopWidth: '0px', borderBottomWidth: '0px', borderLeftWidth: '0px', borderRightWidth: '0px',
  overflow: 'visible', visibility: 'visible', display: 'block',
  minHeight: '0px', maxHeight: 'none',
  getPropertyValue: (_prop: string) => '',
});

/** Helper: find the page title (h6 in the header row, first child of data-tour container) */
function getTitle() {
  const container = document.querySelector('[data-tour="submit-ticket-page"]')!;
  // First child box is the header row, h6 inside it is the title
  const h6 = container.querySelector('h6')!;
  return h6;
}

function setupPlatform(overrides: { uploadLogs?: boolean; isBeta?: boolean } = {}) {
  mockUploadLogs = vi.fn().mockResolvedValue({ success: true, s3Keys: ['key1'] });
  window._platform = {
    os: 'macos',
    version: overrides.isBeta ? '0.4.0-beta.1' : '0.3.22',
    storage: { get: vi.fn().mockResolvedValue(null), set: vi.fn().mockResolvedValue(undefined), remove: vi.fn().mockResolvedValue(undefined) },
    getUdid: vi.fn().mockResolvedValue('test-udid'),
    openExternal: vi.fn().mockResolvedValue(undefined),
    writeClipboard: vi.fn().mockResolvedValue(undefined),
    readClipboard: vi.fn().mockResolvedValue(''),
    syncLocale: vi.fn().mockResolvedValue(undefined),
    ...(overrides.uploadLogs ? { uploadLogs: mockUploadLogs } : {}),
    ...(overrides.isBeta ? { updater: { channel: 'beta' as const, checkForUpdate: vi.fn(), onUpdateReady: vi.fn(), installUpdate: vi.fn() } } : {}),
  } as any;
  window._k2 = { run: vi.fn().mockResolvedValue({ code: 0, data: { state: 'disconnected' } }) } as any;
}

function setupAuthenticatedUser() {
  mockUseAuthStore.mockImplementation((selector: any) => {
    const state = { isAuthenticated: true };
    return typeof selector === 'function' ? selector(state) : state;
  });
  mockUseUser.mockReturnValue({ user: { loginIdentifies: [{ value: 'test@example.com' }] }, isLoading: false, error: null, mutate: vi.fn() } as any);
}

function setupAnonymousUser() {
  mockUseAuthStore.mockImplementation((selector: any) => {
    const state = { isAuthenticated: false };
    return typeof selector === 'function' ? selector(state) : state;
  });
  mockUseUser.mockReturnValue({ user: null, isLoading: false, error: null, mutate: vi.fn() } as any);
}

const renderPage = (route = '/submit-ticket') =>
  render(<SubmitTicket />, { useMemoryRouter: true, initialEntries: [route] });

describe('SubmitTicket', () => {
  beforeEach(() => {
    window.getComputedStyle = vi.fn().mockImplementation(mockComputedStyle) as any;
    setupPlatform();
    setupAuthenticatedUser();
    mockCloudApiPost.mockResolvedValue({ code: 0, data: null, message: 'ok' } as any);
  });

  afterEach(() => {
    delete (window as any)._platform;
    delete (window as any)._k2;
  });

  describe('布局结构', () => {
    it('返回按钮和标题在同一行（header row）', () => {
      renderPage();
      const title = getTitle();
      expect(title.textContent).toBe('提交工单');

      // header row 包含 back button 和 title
      const headerRow = title.parentElement!;
      expect(within(headerRow).getAllByRole('button').length).toBeGreaterThanOrEqual(1);
      expect(headerRow.querySelector('[data-testid="ArrowBackIcon"]')).toBeInTheDocument();
    });

    it('标题不在 Card 内部', () => {
      renderPage();
      const title = getTitle();
      let el: HTMLElement | null = title;
      while (el) {
        expect(el.classList.toString()).not.toMatch(/MuiCard-root/);
        el = el.parentElement;
        if (el?.getAttribute('data-tour') === 'submit-ticket-page') break;
      }
    });

    it('feedback 模式显示问题反馈标题', () => {
      renderPage('/submit-ticket?feedback=true');
      expect(getTitle().textContent).toBe('问题反馈');
    });

    it('提交按钮和 stepper 都可见', () => {
      renderPage();
      expect(screen.getByRole('button', { name: /提交工单/ })).toBeInTheDocument();
      expect(screen.getByText('工单处理流程')).toBeInTheDocument();
    });
  });

  describe('Beta 用户警告', () => {
    it('Beta 用户显示切回稳定版警告', () => {
      setupPlatform({ isBeta: true });
      renderPage();
      expect(screen.getByText(/Beta 版本/)).toBeInTheDocument();
      expect(screen.getByText('切回稳定版')).toBeInTheDocument();
    });

    it('非 Beta 用户不显示警告', () => {
      setupPlatform({ isBeta: false });
      renderPage();
      expect(getTitle().textContent).toBe('提交工单');
      expect(screen.queryByText('切回稳定版')).not.toBeInTheDocument();
    });

    it('Beta 警告在标题下方（DOM 顺序）', () => {
      setupPlatform({ isBeta: true });
      renderPage();
      const title = getTitle();
      const betaWarning = screen.getByText(/Beta 版本/);
      // title 在 DOM 中应在 betaWarning 之前
      expect(title.compareDocumentPosition(betaWarning) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
  });

  describe('日志上传时机', () => {
    it('进入页面不会自动上传日志', async () => {
      setupPlatform({ uploadLogs: true });
      renderPage();
      expect(getTitle()).toBeInTheDocument();
      await new Promise(r => setTimeout(r, 50));
      expect(mockUploadLogs).not.toHaveBeenCalled();
    });

    it('点击提交时上传日志', async () => {
      setupPlatform({ uploadLogs: true });
      renderPage();
      fireEvent.change(screen.getByRole('textbox', { name: '问题描述' }), { target: { value: '网络连接不稳定' } });
      fireEvent.click(screen.getByRole('button', { name: /提交工单/ }));
      await waitFor(() => {
        expect(mockUploadLogs).toHaveBeenCalledTimes(1);
      });
    });

    it('无 uploadLogs 能力时跳过上传，直接提交', async () => {
      setupPlatform({ uploadLogs: false });
      renderPage();
      fireEvent.change(screen.getByRole('textbox', { name: '问题描述' }), { target: { value: '问题描述内容' } });
      fireEvent.click(screen.getByRole('button', { name: /提交工单/ }));
      await waitFor(() => {
        expect(mockCloudApiPost).toHaveBeenCalledWith('/api/user/ticket', expect.objectContaining({ content: '问题描述内容' }));
      });
    });

    it('上传成功后提交工单包含 feedbackId', async () => {
      setupPlatform({ uploadLogs: true });
      renderPage();
      fireEvent.change(screen.getByRole('textbox', { name: '问题描述' }), { target: { value: '问题描述内容' } });
      fireEvent.click(screen.getByRole('button', { name: /提交工单/ }));
      await waitFor(() => {
        expect(mockCloudApiPost).toHaveBeenCalledWith('/api/user/ticket', expect.objectContaining({
          content: '问题描述内容',
          feedbackId: expect.any(String),
        }));
      });
    });
  });

  describe('表单验证', () => {
    it('内容为空时提交按钮禁用', () => {
      renderPage();
      expect(screen.getByRole('button', { name: /提交工单/ })).toBeDisabled();
    });

    it('匿名用户需要填写邮箱才能提交', () => {
      setupAnonymousUser();
      renderPage();
      fireEvent.change(screen.getByRole('textbox', { name: '问题描述' }), { target: { value: '问题内容' } });
      expect(screen.getByRole('button', { name: /提交工单/ })).toBeDisabled();
    });

    it('提交成功后显示成功页面', async () => {
      renderPage();
      fireEvent.change(screen.getByRole('textbox', { name: '问题描述' }), { target: { value: '问题内容' } });
      fireEvent.click(screen.getByRole('button', { name: /提交工单/ }));
      await waitFor(() => {
        expect(screen.getByText('工单提交成功！')).toBeInTheDocument();
      });
    });
  });
});
