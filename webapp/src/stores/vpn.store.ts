/**
 * VPN Store - VPN 状态管理
 *
 * 职责：
 * - 管理 VPN 运行状态（status, serviceState）
 * - 支持乐观更新（Optimistic Update）以提供即时 UI 反馈
 * - 管理连接错误信息（通过 status.error）
 * - 提供便捷的状态布尔值
 *
 * 使用：
 * ```tsx
 * const { isConnected, setOptimisticState } = useVPNStatus();
 *
 * // 乐观更新示例
 * setOptimisticState('connecting'); // UI 立即响应
 * await window._k2.run('start'); // 后端事件到达后自动覆盖
 * ```
 */

import { useState, useEffect, useMemo } from 'react';
import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';

import type { StatusResponseData } from '../services/control-types';

// ============ Types ============

type ServiceState =
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnecting'
  | 'error';

interface VPNState {
  status: StatusResponseData | null;
  localState: ServiceState | null;  // 乐观更新状态
  serviceConnected: boolean;        // Service 进程是否可达
  serviceFailedSince: number | null; // 连接失败开始时间
}

interface VPNActions {
  setStatus: (status: StatusResponseData | null) => void;
  setOptimisticState: (state: ServiceState | null) => void;
  setServiceFailed: (failed: boolean) => void;
}

// ============ Constants ============

const OPTIMISTIC_TIMEOUT_MS = 5000;
const STATE_DEBOUNCE_MS = 3000;
const POLL_INTERVAL_MS = 2000;
const SERVICE_FAILURE_THRESHOLD_MS = 10000;

// ============ Helpers ============

/** 验证乐观状态到后端状态的转换是否合法 */
const isValidTransition = (from: ServiceState, to: ServiceState): boolean => {
  const valid: Record<ServiceState, ServiceState[]> = {
    connecting: ['connecting', 'connected', 'error', 'reconnecting'],
    disconnecting: ['disconnecting', 'disconnected', 'error'],
    reconnecting: ['reconnecting', 'connecting', 'connected', 'error'],
    disconnected: [],
    connected: [],
    error: [],
  };
  return valid[from]?.includes(to) ?? false;
};

/** 判断是否需要防抖 */
const shouldDebounce = (current: ServiceState, next: ServiceState): boolean => {
  if (next === 'connected' || next === 'disconnected') return false;
  return current === 'connected' && (next === 'connecting' || next === 'reconnecting');
};

// ============ Store ============

// 模块级定时器（避免闭包问题）
let optimisticTimer: NodeJS.Timeout | null = null;
let debounceTimer: NodeJS.Timeout | null = null;
let pendingState: ServiceState | null = null;

export const useVPNStore = create<VPNState & VPNActions>()(
  subscribeWithSelector((set, get) => ({
    // State
    status: null,
    localState: null,
    serviceConnected: true,
    serviceFailedSince: null,

    // Actions
    setStatus: (status) => set({ status }),

    setOptimisticState: (state) => {
      if (optimisticTimer) {
        clearTimeout(optimisticTimer);
        optimisticTimer = null;
      }

      if (!state) {
        set({ localState: null });
        return;
      }

      set({ localState: state });

      // 5秒超时保护
      optimisticTimer = setTimeout(() => {
        console.warn('[VPNStore] 乐观状态超时，清除');
        set({ localState: null });
        optimisticTimer = null;
      }, OPTIMISTIC_TIMEOUT_MS);
    },

    setServiceFailed: (failed) => {
      const { serviceConnected, serviceFailedSince } = get();
      if (failed && serviceConnected) {
        set({ serviceConnected: false, serviceFailedSince: Date.now() });
      } else if (!failed && (!serviceConnected || serviceFailedSince)) {
        set({ serviceConnected: true, serviceFailedSince: null });
      }
    },
  }))
);

// ============ 派生状态计算 ============

