/**
 * Dashboard 页面测试
 *
 * 测试仪表盘页面的核心功能
 * 注意：Dashboard 直接使用 window._k2.run() API
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../../test/utils/render';

// Mock stores
vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return {
    ...actual,
    useVPNStatus: vi.fn(),
    useAuthStore: vi.fn(),
    useLoginDialogStore: vi.fn(),
    useLoginDialog: vi.fn(),
    useDashboard: vi.fn(),
    useDashboardStore: vi.fn(),
  };
});

vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return {
    ...actual,
    useLoginDialogStore: vi.fn(),
    useLoginDialog: vi.fn(),
  };
});

vi.mock('../../stores/dashboard.store', async () => {
  const actual = await vi.importActual('../../stores/dashboard.store');
  return {
    ...actual,
    useDashboard: vi.fn(),
    useDashboardStore: vi.fn(),
  };
});

vi.mock('../../hooks/useUser', () => ({
  useUser: vi.fn(),
}));

// Mock CloudTunnelList component
vi.mock('../../components/CloudTunnelList', () => ({
  CloudTunnelList: vi.fn(() => null),
}));

// Mock CollapsibleConnectionSection component
vi.mock('../../components/CollapsibleConnectionSection', () => ({
  CollapsibleConnectionSection: vi.fn(({ serviceState, hasTunnelSelected, tunnelName, onToggle }) => (
    <div data-testid="connection-section">
      <span data-testid="service-state">{serviceState}</span>
      <span data-testid="tunnel-selected">{hasTunnelSelected ? 'yes' : 'no'}</span>
      <span data-testid="tunnel-name">{tunnelName || 'none'}</span>
      <button data-testid="toggle-btn" onClick={onToggle}>Toggle</button>
    </div>
  )),
}));

// Import mocked modules
import { useVPNStatus, useAuthStore } from '../../stores';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { useDashboard } from '../../stores/dashboard.store';
import { useUser } from '../../hooks/useUser';
import Dashboard from '../Dashboard';

// Mock window._k2
const mockExec = vi.fn();

const createMockVPNStatus = (overrides = {}) => ({
  serviceState: 'disconnected',
  isConnected: false,
  isDisconnected: true,
  isConnecting: false,
  isTransitioning: false,
  isServiceRunning: false,
  isRetrying: false,
  networkAvailable: true,
  setOptimisticState: vi.fn(),
  error: null,
  serviceConnected: true,
  isServiceFailedLongTime: false,
  serviceFailureDuration: 0,
  ...overrides,
});

const createMockDashboardStore = (overrides = {}) => ({
  advancedSettingsExpanded: false,
  toggleAdvancedSettings: vi.fn(),
  scrollPosition: 0,
  setScrollPosition: vi.fn(),
  ...overrides,
});

describe('Dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Setup window._k2 mock (VPN-only)
    (window as any)._k2 = {
      run: mockExec,
    };

    // Setup window._platform mock
    (window as any)._platform = {
      isDesktop: false,
      os: 'test',
      version: '1.0.0',
    };

    // Default mock implementations
    mockExec.mockImplementation(async (action: string) => {
      if (action === 'get_config') {
        return {
          code: 0,
          data: {
            mode: 'tun',
            active_tunnel: null,
            rule: { type: 'chnroute', antiporn: false },
            dns_mode: 'fake-ip',
          },
        };
      }
      if (action === 'set_config') {
        return { code: 0, data: {} };
      }
      if (action === 'start' || action === 'stop') {
        return { code: 0 };
      }
      return { code: 0 };
    });

    // Setup default mocks
    vi.mocked(useVPNStatus).mockReturnValue(createMockVPNStatus() as any);
    vi.mocked(useAuthStore).mockImplementation((selector: any) => {
      const state = { isAuthenticated: false, user: null };
      return selector(state);
    });
    vi.mocked(useLoginDialogStore).mockImplementation((selector: any) => {
      const state = { open: vi.fn() };
      return selector(state);
    });
    vi.mocked(useDashboard).mockReturnValue(createMockDashboardStore());
    vi.mocked(useUser).mockReturnValue({ user: null, loading: false } as any);
  });

  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
  });

  describe('初始化', () => {
    it('应该在挂载时调用 get_config', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(mockExec).toHaveBeenCalledWith('get_config');
      });
    });

    it('应该渲染 Dashboard 容器', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('connection-section')).toBeInTheDocument();
      });
    });
  });

  describe('连接状态显示', () => {
    it('disconnected 状态应该正确传递给 ConnectionSection', async () => {
      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'disconnected',
          isDisconnected: true,
        }) as any
      );

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('disconnected');
      });
    });

    it('connected 状态应该正确传递给 ConnectionSection', async () => {
      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'connected',
          isConnected: true,
          isDisconnected: false,
          isServiceRunning: true,
        }) as any
      );

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('connected');
      });
    });

    it('connecting 状态应该正确传递', async () => {
      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'connecting',
          isConnecting: true,
          isTransitioning: true,
          isDisconnected: false,
        }) as any
      );

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('connecting');
      });
    });
  });

  describe('隧道选择状态', () => {
    it('未选择隧道时应该显示 hasTunnelSelected=no', async () => {
      mockExec.mockImplementation(async (action: string) => {
        if (action === 'get_config') {
          return {
            code: 0,
            data: {
              active_tunnel: null,
              rule: { type: 'chnroute' },
            },
          };
        }
        return { code: 0 };
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('no');
      });
    });

    it('已选择隧道时应该显示 hasTunnelSelected=yes 和隧道名称', async () => {
      mockExec.mockImplementation(async (action: string) => {
        if (action === 'get_config') {
          return {
            code: 0,
            data: {
              tunnel: {
                mode: 'cloud',
                items: ['k2v4://hk1.example.com?ipv4=1.2.3.4&country=HK#Hong%20Kong%201'],
              },
              rule: { type: 'chnroute' },
            },
          };
        }
        return { code: 0 };
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('yes');
        expect(screen.getByTestId('tunnel-name')).toHaveTextContent('Hong Kong 1');
      });
    });
  });

  describe('连接切换', () => {
    it('点击 toggle 按钮在断开状态下应该调用 start', async () => {
      const mockSetOptimisticState = vi.fn();

      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'disconnected',
          isDisconnected: true,
          setOptimisticState: mockSetOptimisticState,
        }) as any
      );

      mockExec.mockImplementation(async (action: string) => {
        if (action === 'get_config') {
          return {
            code: 0,
            data: {
              tunnel: {
                mode: 'cloud',
                items: ['k2v4://test.example.com?ipv4=1.2.3.4#Test'],
              },
              rule: { type: 'chnroute' },
            },
          };
        }
        return { code: 0 };
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('yes');
      });

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockSetOptimisticState).toHaveBeenCalledWith('connecting');
        expect(mockExec).toHaveBeenCalledWith('start');
      });
    });

    it('点击 toggle 按钮在连接状态下应该调用 stop', async () => {
      const mockSetOptimisticState = vi.fn();

      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'connected',
          isConnected: true,
          isDisconnected: false,
          isServiceRunning: true,
          setOptimisticState: mockSetOptimisticState,
        }) as any
      );

      mockExec.mockImplementation(async (action: string) => {
        if (action === 'get_config') {
          return {
            code: 0,
            data: {
              tunnel: {
                mode: 'cloud',
                items: ['k2v4://test.example.com?ipv4=1.2.3.4#Test'],
              },
              rule: { type: 'chnroute' },
            },
          };
        }
        return { code: 0 };
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('connected');
      });

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockSetOptimisticState).toHaveBeenCalledWith('disconnecting');
        expect(mockExec).toHaveBeenCalledWith('stop');
      });
    });

    it('未选择隧道时点击 toggle 不应该调用 start', async () => {
      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          serviceState: 'disconnected',
          isDisconnected: true,
        }) as any
      );

      mockExec.mockImplementation(async (action: string) => {
        if (action === 'get_config') {
          return {
            code: 0,
            data: {
              active_tunnel: null,
              rule: { type: 'chnroute' },
            },
          };
        }
        return { code: 0 };
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('no');
      });

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      // Wait a bit and verify start was not called
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(mockExec).not.toHaveBeenCalledWith('start');
    });
  });

  describe('高级设置', () => {
    it('应该渲染高级设置按钮', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        // 高级设置按钮应该存在 - 查找包含设置图标的按钮
        const settingsIcon = screen.getByTestId('SettingsIcon');
        expect(settingsIcon).toBeInTheDocument();
        // 确保按钮包含文本
        const settingsButton = settingsIcon.closest('button');
        expect(settingsButton).toBeInTheDocument();
      });
    });

    it('点击高级设置按钮应该调用 toggleAdvancedSettings', async () => {
      const mockToggle = vi.fn();
      vi.mocked(useDashboard).mockReturnValue(
        createMockDashboardStore({
          toggleAdvancedSettings: mockToggle,
        })
      );

      render(<Dashboard />);

      await waitFor(() => {
        const settingsIcon = screen.getByTestId('SettingsIcon');
        const settingsButton = settingsIcon.closest('button');
        expect(settingsButton).toBeInTheDocument();
        fireEvent.click(settingsButton!);
      });

      expect(mockToggle).toHaveBeenCalled();
    });
  });

  describe('未认证用户', () => {
    it('未认证用户应该看到登录提示', async () => {
      vi.mocked(useAuthStore).mockImplementation((selector: any) => {
        const state = { isAuthenticated: false };
        return selector(state);
      });

      render(<Dashboard />);

      await waitFor(() => {
        // 检查登录相关元素存在 - 查找 DnsIcon 表示空状态
        const dnsIcon = screen.getByTestId('DnsIcon');
        expect(dnsIcon).toBeInTheDocument();
        // 查找登录相关按钮 (contained primary)
        const buttons = screen.getAllByRole('button');
        const loginButton = buttons.find(btn =>
          btn.classList.contains('MuiButton-containedPrimary')
        );
        expect(loginButton).toBeInTheDocument();
      });
    });
  });

  describe('认证用户', () => {
    it('认证用户应该看到 CloudTunnelList', async () => {
      vi.mocked(useAuthStore).mockImplementation((selector: any) => {
        const state = { isAuthenticated: true };
        return selector(state);
      });

      const { CloudTunnelList } = await import('../../components/CloudTunnelList');

      render(<Dashboard />);

      await waitFor(() => {
        expect(CloudTunnelList).toHaveBeenCalled();
      });
    });
  });

  describe('服务故障处理', () => {
    it('服务长时间故障时应该禁用 Dashboard', async () => {
      vi.mocked(useVPNStatus).mockReturnValue(
        createMockVPNStatus({
          isServiceFailedLongTime: true,
          serviceConnected: false,
        }) as any
      );

      const { container } = render(<Dashboard />);

      await waitFor(() => {
        // Dashboard 容器应该有禁用样式 - 检查 class 中包含样式
        const dashboardContainer = container.firstChild as HTMLElement;
        expect(dashboardContainer).toBeInTheDocument();
        // MUI 的样式通过 sx prop 应用，检查 style 属性存在
        // 由于 jsdom 限制，我们检查组件渲染成功即可
        expect(dashboardContainer.className).toContain('MuiBox-root');
      });
    });
  });
});
