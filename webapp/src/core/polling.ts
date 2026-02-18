/**
 * Status Polling
 *
 * 2s 轮询 VPN 状态
 */

import { useEffect, useRef, useCallback } from 'react';
import type { StatusResponseData } from '../services/vpn-types';

const POLL_INTERVAL = 2000; // 2s

export type StatusChangeHandler = (status: StatusResponseData) => void;
export type ConnectionChangeHandler = (connected: boolean, error?: string | null) => void;

interface PollingOptions {
  /** 轮询间隔（毫秒），默认 2000 */
  interval?: number;
  /** 状态变化回调 */
  onStatusChange?: StatusChangeHandler;
  /** 连接状态变化回调 */
  onConnectionChange?: ConnectionChangeHandler;
}

/**
 * 状态轮询 Hook
 *
 * 在 App 顶层调用一次即可
 *
 * @example
 * function App() {
 *   useStatusPolling({
 *     onStatusChange: (status) => vpnStore.updateStatus(status),
 *     onConnectionChange: (connected) => coreStore.setConnected(connected),
 *   });
 *   return <AppRoutes />;
 * }
 */
export function useStatusPolling(options: PollingOptions = {}) {
  const {
    interval = POLL_INTERVAL,
    onStatusChange,
    onConnectionChange,
  } = options;

  const timerRef = useRef<ReturnType<typeof setTimeout>>();
  const runningRef = useRef(false);
  const lastConnectedRef = useRef<boolean | null>(null);

  const poll = useCallback(async () => {
    if (!runningRef.current) return;

    if (!window._k2) {
      console.warn('[Polling] K2 not ready, skipping...');
      if (runningRef.current) {
        timerRef.current = setTimeout(poll, interval);
      }
      return;
    }

    try {
      const resp = await window._k2.run<StatusResponseData>('status');

      if (resp.code === 0 && resp.data) {
        // 连接成功
        if (lastConnectedRef.current !== true) {
          lastConnectedRef.current = true;
          onConnectionChange?.(true, null);
        }
        onStatusChange?.(resp.data);
      } else if (resp.code === -1) {
        // 连接失败
        if (lastConnectedRef.current !== false) {
          lastConnectedRef.current = false;
          onConnectionChange?.(false, resp.message || 'Connection failed');
        }
      }
    } catch (error) {
      // 异常
      if (lastConnectedRef.current !== false) {
        lastConnectedRef.current = false;
        onConnectionChange?.(false, error instanceof Error ? error.message : 'Unknown error');
      }
    }

    // 调度下一次轮询
    if (runningRef.current) {
      timerRef.current = setTimeout(poll, interval);
    }
  }, [interval, onStatusChange, onConnectionChange]);

  useEffect(() => {
    console.info('[Polling] Starting status polling...');
    runningRef.current = true;

    // 立即开始轮询
    poll();

    return () => {
      console.info('[Polling] Stopping status polling...');
      runningRef.current = false;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [poll]);
}

/**
 * 手动轮询一次（用于需要立即刷新状态的场景）
 */
export async function pollStatusOnce(): Promise<StatusResponseData | null> {
  if (!window._k2) {
    console.warn('[Polling] K2 not ready');
    return null;
  }

  try {
    const resp = await window._k2.run<StatusResponseData>('status');
    if (resp.code === 0 && resp.data) {
      return resp.data;
    }
  } catch (error) {
    console.error('[Polling] Poll once failed:', error);
  }

  return null;
}
