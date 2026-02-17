/**
 * Auth Store - 认证状态管理
 *
 * 职责：
 * - 管理用户认证状态（isAuthenticated, isAuthChecking）
 * - 同步服务端认证状态
 *
 * 注意：
 * - 认证错误处理已移至 k2api 统一处理
 * - 会员过期状态从 user.expiredAt 计算，不再维护 isMembershipExpired
 *
 * 使用：
 * ```tsx
 * const isAuthenticated = useAuthStore(s => s.isAuthenticated);
 * const { setIsAuthenticated } = useAuthStore();
 * ```
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';

interface AuthState {
  // 状态
  isAuthenticated: boolean;
  isAuthChecking: boolean;

  // Actions
  setIsAuthenticated: (value: boolean) => void;
  setIsAuthChecking: (value: boolean) => void;

  // 初始化同步（内部使用）
  syncAuthStatus: () => Promise<void>;
}

export const useAuthStore = create<AuthState>()(
  subscribeWithSelector((set) => ({
    // 初始状态
    isAuthenticated: false,
    isAuthChecking: true,

    // Actions
    setIsAuthenticated: (value) => set({ isAuthenticated: value }),
    setIsAuthChecking: (value) => set({ isAuthChecking: value }),

    // 同步认证状态
    // 假设已登录，如果收到 401 错误会通过 k2api 自动处理
    syncAuthStatus: async () => {
      set({ isAuthenticated: true, isAuthChecking: false });
    },
  }))
);

/**
 * 初始化 Auth Store
 * 在应用启动时调用
 */
export function initializeAuthStore(): () => void {
  // 立即同步认证状态
  useAuthStore.getState().syncAuthStatus();

  // 返回清理函数（当前无需清理）
  return () => {};
}

// ============ 便捷 Hooks ============

/**
 * 获取认证状态
 */
export function useAuth() {
  return {
    isAuthenticated: useAuthStore((s) => s.isAuthenticated),
    setIsAuthenticated: useAuthStore((s) => s.setIsAuthenticated),
  };
}

/**
 * 获取认证检查状态
 */
export function useAuthChecking(): boolean {
  return useAuthStore((s) => s.isAuthChecking);
}
