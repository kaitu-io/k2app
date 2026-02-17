/**
 * Alert Store - Toast 通知管理
 *
 * 职责：
 * - 管理全局 Toast/Snackbar 通知
 * - 支持多种严重程度（success, error, warning, info）
 * - 自动隐藏和手动关闭
 *
 * 使用：
 * ```tsx
 * // 在组件中
 * const { showAlert } = useAlertStore();
 * showAlert('操作成功', 'success');
 *
 * // 在非 React 环境
 * alertStore.getState().showAlert('错误消息', 'error');
 * ```
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';

type AlertColor = 'success' | 'error' | 'warning' | 'info';

interface AlertState {
  // 状态
  open: boolean;
  message: string;
  severity: AlertColor;
  duration: number;

  // Actions
  showAlert: (message: string, severity?: AlertColor, duration?: number) => void;
  hideAlert: () => void;
}

export const useAlertStore = create<AlertState>()(
  subscribeWithSelector((set) => ({
    // 初始状态
    open: false,
    message: '',
    severity: 'info',
    duration: 4000,

    // Actions
    showAlert: (message, severity = 'info', duration = 4000) => {
      set({
        open: true,
        message,
        severity,
        duration,
      });
    },

    hideAlert: () => {
      set({ open: false });
    },
  }))
);

// ============ 便捷 Hooks ============

/**
 * 获取 showAlert 函数
 */
export function useAlert() {
  return {
    showAlert: useAlertStore((s) => s.showAlert),
  };
}

/**
 * 获取完整的 Alert 状态（用于渲染 Snackbar）
 */
export function useAlertState() {
  return {
    open: useAlertStore((s) => s.open),
    message: useAlertStore((s) => s.message),
    severity: useAlertStore((s) => s.severity),
    duration: useAlertStore((s) => s.duration),
    hideAlert: useAlertStore((s) => s.hideAlert),
  };
}
