/**
 * Account 页「我的路由器」入口。
 *
 * Task 5（App 内 webapp 收口）：专属节点/线路已改为路由器版官网自助购买，
 * App 内不再有任何售卖面。Account 的入口只在用户已有线路时展示——不再看
 * `features.privateNode`（那是旧的售卖面开关，控制"购买专属线路"按钮是否
 * 出现），标签也从「专属节点」改为「我的路由器」。
 *
 * 专属节点功能本身仍是开途专属（overleap 从不出售，因此永远不会有节点，
 * 入口在 overleap 上也就永远不出现）——有效行为按品牌门控分支：kaitu 起
 * 真实的入口显隐 + 跳转断言；overleap 起一个不依赖 Account 渲染的独立真实
 * 断言（/private-node 路由的注册门 `appConfig.features.privateNode` 为
 * false），照 Account.brand-surfaces.test.tsx 的写法。
 *
 * Mock 方式参照 Account.brand-surfaces.test.tsx。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
import { render } from '../../test/utils/render';

vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return { ...actual, useAuth: vi.fn() };
});
vi.mock('../../hooks/useUser', () => ({ useUser: vi.fn() }));
vi.mock('../../contexts/ThemeContext', () => ({
  useTheme: vi.fn(() => ({ themeMode: 'dark', setThemeMode: vi.fn() })),
}));
vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: vi.fn(() => ({ links: { walletUrl: 'https://example.com/wallet' } })),
}));
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { request: vi.fn(), post: vi.fn() },
}));
vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return { ...actual, useLoginDialogStore: { getState: vi.fn(() => ({ open: vi.fn() })) } };
});
vi.mock('../../components/VersionItem', () => ({
  default: vi.fn(() => <div data-testid="version-item" />),
}));
vi.mock('../../components/BetaChannelToggle', () => ({ default: vi.fn(() => null) }));

const usePrivateNodesMock = vi.fn();
vi.mock('../../hooks/usePrivateNodes', () => ({
  usePrivateNodes: () => usePrivateNodesMock(),
}));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';
import { brandConfig } from '../../brands';
import { getCurrentAppConfig } from '../../config/apps';
import type { PrivateNodeSubscriptionView } from '../../services/api-types';

function makeNode(): PrivateNodeSubscriptionView {
  return {
    id: 1,
    status: 'active',
    isServiceable: true,
    region: 'ap-northeast-1',
    ipType: 'non_residential',
    trafficTotalBytes: 100 * 1024 ** 3,
    trafficUsedBytes: 10 * 1024 ** 3,
    purchasedAt: 1_700_000_000,
    expiresAt: 1_800_000_000,
    graceUntil: 0,
    suspendUntil: 0,
    planLabel: '路由器版',
    quotaExhausted: false,
  };
}

function mountLoggedInOnDesktop() {
  (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
  (window as any)._platform = { os: 'macos', version: '0.4.10', openExternal: vi.fn() };
  vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
  vi.mocked(useUser).mockReturnValue({
    user: {
      id: 1,
      expiredAt: '2027-01-01T00:00:00Z',
      loginIdentifies: [{ type: 'email', value: 'test@example.com' }],
    },
    loading: false,
    isMembership: true,
    isExpired: false,
    fetchUser: vi.fn(),
  } as any);
  return render(<Account />);
}

describe('Account — 专属节点/我的路由器入口', () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
  });

  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
    localStorage.clear();
  });

  describe.runIf(brandConfig.features.privateNode)('kaitu: 入口只看是否已有线路', () => {
    it('没有线路 → 不渲染入口', () => {
      usePrivateNodesMock.mockReturnValue({ nodes: [], loading: false, error: null, refresh: vi.fn() });
      mountLoggedInOnDesktop();
      expect(screen.queryByText('我的路由器')).not.toBeInTheDocument();
    });

    it('有一条线路 → 渲染入口，点击跳转 /private-node', () => {
      usePrivateNodesMock.mockReturnValue({
        nodes: [makeNode()],
        loading: false,
        error: null,
        refresh: vi.fn(),
      });
      mountLoggedInOnDesktop();
      const entry = screen.getByText('我的路由器');
      expect(entry).toBeInTheDocument();
      fireEvent.click(entry);
      expect(navigateMock).toHaveBeenCalledWith('/private-node');
    });
  });

  // overleap 从不销售专属线路：/private-node 路由本身不注册（App.tsx 门在
  // appConfig.features.privateNode 上），断言不依赖 Account 组件渲染。
  it.skipIf(brandConfig.id !== 'overleap')(
    'overleap: /private-node 路由门关闭（功能开关为 false）',
    () => {
      expect(getCurrentAppConfig().features.privateNode).toBe(false);
    },
  );
});