function computeDerivedState(state: VPNState) {
  const serviceState = state.localState || (state.status?.state as ServiceState) || 'disconnected';
  const error = state.status?.error ?? null;
  const failureDuration = state.serviceFailedSince ? Date.now() - state.serviceFailedSince : null;

  // isRetrying: error state - whether K2 layer is retrying
  // - Network errors (570/571): true, backend retries every 5 seconds
  // - Auth errors (401/402): false, requires user action
  const isRetrying = serviceState === 'error' && (state.status?.retrying ?? false);

  // networkAvailable: whether network is available for VPN connection
  // Used to show different messages during error retry (network down vs server unreachable)
  const networkAvailable = state.status?.networkAvailable ?? true;

  return {
    serviceState,
    error,
    isConnected: serviceState === 'connected',
    isDisconnected: serviceState === 'disconnected',
    isConnecting: serviceState === 'connecting',
    isReconnecting: serviceState === 'reconnecting',
    isDisconnecting: serviceState === 'disconnecting',
    isError: serviceState === 'error',
    isRetrying, // error state - whether K2 is retrying
    networkAvailable, // network availability during error retry
    isTransitioning: ['connecting', 'reconnecting', 'disconnecting'].includes(serviceState),
    isServiceRunning: ['connected', 'connecting', 'reconnecting', 'error'].includes(serviceState),
    serviceConnected: state.serviceConnected,
    serviceFailureDuration: failureDuration,
    isServiceFailedLongTime: failureDuration !== null && failureDuration >= SERVICE_FAILURE_THRESHOLD_MS,
  };
}

// ============ Initialize ============

export function initializeVPNStore(): () => void {
  console.info('[VPNStore] 启动状态轮询');

  const handleStatusChange = (newStatus: StatusResponseData) => {
    const { localState, status: currentStatus } = useVPNStore.getState();
    const currentState = (currentStatus?.state as ServiceState) || 'disconnected';
    const backendState = newStatus.state as ServiceState;

    // 注意：认证错误处理已移至 k2api 统一处理
    // VPN 状态中的 error 不再触发认证流程

    // ============ Service 版本检测 ============
    // 检测 app 版本与 service 版本是否匹配（macOS 更新后 service 可能未重启）
    // REMOVED: All version check logic moved to Rust (src-tauri/src/main.rs)
    // Tauri now handles version checking on startup via ensure_service_running()
    // which calls admin_reinstall_service ('svc up') if needed

    // 防抖逻辑
    if (shouldDebounce(currentState, backendState)) {
      if (debounceTimer) clearTimeout(debounceTimer);
      pendingState = backendState;

      debounceTimer = setTimeout(() => {
        if (pendingState) {
          useVPNStore.setState({ status: newStatus });
        }
        pendingState = null;
        debounceTimer = null;
      }, STATE_DEBOUNCE_MS);
      return;
    }

    // 取消防抖
    if (debounceTimer) {
      clearTimeout(debounceTimer);
      debounceTimer = null;
      pendingState = null;
    }

    // 更新状态
    useVPNStore.setState({ status: newStatus });

    // 清除合法的乐观状态
    if (localState && isValidTransition(localState, backendState)) {
      if (optimisticTimer) {
        clearTimeout(optimisticTimer);
        optimisticTimer = null;
      }
      useVPNStore.setState({ localState: null });
    }
  };

  const pollStatus = async () => {
    try {
      const response = await window._k2.run('status') as {
        code: number;
        data?: StatusResponseData;
        message?: string;
      };

      if (response.code === 0 && response.data) {
        handleStatusChange(response.data);
        useVPNStore.getState().setServiceFailed(false);
      } else {
        console.warn('[VPNStore] 服务返回错误:', response.code, response.message);
        useVPNStore.getState().setServiceFailed(true);
      }
    } catch (error) {
      console.error('[VPNStore] 轮询异常:', error);
      useVPNStore.getState().setServiceFailed(true);
    }
  };

  pollStatus();
  const interval = setInterval(pollStatus, POLL_INTERVAL_MS);

  return () => {
    clearInterval(interval);
    [optimisticTimer, debounceTimer].forEach(t => t && clearTimeout(t));
    optimisticTimer = debounceTimer = null;
    pendingState = null;
    useVPNStore.setState({ serviceConnected: true, serviceFailedSince: null });
  };
}

// ============ Hooks ============

/**
 * 获取 VPN 状态的便捷 Hook
 */
export function useVPNStatus() {
  // 订阅核心状态
  const status = useVPNStore((s) => s.status);
  const localState = useVPNStore((s) => s.localState);
  const serviceConnected = useVPNStore((s) => s.serviceConnected);
  const serviceFailedSince = useVPNStore((s) => s.serviceFailedSince);
  const setOptimisticState = useVPNStore((s) => s.setOptimisticState);

  // 定时器：用于更新 isServiceFailedLongTime
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!serviceFailedSince) return;
    const timer = setInterval(() => setTick(n => n + 1), 1000);
    return () => clearInterval(timer);
  }, [serviceFailedSince]);

  // 计算派生状态
  const derived = useMemo(
    () => computeDerivedState({ status, localState, serviceConnected, serviceFailedSince }),
    [status, localState, serviceConnected, serviceFailedSince, tick]
  );

  return { ...derived, setOptimisticState };
}
