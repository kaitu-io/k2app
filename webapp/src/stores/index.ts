/**
 * Zustand Stores - 中央导出
 *
 * 所有 store 的统一导出点
 */

// ============ Auth Store ============
export {
  useAuthStore,
  initializeAuthStore,
  useAuth,
  useAuthChecking,
} from './auth.store';

// ============ VPN Store ============
export {
  useVPNStore,
  initializeVPNStore,
  useVPNStatus,
} from './vpn.store';

// ============ Alert Store ============
export {
  useAlertStore,
  useAlert,
  useAlertState,
} from './alert.store';

// ============ Layout Store ============
export {
  useLayoutStore,
  initializeLayoutStore,
  useLayout,
  useLayoutMode,
} from './layout.store';

// ============ Login Dialog Store ============
export {
  useLoginDialogStore,
  useLoginDialog,
} from './login-dialog.store';

// ============ Dashboard Store ============
export {
  useDashboardStore,
  useDashboard,
} from './dashboard.store';

// ============ Config Store ============
export { useConfigStore } from './config.store';

// 内部导入（用于 initializeAllStores）
import { initializeAuthStore } from './auth.store';
import { initializeVPNStore } from './vpn.store';
import { initializeLayoutStore } from './layout.store';
import { useConfigStore } from './config.store';

/**
 * 初始化所有 Store
 * 在应用启动时调用
 *
 * 注意：新架构中，K2 通过 window._k2 注入，
 * 不再需要传递参数。
 *
 * @returns 清理函数
 */
export function initializeAllStores(): () => void {
  // 按依赖顺序初始化 stores
  const cleanupLayout = initializeLayoutStore();
  useConfigStore.getState().loadConfig(); // fire-and-forget, sets loaded=true when done
  const cleanupAuth = initializeAuthStore();
  const cleanupVPN = initializeVPNStore();

  return () => {
    cleanupVPN();
    cleanupAuth();
    cleanupLayout();
  };
}
