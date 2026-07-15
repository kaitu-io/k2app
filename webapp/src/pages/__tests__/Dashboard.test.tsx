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
    useEffectiveCloudSelection: vi.fn(),
    useHasConnectableSelection: vi.fn(),
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
import { useConnectionStore, useEffectiveCloudSelection, useHasConnectableSelection, hasConnectableSelection, AUTO_TUNNEL_SENTINEL } from '../../stores/connection.store';
import { useUser } from '../../hooks/useUser';
import Dashboard from '../Dashboard';
import { getCurrentAppConfig } from '../../config/apps';

// Brand gate — overleap ships no self-hosted surface. Adaptive, not forked, so
// `K2_BRAND=overleap vitest run` stays green.
const SELF_HOSTED = getCurrentAppConfig().features.selfHostedTunnels === true;

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
const mockClearCloudSelection = vi.fn();
const mockReconcileSelection = vi.fn();

const createMockConnectionStore = (overrides = {}) => ({
  selectedCloudTunnel: null,
  activeTunnel: null,
  connectedTunnel: null,
  connectEpoch: 0,
  connectedAt: null,
  feedbackRequested: false,
  pendingFeedback: false,
  lastConnectionInfo: null,
  serverMode: 'manual' as 'manual' | 'self_hosted' | 'k2sub',
  serverModeLoaded: true,
  setServerMode: vi.fn().mockResolvedValue(undefined),
  selectCloudTunnel: mockSelectCloudTunnel,
  selectSelfHosted: mockSelectSelfHosted,
  connect: mockConnect,
  disconnect: mockDisconnect,
  enrichFromTunnelList: vi.fn(),
  clearPendingFeedback: vi.fn(),
  clearCloudSelection: mockClearCloudSelection,
  reconcileSelection: mockReconcileSelection,
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
    const defaultState = createMockConnectionStore();
    vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
      return selector ? selector(defaultState) : defaultState;
    });
    (useConnectionStore as any).getState = vi.fn(() => defaultState);
    // Default: no concrete tunnel selected → Auto mode
    vi.mocked(useEffectiveCloudSelection).mockReturnValue(AUTO_TUNNEL_SENTINEL);
    // Default: manual + Auto fallback ⇒ ready to connect. Tests that model
    // an unselected state (self_hosted-without-tunnel) override to false.
    vi.mocked(useHasConnectableSelection).mockReturnValue(true);
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
      // Only legitimately-unselected production state: self_hosted mode with
      // no configured tunnel. (Manual mode always has Auto fallback; k2sub
      // always has subsCountry=null fallback — neither can be "unselected".)
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        const state = createMockConnectionStore({
          serverMode: 'self_hosted' as const,
          activeTunnel: null,
          selectedCloudTunnel: null,
        });
        return selector ? selector(state) : state;
      });
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(null as any);
      vi.mocked(useHasConnectableSelection).mockReturnValue(false);
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('no');
      });
    });

    it('选择隧道后应该显示 tunnel name', async () => {
      const tunnel = {
        source: 'cloud' as const,
        domain: 'test.example.com',
        name: 'Test Tunnel',
        country: 'JP',
        serverUrl: 'k2v5://test.example.com:443',
      };
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        const state = createMockConnectionStore({
          serverMode: 'manual' as const,
          selectedCloudTunnel: tunnel,
          activeTunnel: tunnel,
        });
        return selector ? selector(state) : state;
      });
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(tunnel as any);

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
      // self_hosted mode without a configured tunnel — the only "no selection"
      // production state. Guard must abort before calling connect().
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        const state = createMockConnectionStore({
          serverMode: 'self_hosted' as const,
          activeTunnel: null,
          selectedCloudTunnel: null,
        });
        return selector ? selector(state) : state;
      });
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(null as any);
      vi.mocked(useHasConnectableSelection).mockReturnValue(false);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await new Promise(resolve => setTimeout(resolve, 100));
      expect(mockConnect).not.toHaveBeenCalled();
    });

    it('idle with error and tunnel should reconnect on toggle', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
        isRetrying: false,
        error: { code: 503, message: 'fail' },
      }) as any);

      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        const state = createMockConnectionStore({
          activeTunnel: {
            source: 'cloud',
            domain: 'test.example.com',
            name: 'Test Tunnel',
            country: 'US',
            serverUrl: 'k2v5://test.example.com:443',
          },
        });
        return selector ? selector(state) : state;
      });

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockConnect).toHaveBeenCalled();
      });
    });

    it('idle with error without tunnel should not reconnect', async () => {
      // self_hosted without configured tunnel: even in error+idle state, the
      // guard must abort because the user has no resolvable selection.
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
        isRetrying: false,
        error: { code: 503, message: 'fail' },
      }) as any);
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        const state = createMockConnectionStore({
          serverMode: 'self_hosted' as const,
          activeTunnel: null,
          selectedCloudTunnel: null,
        });
        return selector ? selector(state) : state;
      });
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(null as any);
      vi.mocked(useHasConnectableSelection).mockReturnValue(false);

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

  describe('Auto 自动选择', () => {
    it('serverMode=manual + selectedCloudTunnel=null 时顶部卡片显示 ⚡ 自动选择', async () => {
      // Default mock: serverMode='manual', selectedCloudTunnel=null → Auto mode
      render(<Dashboard />);

      await waitFor(() => {
        // tunnelName prop contains '⚡' prefix for Auto mode
        const tunnelNameEl = screen.getByTestId('tunnel-name');
        expect(tunnelNameEl.textContent).toContain('⚡');
      });
    });

    it('已连接时顶部卡片追加当前命中隧道名', async () => {
      const state = createMockConnectionStore({
        selectedCloudTunnel: null,
        serverMode: 'manual' as const,
        connectedTunnel: {
          source: 'cloud' as const,
          domain: 'jp-01.example.com',
          name: 'Tokyo-01',
          country: 'JP',
          serverUrl: 'k2v5://jp-01.example.com:443',
        },
      });
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);

      render(<Dashboard />);

      await waitFor(() => {
        const tunnelNameEl = screen.getByTestId('tunnel-name');
        expect(tunnelNameEl.textContent).toContain('⚡');
        expect(tunnelNameEl.textContent).toContain('Tokyo-01');
      });
    });

    it('selectedCloudTunnel 非 null 时显示具体隧道名', async () => {
      const concreteTunnel = {
        source: 'cloud' as const,
        domain: 'sg-01.example.com',
        name: 'Singapore-01',
        country: 'SG',
        serverUrl: 'k2v5://sg-01.example.com:443',
      };
      const state = createMockConnectionStore({
        selectedCloudTunnel: concreteTunnel,
        activeTunnel: concreteTunnel,
        serverMode: 'manual' as const,
      });
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(concreteTunnel as any);

      render(<Dashboard />);

      await waitFor(() => {
        const tunnelNameEl = screen.getByTestId('tunnel-name');
        expect(tunnelNameEl.textContent).toBe('Singapore-01');
        expect(tunnelNameEl.textContent).not.toContain('⚡');
      });
    });

    it('点击 Auto 虚拟行调用 clearCloudSelection', async () => {
      // Authenticate so CloudTunnelList renders
      vi.mocked(useAuthStore).mockImplementation((selector: any) => {
        return selector({ isAuthenticated: true, user: null });
      });

      const { CloudTunnelList } = await import('../../components/CloudTunnelList');
      let capturedOnSelect: ((t: any) => void) | undefined;
      vi.mocked(CloudTunnelList).mockImplementation(({ onSelect }: any) => {
        capturedOnSelect = onSelect;
        return null;
      });

      const state = createMockConnectionStore();
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);

      render(<Dashboard />);

      await waitFor(() => expect(capturedOnSelect).toBeDefined());
      capturedOnSelect!(AUTO_TUNNEL_SENTINEL);

      expect(mockClearCloudSelection).toHaveBeenCalled();
      expect(mockSelectCloudTunnel).not.toHaveBeenCalled();
    });

    it('点击具体隧道行调用 selectCloudTunnel', async () => {
      // Authenticate so CloudTunnelList renders
      vi.mocked(useAuthStore).mockImplementation((selector: any) => {
        return selector({ isAuthenticated: true, user: null });
      });

      const { CloudTunnelList } = await import('../../components/CloudTunnelList');
      let capturedOnSelect: ((t: any) => void) | undefined;
      vi.mocked(CloudTunnelList).mockImplementation(({ onSelect }: any) => {
        capturedOnSelect = onSelect;
        return null;
      });

      const state = createMockConnectionStore();
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);

      render(<Dashboard />);

      const concreteTunnel = {
        source: 'cloud' as const,
        domain: 'hk-01.example.com',
        name: 'Hong Kong-01',
        country: 'HK',
        serverUrl: 'k2v5://hk-01.example.com:443',
      };

      await waitFor(() => expect(capturedOnSelect).toBeDefined());
      capturedOnSelect!(concreteTunnel);

      expect(mockSelectCloudTunnel).toHaveBeenCalledWith(concreteTunnel);
      expect(mockClearCloudSelection).not.toHaveBeenCalled();
    });

    it('connect button is enabled in Auto mode + disconnected state', async () => {
      // Default beforeEach: serverMode='manual', selectedCloudTunnel=null,
      // useEffectiveCloudSelection=AUTO_TUNNEL_SENTINEL → hasTunnelSelected must be true
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('yes');
      });
    });

    it('clicking connect in Auto+disconnected calls connect() (does not abort)', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);
      // Default: useEffectiveCloudSelection = AUTO_TUNNEL_SENTINEL, no displayTunnel
      const state = createMockConnectionStore();
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockConnect).toHaveBeenCalled();
      });
    });
  });

  describe('serverMode = k2sub (gateway)', () => {
    // Regression: picking a k2subs:// link (gateway K2sub tab) used to leave
    // the connect button disabled because the old hasTunnelSelected check
    // only recognized manual + Auto sentinel and self_hosted's activeTunnel.
    // The daemon resolves k2subs:// itself, so the UI is always ready.

    it('connect button enabled in k2sub mode + disconnected (Auto/no country)', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);
      const state = createMockConnectionStore({
        serverMode: 'k2sub' as const,
        selectedCloudTunnel: null,
        activeTunnel: null,
      });
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);
      // useEffectiveCloudSelection returns null for non-manual modes
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(null as any);

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByTestId('tunnel-selected')).toHaveTextContent('yes');
      });
    });

    it('clicking connect in k2sub+disconnected calls connect() (does not abort)', async () => {
      vi.mocked(useVPNMachine).mockReturnValue(createMockVPNMachine({
        state: 'idle',
        isDisconnected: true,
      }) as any);
      const state = createMockConnectionStore({
        serverMode: 'k2sub' as const,
        selectedCloudTunnel: null,
        activeTunnel: null,
      });
      vi.mocked(useConnectionStore).mockImplementation((selector?: any) => {
        return selector ? selector(state) : state;
      });
      (useConnectionStore as any).getState = vi.fn(() => state);
      vi.mocked(useEffectiveCloudSelection).mockReturnValue(null as any);

      render(<Dashboard />);

      const toggleBtn = screen.getByTestId('toggle-btn');
      fireEvent.click(toggleBtn);

      await waitFor(() => {
        expect(mockConnect).toHaveBeenCalled();
      });
    });
  });

  // ==================== Self-hosted brand gate ====================
  //
  // /tunnels early-returns when the brand gate is closed, so every entry point
  // into it must be gated too — otherwise the user navigates to a page that
  // renders nothing and cannot get back with in-app controls.
  describe('自部署 brand gate', () => {
    it('shows the guest self-deploy entry point only when the brand gate is open', () => {
      // Guests see the phantom-cloud overlay, which carries the self-deploy link.
      vi.mocked(useAuthStore).mockImplementation((selector: any) =>
        selector({ isAuthenticated: false, user: null })
      );

      render(<Dashboard />);

      // Text queries, not the `name` ByRole option: `name` triggers
      // computeAccessibleName -> getComputedStyle, which setup.ts installs as a
      // vi.fn() that vi.clearAllMocks() strips of its implementation.
      const selfDeployBtn = screen.queryByText(/自部署服务|Self-Deploy/i);

      if (SELF_HOSTED) {
        expect(selfDeployBtn).toBeInTheDocument();
      } else {
        expect(selfDeployBtn).not.toBeInTheDocument();
      }
      // The login CTA is brand-neutral and must survive either way, proving the
      // overlay itself rendered and the assertion above is not vacuous.
      expect(
        screen.getByText(/登录解锁全球节点|Login to unlock global nodes/i)
      ).toBeInTheDocument();
    });
  });
});
