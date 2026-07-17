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
import { brandConfig } from '../../brands';
import { getBrandName, getBrandSlogan } from '../../brands/i18n-vars';

// Test locale is zh-CN (navigator.language pinned in src/test/setup.ts), so
// brand-derived copy is resolved for zh-CN here. Deriving these from
// brandConfig — rather than hardcoding kaitu strings — keeps the suite green
// and meaningful under `K2_BRAND=overleap` too.
const BRAND_NAME_ZH = getBrandName('zh-CN');
// Mirrors Account.tsx: domain label, plus the localized name only when it
// differs from the Latin product name (kaitu zh → "Kaitu.io 开途";
// overleap → "Overleap.io").
const BRAND_BANNER =
  BRAND_NAME_ZH !== brandConfig.productName
    ? `${brandConfig.domainLabel} ${BRAND_NAME_ZH}`
    : brandConfig.domainLabel;
const BRAND_SLOGAN_ZH = getBrandSlogan('zh-CN');

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

  // ============ iOS 外部支付导线屏蔽 (Apple 3.1.1) ============
  describe('iOS 外部支付导线屏蔽 (3.1.1)', () => {
    const WALLET = '我的钱包';
    const DELEGATE = '代付人设置';
    const BANNER = BRAND_BANNER;
    const BRAND_URL = brandConfig.baseURL;

    it('非 iOS 平台应显示我的钱包和代付人设置', () => {
      (window as any)._platform.os = 'macos';
      render(<Account />);
      expect(screen.getByText(WALLET)).toBeTruthy();
      expect(screen.getByText(DELEGATE)).toBeTruthy();
    });

    it('iOS 平台不应显示我的钱包（外部钱包/支付）', () => {
      (window as any)._platform.os = 'ios';
      render(<Account />);
      expect(screen.queryByText(WALLET)).toBeNull();
    });

    it('iOS 平台不应显示代付人设置（第三方代付）', () => {
      (window as any)._platform.os = 'ios';
      render(<Account />);
      expect(screen.queryByText(DELEGATE)).toBeNull();
    });

    it('非 iOS 平台品牌横幅点击应打开品牌官网', () => {
      (window as any)._platform.os = 'macos';
      render(<Account />);
      fireEvent.click(screen.getByText(BANNER));
      expect((window as any)._platform.openExternal).toHaveBeenCalledWith(BRAND_URL);
    });

    it('iOS 平台品牌横幅不应外链到品牌官网（Apple 点名 URL）', () => {
      (window as any)._platform.os = 'ios';
      render(<Account />);
      fireEvent.click(screen.getByText(BANNER));
      expect((window as any)._platform.openExternal).not.toHaveBeenCalledWith(BRAND_URL);
    });
  });

  // ==================== Task 3: Slogan 延迟显示 ====================
  describe('Slogan 延迟显示', () => {
    // Slogan comes from the brand registry (brands/i18n-vars getBrandSlogan),
    // resolved for the zh-CN test locale.
    const findSlogan = () => {
      const sloganElements = screen.queryAllByText(BRAND_SLOGAN_ZH);
      return sloganElements.length > 0 ? sloganElements[0] : null;
    };

    it('应始终显示 slogan', () => {
      render(<Account />);
      expect(findSlogan()).not.toBeNull();
    });

    it('slogan 来自当前品牌，不含跨品牌标识', () => {
      render(<Account />);
      const slogan = findSlogan();
      expect(slogan).not.toBeNull();
      // Assert on what the component actually painted, not on the module-level
      // constant: `expect(BRAND_SLOGAN_ZH).not.toMatch(...)` only restates that
      // the registry is clean — it passes even if Account renders a hardcoded
      // cross-brand string, which is precisely the regression this guards.
      expect(slogan!.textContent).toBe(BRAND_SLOGAN_ZH);
      const forbidden = brandConfig.id === 'overleap' ? /开途|開途|Kaitu/ : /Overleap/;
      expect(slogan!.textContent).not.toMatch(forbidden);
    });
  });

  // ==================== Task 13: 密码入口 ====================
  describe('密码入口', () => {
    it('user.hasPassword=true 时显示 "修改密码"', () => {
      vi.mocked(useUser).mockReturnValue({
        user: createMockUser({ hasPassword: true }),
        loading: false,
        isMembership: true,
        isExpired: false,
        fetchUser: vi.fn(),
      } as any);

      render(<Account />);
      expect(screen.getByText('修改密码')).toBeTruthy();
      expect(screen.queryByText('设置密码')).toBeNull();
    });

    it('user.hasPassword=false 时显示 "设置密码"', () => {
      vi.mocked(useUser).mockReturnValue({
        user: createMockUser({ hasPassword: false }),
        loading: false,
        isMembership: true,
        isExpired: false,
        fetchUser: vi.fn(),
      } as any);

      render(<Account />);
      expect(screen.getByText('设置密码')).toBeTruthy();
      expect(screen.queryByText('修改密码')).toBeNull();
    });
  });
});
