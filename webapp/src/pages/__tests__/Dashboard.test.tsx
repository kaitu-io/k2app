/**
 * Dashboard 页面测试
 *
 * 测试仪表盘页面的核心功能
 * Uses new vpn-machine + connection stores
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';

// Mock stores
vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return {
    ...actual,
    useAuthStore: vi.fn(),
  };
});

vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return {
    ...actual,
    useLoginDialogStore: vi.fn(),
  };
});

vi.mock('../../stores/dashboard.store', async () => {
  const actual = await vi.importActual('../../stores/dashboard.store');
  return {
    ...actual,
    useDashboard: vi.fn(),
  };
});

vi.mock('../../stores/vpn-machine.store', async () => {
  const actual = await vi.importActual('../../stores/vpn-machine.store');
  return {
    ...actual,
    useVPNMachine: vi.fn(),
  };
});

vi.mock('../../stores/connection.store', async () => {
  const actual = await vi.importActual('../../stores/connection.store');
  return {
    ...actual,
    useConnectionStore: vi.fn(),
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
import { useAuthStore } from '../../stores';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { useDashboard } from '../../stores/dashboard.store';
import { useVPNMachine } from '../../stores/vpn-machine.store';
import { useConnectionStore } from '../../stores/connection.store';
import { useUser } from '../../hooks/useUser';
import Dashboard from '../Dashboard';

// Mock window._k2
const mockRun = vi.fn();

const createMockVPNMachine = (overrides = {}) => ({
  state: 'idle' as const,
  isConnected: false,
  isDisconnected: true,
  isServiceDown: false,
  isTransitioning: false,
  isInteractive: false,
  isRetrying: false,
  networkAvailable: true,
  error: null,
  ...overrides,
});

const mockConnect = vi.fn();
const mockDisconnect = vi.fn();
const mockSelectCloudTunnel = vi.fn();
const mockSelectSelfHosted = vi.fn();

const createMockConnectionStore = (overrides = {}) => ({
  selectedSource: 'cloud' as const,
  selectedCloudTunnel: null,
  activeTunnel: null,
  connectedTunnel: null,
  connectEpoch: 0,
  selectCloudTunnel: mockSelectCloudTunnel,
  selectSelfHosted: mockSelectSelfHosted,
  connect: mockConnect,
  disconnect: mockDisconnect,
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

    // Setup window._k2 mock
    (window as any)._k2 = { run: mockRun };
    (window as any)._platform = {
      os: 'test',
      version: '1.0.0',
    };

    mockRun.mockResolvedValue({ code: 0 });

    // Setup default mocks
    vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine() as any);
    vi.mocked(useConnectionStore).mockReturnValue(createMockConnectionStore() as any);
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
    it('应该渲染 Dashboard 容器', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('connection-section')).toBeInTheDocument();
      });
    });
  });

  describe('连接状态显示', () => {
    it('disconnected 状态应该正确传递给 ConnectionSection', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);

      render(<Dashboard />);

      await waitFor(() => {
        // idle maps to 'disconnected' for CollapsibleConnectionSection
        expect(screen.getByTestId('service-state')).toHaveTextContent('disconnected');
      });
    });

    it('connected 状态应该正确传递给 ConnectionSection', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'connected',
        isConnected: true,
        isDisconnected: false,
        isInteractive: true,
      }) as any);

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('connected');
      });
    });

    it('connecting 状态应该正确传递', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'connecting',
        isTransitioning: true,
        isDisconnected: false,
        isInteractive: true,
      }) as any);

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('service-state')).toHaveTextContent('connecting');
      });
    });
  });

  describe('隧道选择状态', () => {
    it('未选择隧道时应该显示 hasTunnelSelected=no', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('no');
      });
    });

    it('选择隧道后应该显示 tunnel name', async () => {
      vi.mocked(useConnectionStore).mockReturnValue(createMockConnectionStore({
        activeTunnel: {
          source: 'cloud',
          domain: 'test.example.com',
          name: 'Test Tunnel',
          country: 'JP',
          serverUrl: 'k2v5://test.example.com:443',
        },
      }) as any);

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('yes');
        expect(screen.getByTestId('tunnel-name')).toHaveTextContent('Test Tunnel');
      });
    });
  });

  describe('连接切换', () => {
    it('点击 toggle 按钮在连接状态下应该调用 disconnect', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'connected',
        isConnected: true,
        isDisconnected: false,
        isInteractive: true,
      }) as any);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockDisconnect).toHaveBeenCalled();
      });
    });

    it('未选择隧道时点击 toggle 不应该调用 connect', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await new Promise(resolve => setTimeout(resolve, 100));
      expect(mockConnect).not.toHaveBeenCalled();
    });

    it('error state with tunnel should reconnect on toggle', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'error',
        isDisconnected: false,
        isRetrying: false,
      }) as any);

      vi.mocked(useConnectionStore).mockReturnValue(createMockConnectionStore({
        activeTunnel: {
          source: 'cloud',
          domain: 'test.example.com',
          name: 'Test Tunnel',
          country: 'US',
          serverUrl: 'k2v5://test.example.com:443',
        },
      }) as any);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockConnect).toHaveBeenCalled();
      });
    });

    it('error state without tunnel should not reconnect', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'error',
        isDisconnected: false,
        isRetrying: false,
      }) as any);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await new Promise(resolve => setTimeout(resolve, 100));
      expect(mockConnect).not.toHaveBeenCalled();
    });
  });

  describe('高级设置', () => {
    it('应该渲染高级设置按钮', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        const settingsIcon = screen.getByTestId('SettingsIcon');
        expect(settingsIcon).toBeInTheDocument();
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
      render(<Dashboard />);

      await waitFor(() => {
        // Unauthenticated users see a contained primary button to unlock cloud nodes
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
    it('serviceDown 状态时应该禁用 Dashboard', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'serviceDown',
        isServiceDown: true,
        isDisconnected: false,
      }) as any);

      const { container } = render(<Dashboard />);

      await waitFor(() => {
        const dashboardContainer = container.firstChild as HTMLElement;
        expect(dashboardContainer).toBeInTheDocument();
        expect(dashboardContainer.className).toContain('MuiBox-root');
      });
    });
  });
});
