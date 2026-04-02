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

// ============ Connection Store ============
export { useConnectionStore, initializeConnectionStore } from './connection.store';

// ============ Self-Hosted Store ============
export { useSelfHostedStore } from './self-hosted.store';

// ============ Feedback Store ============
export { useFeedbackStore } from './feedback.store';

// ============ VPN Machine Store ============
export {
  useVPNMachineStore,
  useVPNMachine,
  dispatch as vpnMachineDispatch,
  initializeVPNMachine,
} from './vpn-machine.store';

// 内部导入（用于 initializeAllStores）
import { initializeAuthStore, useAuthStore } from './auth.store';
import { initializeVPNMachine, useVPNMachineStore } from './vpn-machine.store';
import { initializeLayoutStore } from './layout.store';
import { useConfigStore } from './config.store';
import { initializeConnectionStore, useConnectionStore } from './connection.store';
import { useSelfHostedStore } from './self-hosted.store';
import { useFeedbackStore } from './feedback.store';

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
  useSelfHostedStore.getState().loadTunnel(); // fire-and-forget, sets loaded=true when done
  const cleanupAuth = initializeAuthStore();
  const cleanupVPNMachine = initializeVPNMachine();
  const cleanupConnection = initializeConnectionStore();

  // Subscribe to auth state for feedback polling
  const unsubFeedback = useAuthStore.subscribe(
    (s) => s.isAuthenticated,
    (isAuthenticated) => {
      if (isAuthenticated) {
        useFeedbackStore.getState().startPolling();
      } else {
        useFeedbackStore.getState().stopPolling();
      }
    },
    { fireImmediately: true }
  );

  // Subscribe to VPN state changes for analytics
  import('../services/stats').then(({ statsService }) => {
    let connectTime: number | null = null;

    // Snapshot tunnel info at connect time (before connectedTunnel gets cleared on disconnect)
    let lastConnectedSource: 'cloud' | 'self_hosted' = 'cloud';
    let lastNodeIpv4 = '';
    let lastNodeRegion = '';
    let lastRuleMode = '';

    const unsubStats = useVPNMachineStore.subscribe(
      (s) => s.state,
      (state, prevState) => {
        if (state === 'connected' && prevState !== 'connected') {
          connectTime = Date.now();

          // Read tunnel metadata from connection store
          const connState = useConnectionStore.getState();
          const configState = useConfigStore.getState();
          lastConnectedSource = connState.selectedSource;
          lastNodeIpv4 = connState.selectedCloudTunnel?.node?.ipv4 || '';
          lastNodeRegion = connState.selectedCloudTunnel?.node?.country || '';
          lastRuleMode = configState.ruleMode;

          statsService.trackConnect({
            nodeType: lastConnectedSource === 'self_hosted' ? 'self-hosted' : 'cloud',
            nodeIpv4: lastConnectedSource === 'cloud' ? lastNodeIpv4 : '',
            nodeRegion: lastConnectedSource === 'cloud' ? lastNodeRegion : '',
            ruleMode: lastRuleMode,
          });
        }

        // Track disconnect when connection session ends (any path to idle).
        // connectTime is the session-active flag: set on entering connected, null otherwise.
        // This covers all paths: user disconnect (disconnecting→idle), daemon crash (connected→idle),
        // error (reconnecting→idle), serviceDown recovery (serviceDown→idle).
        // Excludes paths that never reached connected (connecting→idle, idle→idle self-transition).
        if (state === 'idle' && connectTime !== null) {
          const durationSec = Math.floor((Date.now() - connectTime) / 1000);
          const errorInfo = useVPNMachineStore.getState().error;
          statsService.trackDisconnect({
            nodeType: lastConnectedSource === 'self_hosted' ? 'self-hosted' : 'cloud',
            nodeIpv4: lastConnectedSource === 'cloud' ? lastNodeIpv4 : '',
            nodeRegion: lastConnectedSource === 'cloud' ? lastNodeRegion : '',
            ruleMode: lastRuleMode,
            durationSec,
            reason: errorInfo ? 'error' : 'user',
          });
          connectTime = null;
        }
      }
    );

    (window as any).__statsUnsub = unsubStats;
  }).catch(() => {});

  return () => {
    unsubFeedback();
    useFeedbackStore.getState().stopPolling();
    cleanupConnection();
    cleanupVPNMachine();
    cleanupAuth();
    cleanupLayout();
  };
}
