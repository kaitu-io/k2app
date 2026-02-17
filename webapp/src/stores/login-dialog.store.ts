/**
 * Login Dialog Store - 登录弹窗状态管理
 *
 * 职责：
 * - 控制登录弹窗的显示/隐藏
 * - 记录触发来源和跳转路径
 * - 提供说明文案
 */

import { create } from 'zustand';

interface LoginDialogState {
  // 状态
  isOpen: boolean;
  trigger: string;           // 触发来源
  redirectPath?: string;     // 登录后跳转路径
  message?: string;          // 显示的说明文案

  // Actions
  open: (options: {
    trigger: string;
    redirectPath?: string;
    message?: string;
  }) => void;
  close: () => void;
}

export const useLoginDialogStore = create<LoginDialogState>((set) => ({
  // 初始状态
  isOpen: false,
  trigger: '',
  redirectPath: undefined,
  message: undefined,

  // Actions
  open: ({ trigger, redirectPath, message }) =>
    set({
      isOpen: true,
      trigger,
      redirectPath,
      message,
    }),

  close: () =>
    set({
      isOpen: false,
      trigger: '',
      redirectPath: undefined,
      message: undefined,
    }),
}));

// ============ 便捷 Hooks ============

/**
 * 获取登录弹窗状态和操作
 */
export function useLoginDialog() {
  return {
    isOpen: useLoginDialogStore((s) => s.isOpen),
    trigger: useLoginDialogStore((s) => s.trigger),
    redirectPath: useLoginDialogStore((s) => s.redirectPath),
    message: useLoginDialogStore((s) => s.message),
    open: useLoginDialogStore((s) => s.open),
    close: useLoginDialogStore((s) => s.close),
  };
}
