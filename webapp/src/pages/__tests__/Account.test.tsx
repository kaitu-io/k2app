/**
 * Account 页面测试
 *
 * 覆盖 App Store 审核合规改动:
 * - Task 1: 注销账号功能
 * - Task 2: iOS 购买入口屏蔽
 * - Task 3: Slogan 延迟显示 (7天)
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent, waitFor } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock stores
vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return {
    ...actual,
    useAuth: vi.fn(),
  };
});

vi.mock('../../hooks/useUser', () => ({
  useUser: vi.fn(),
}));

vi.mock('../../contexts/ThemeContext', () => ({
  useTheme: vi.fn(() => ({
    themeMode: 'dark',
    setThemeMode: vi.fn(),
  })),
}));

vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: vi.fn(() => ({
    links: { walletUrl: 'https://example.com/wallet' },
  })),
}));

vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    request: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return {
    ...actual,
    useLoginDialogStore: {
      getState: vi.fn(() => ({ open: vi.fn() })),
    },
  };
});

vi.mock('../../components/VersionItem', () => ({
  default: vi.fn(() => <div data-testid="version-item" />),
}));

vi.mock('../../components/BetaChannelToggle', () => ({
  default: vi.fn(() => null),
}));

// Import mocked modules
import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import { cloudApi } from '../../services/cloud-api';
import Account from '../Account';

const mockRun = vi.fn();

const createMockUser = (overrides = {}) => ({
  id: 1,
  expiredAt: '2027-01-01T00:00:00Z',
  loginIdentifies: [{ type: 'email', value: 'test@example.com' }],
  ...overrides,
});

// i18n loads real zh-CN translations, use actual Chinese text
const TEXT = {
  deleteAccount: '注销账号',
  deleteAccountWarning: '注销后账号数据将被删除，此操作不可撤销。确认要注销吗？',
  deleteAccountFailed: '注销失败，请重试',
  renewNow: '立即续费',
  proPlan: '开通服务',
  switchAccount: '切换账号',
  cancel: '取消',
  confirm: '确认',
};

describe('Account', () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Restore getComputedStyle after clearAllMocks wipes the setup.ts mock
    // MUI Dialog/Modal needs paddingRight from getComputedStyle
    const mockStyles: Record<string, string> = {
      visibility: 'visible', display: 'block', opacity: '1',
      paddingRight: '0px', overflowY: 'auto', overflow: 'visible',
    };
    (window.getComputedStyle as any).mockImplementation(() =>
      new Proxy(mockStyles, {
        get(target, prop) {
          if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
          if (typeof prop === 'string') return target[prop] || '';
          return undefined;
        },
      })
    );

    (window as any)._k2 = { run: mockRun };
    (window as any)._platform = {
      os: 'test',
      version: '1.0.0',
      openExternal: vi.fn(),
    };

    mockRun.mockResolvedValue({ code: 0 });

    vi.mocked(useAuth).mockReturnValue({
      isAuthenticated: true,
      setIsAuthenticated: vi.fn(),
    } as any);

    vi.mocked(useUser).mockReturnValue({
      user: createMockUser(),
      loading: false,
      isMembership: true,
      isExpired: false,
      fetchUser: vi.fn(),
    } as any);
  });

  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
    localStorage.clear();
  });

  // ==================== Task 1: 注销账号 ====================
  describe('注销账号功能', () => {
    it('登录状态下应显示注销账号按钮', () => {
      render(<Account />);
      expect(screen.getByText(TEXT.deleteAccount)).toBeTruthy();
    });

    it('未登录状态下不应显示注销账号按钮', () => {
      vi.mocked(useAuth).mockReturnValue({
        isAuthenticated: false,
        setIsAuthenticated: vi.fn(),
      } as any);

      render(<Account />);
      expect(screen.queryByText(TEXT.deleteAccount)).toBeNull();
    });

    it('点击注销按钮应弹出确认对话框', () => {
      render(<Account />);

      fireEvent.click(screen.getByText(TEXT.deleteAccount));

      expect(screen.getByText(TEXT.deleteAccountWarning)).toBeTruthy();
    });

    it('点击取消应关闭对话框', async () => {
      render(<Account />);

      fireEvent.click(screen.getByText(TEXT.deleteAccount));
      expect(screen.getByText(TEXT.deleteAccountWarning)).toBeTruthy();

      fireEvent.click(screen.getByText(TEXT.cancel));

      // MUI Dialog uses fade animation, wait for unmount
      await waitFor(() => {
        expect(screen.queryByText(TEXT.deleteAccountWarning)).toBeNull();
      });
    });

    it('确认注销应调用 DELETE API 并登出', async () => {
      const mockSetIsAuthenticated = vi.fn();
      vi.mocked(useAuth).mockReturnValue({
        isAuthenticated: true,
        setIsAuthenticated: mockSetIsAuthenticated,
      } as any);

      vi.mocked(cloudApi.request).mockResolvedValue({ code: 0 } as any);
      vi.mocked(cloudApi.post).mockResolvedValue({ code: 0 } as any);

      render(<Account />);

      fireEvent.click(screen.getByText(TEXT.deleteAccount));
      fireEvent.click(screen.getByText(TEXT.confirm));

      await waitFor(() => {
        expect(cloudApi.request).toHaveBeenCalledWith('DELETE', '/api/user/delete-account');
      });

      await waitFor(() => {
        expect(mockRun).toHaveBeenCalledWith('down');
        expect(mockSetIsAuthenticated).toHaveBeenCalledWith(false);
      });
    });

    it('注销 API 失败应显示错误提示', async () => {
      vi.mocked(cloudApi.request).mockRejectedValue(new Error('Network error'));

      const alertSpy = vi.spyOn(window, 'alert').mockImplementation(() => {});

      render(<Account />);

      fireEvent.click(screen.getByText(TEXT.deleteAccount));
      fireEvent.click(screen.getByText(TEXT.confirm));

      await waitFor(() => {
        expect(alertSpy).toHaveBeenCalledWith(TEXT.deleteAccountFailed);
      });

      alertSpy.mockRestore();
    });
  });

  // ==================== Task 2: iOS 购买入口屏蔽 ====================
  describe('iOS 购买入口屏蔽', () => {
    beforeEach(() => {
      vi.mocked(useUser).mockReturnValue({
        user: createMockUser({ expiredAt: '2020-01-01T00:00:00Z' }),
        loading: false,
        isMembership: false,
        isExpired: true,
        fetchUser: vi.fn(),
      } as any);
    });

    it('非 iOS 平台过期会员应显示立即续费按钮', () => {
      (window as any)._platform.os = 'macos';

      render(<Account />);
      expect(screen.getByText(TEXT.renewNow)).toBeTruthy();
    });

    it('iOS 平台过期会员不应显示立即续费按钮', () => {
      (window as any)._platform.os = 'ios';

      render(<Account />);
      expect(screen.queryByText(TEXT.renewNow)).toBeNull();
    });

    it('iOS 平台非会员不应显示开通服务按钮', () => {
      (window as any)._platform.os = 'ios';

      render(<Account />);
      expect(screen.queryByText(TEXT.proPlan)).toBeNull();
    });

    it('非 iOS 平台非会员应显示开通服务按钮', () => {
      (window as any)._platform.os = 'macos';

      render(<Account />);
      expect(screen.getByText(TEXT.proPlan)).toBeTruthy();
    });
  });

  // ==================== Task 3: Slogan 延迟显示 ====================
  describe('Slogan 延迟显示', () => {
    // Slogan text from zh-CN common.json brand.slogan
    const findSlogan = () => {
      // The slogan is rendered inside a Typography, search by partial match
      const sloganElements = screen.queryAllByText(/开红海/);
      return sloganElements.length > 0 ? sloganElements[0] : null;
    };

    it('首次启动不应显示 slogan', () => {
      render(<Account />);
      expect(findSlogan()).toBeNull();
    });

    it('首次启动应写入 k2_first_launch 时间戳', () => {
      render(<Account />);
      expect(localStorage.getItem('k2_first_launch')).not.toBeNull();
    });

    it('7天内不应显示 slogan', () => {
      const threeDaysAgo = Date.now() - 3 * 24 * 60 * 60 * 1000;
      localStorage.setItem('k2_first_launch', threeDaysAgo.toString());

      render(<Account />);
      expect(findSlogan()).toBeNull();
    });

    it('超过7天应显示 slogan', () => {
      const eightDaysAgo = Date.now() - 8 * 24 * 60 * 60 * 1000;
      localStorage.setItem('k2_first_launch', eightDaysAgo.toString());

      render(<Account />);
      expect(findSlogan()).not.toBeNull();
    });

    it('恰好7天应显示 slogan', () => {
      const sevenDaysAgo = Date.now() - 7 * 24 * 60 * 60 * 1000;
      localStorage.setItem('k2_first_launch', sevenDaysAgo.toString());

      render(<Account />);
      expect(findSlogan()).not.toBeNull();
    });
  });
});
