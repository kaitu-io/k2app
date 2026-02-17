/**
 * Dashboard Store - Dashboard 页面状态持久化
 *
 * 职责：
 * - 持久化 Dashboard 页面状态（切换 Tab 后保持）
 * - 管理高级设置展开/折叠状态
 * - 管理滚动位置
 *
 * 使用：
 * ```tsx
 * const { advancedSettingsExpanded, toggleAdvancedSettings } = useDashboardStore();
 * ```
 */

import { create } from 'zustand';

interface DashboardState {
  // 高级设置展开状态
  advancedSettingsExpanded: boolean;
  // 滚动位置（用于恢复）
  scrollPosition: number;

  // Actions
  setAdvancedSettingsExpanded: (expanded: boolean) => void;
  toggleAdvancedSettings: () => void;
  setScrollPosition: (position: number) => void;
}

export const useDashboardStore = create<DashboardState>()((set, get) => ({
  // 初始状态
  advancedSettingsExpanded: false,
  scrollPosition: 0,

  // Actions
  setAdvancedSettingsExpanded: (expanded) =>
    set({ advancedSettingsExpanded: expanded }),

  toggleAdvancedSettings: () =>
    set({ advancedSettingsExpanded: !get().advancedSettingsExpanded }),

  setScrollPosition: (position) =>
    set({ scrollPosition: position }),
}));

// ============ 便捷 Hooks ============

/**
 * 获取 Dashboard 状态
 */
export function useDashboard() {
  return {
    advancedSettingsExpanded: useDashboardStore((s) => s.advancedSettingsExpanded),
    scrollPosition: useDashboardStore((s) => s.scrollPosition),
    setAdvancedSettingsExpanded: useDashboardStore((s) => s.setAdvancedSettingsExpanded),
    toggleAdvancedSettings: useDashboardStore((s) => s.toggleAdvancedSettings),
    setScrollPosition: useDashboardStore((s) => s.setScrollPosition),
  };
}
