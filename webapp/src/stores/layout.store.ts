/**
 * Layout Store - 响应式布局管理
 *
 * 职责：
 * - 管理布局模式（mobile/desktop）
 * - 根据屏幕宽度自动切换
 * - 支持路由器模式（强制桌面布局）
 *
 * 使用：
 * ```tsx
 * const isMobile = useLayoutStore(s => s.isMobile);
 * const isDesktop = useLayoutStore(s => s.isDesktop);
 * ```
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';

type LayoutMode = 'mobile' | 'desktop';

const DESKTOP_BREAKPOINT = 768;
const SIDEBAR_WIDTH = 220;

interface LayoutState {
  // 状态
  layoutMode: LayoutMode;
  isRouterMode: boolean;
  sidebarWidth: number;
  connectionButtonCollapsed: boolean;

  // Getters (派生状态)
  isMobile: boolean;
  isDesktop: boolean;

  // Actions
  setLayoutMode: (mode: LayoutMode) => void;
  setConnectionButtonCollapsed: (collapsed: boolean) => void;
  toggleConnectionButtonCollapsed: () => void;
}

// 计算初始布局模式
function getInitialLayoutMode(isRouterMode: boolean): LayoutMode {
  if (isRouterMode) {
    return 'desktop';
  }
  if (typeof window !== 'undefined') {
    return window.innerWidth >= DESKTOP_BREAKPOINT ? 'desktop' : 'mobile';
  }
  return 'mobile';
}

// 检测是否为路由器模式
const isRouterMode =
  typeof import.meta !== 'undefined' &&
  (import.meta as any).env?.VITE_CLIENT_IS_ROUTER === 'true';

export const useLayoutStore = create<LayoutState>()(
  subscribeWithSelector((set, get) => ({
    // 初始状态
    layoutMode: getInitialLayoutMode(isRouterMode),
    isRouterMode,
    sidebarWidth: SIDEBAR_WIDTH,
    // 折叠状态：桌面版默认展开，路由器版默认折叠
    connectionButtonCollapsed: isRouterMode,

    // 派生状态（直接存储以便精准订阅）
    isMobile: getInitialLayoutMode(isRouterMode) === 'mobile',
    isDesktop: getInitialLayoutMode(isRouterMode) === 'desktop',

    // Actions
    setLayoutMode: (mode) =>
      set({
        layoutMode: mode,
        isMobile: mode === 'mobile',
        isDesktop: mode === 'desktop',
      }),

    setConnectionButtonCollapsed: (collapsed) =>
      set({ connectionButtonCollapsed: collapsed }),

    toggleConnectionButtonCollapsed: () =>
      set({ connectionButtonCollapsed: !get().connectionButtonCollapsed }),
  }))
);

/**
 * 初始化 Layout Store
 * 设置响应式断点监听
 */
export function initializeLayoutStore(): () => void {
  const { isRouterMode } = useLayoutStore.getState();

  // 路由器模式下不需要监听
  if (isRouterMode) {
    useLayoutStore.setState({
      layoutMode: 'desktop',
      isMobile: false,
      isDesktop: true,
    });
    return () => {};
  }

  // 使用 matchMedia 监听屏幕宽度变化
  const mediaQuery = window.matchMedia(`(min-width: ${DESKTOP_BREAKPOINT}px)`);

  const handleMediaChange = (e: MediaQueryListEvent | MediaQueryList) => {
    const mode: LayoutMode = e.matches ? 'desktop' : 'mobile';
    useLayoutStore.setState({
      layoutMode: mode,
      isMobile: mode === 'mobile',
      isDesktop: mode === 'desktop',
    });
  };

  // 初始检查
  handleMediaChange(mediaQuery);

  // 监听变化
  if (mediaQuery.addEventListener) {
    mediaQuery.addEventListener('change', handleMediaChange);
  } else {
    // 兼容旧版浏览器
    mediaQuery.addListener(handleMediaChange);
  }

  return () => {
    if (mediaQuery.removeEventListener) {
      mediaQuery.removeEventListener('change', handleMediaChange);
    } else {
      mediaQuery.removeListener(handleMediaChange);
    }
  };
}

// ============ 便捷 Hooks ============

/**
 * 获取布局信息
 */
export function useLayout() {
  return {
    layoutMode: useLayoutStore((s) => s.layoutMode),
    isMobile: useLayoutStore((s) => s.isMobile),
    isDesktop: useLayoutStore((s) => s.isDesktop),
    isRouterMode: useLayoutStore((s) => s.isRouterMode),
    sidebarWidth: useLayoutStore((s) => s.sidebarWidth),
    connectionButtonCollapsed: useLayoutStore((s) => s.connectionButtonCollapsed),
    toggleConnectionButtonCollapsed: useLayoutStore((s) => s.toggleConnectionButtonCollapsed),
  };
}

/**
 * 仅获取布局模式
 */
export function useLayoutMode(): LayoutMode {
  return useLayoutStore((s) => s.layoutMode);
}
