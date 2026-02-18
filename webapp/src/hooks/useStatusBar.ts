/**
 * useStatusBar Hook
 * 在移动端根据主题自动调整状态栏样式
 *
 * Capacitor Status Bar 样式说明:
 * - Style.Dark ('dark'): 浅色图标/文字，用于深色背景
 * - Style.Light ('light'): 深色图标/文字，用于浅色背景
 */

import { useEffect } from 'react';

interface StatusBarConfig {
  isDark: boolean;
}

// 主题颜色常量
const THEME_COLORS = {
  dark: '#0F0F13',  // darkTheme.background.default
  light: '#FFFFFF', // lightTheme.background.paper
};

/**
 * 更新 HTML meta theme-color 标签
 * 用于控制系统 UI 颜色（如 Android 状态栏、iOS Safari 地址栏等）
 */
const updateThemeColorMeta = (color: string) => {
  let metaThemeColor = document.querySelector('meta[name="theme-color"]');

  if (metaThemeColor) {
    metaThemeColor.setAttribute('content', color);
  } else {
    metaThemeColor = document.createElement('meta');
    metaThemeColor.setAttribute('name', 'theme-color');
    metaThemeColor.setAttribute('content', color);
    document.head.appendChild(metaThemeColor);
  }
};

/**
 * 根据主题配置状态栏
 * @param isDark 是否为深色主题
 *
 * 注意样式映射:
 * - 深色主题 (isDark=true) → 'dark' 样式 → 浅色图标（适合深色背景）
 * - 浅色主题 (isDark=false) → 'light' 样式 → 深色图标（适合浅色背景）
 */
export const useStatusBar = ({ isDark }: StatusBarConfig) => {

  useEffect(() => {
    const updateStatusBar = async () => {
      // 只在移动平台上设置状态栏
      if (!['ios', 'android'].includes(window._platform!.os)) {
        return;
      }

      const themeColor = isDark ? THEME_COLORS.dark : THEME_COLORS.light;
      // Capacitor 样式: 'dark' = 浅色图标(深色背景用), 'light' = 深色图标(浅色背景用)
      const statusBarStyle = isDark ? 'dark' : 'light';

      try {
        // 设置状态栏样式
        await (window._platform! as any).setStatusBarStyle?.(statusBarStyle);

        // Android: 设置状态栏背景色（透明，让内容延伸）
        await (window._platform! as any).setStatusBarColor?.(themeColor);

        console.debug(`[useStatusBar] Theme applied: ${isDark ? 'dark' : 'light'}, statusBarStyle: ${statusBarStyle}`);

        // 更新 HTML theme-color meta 标签
        updateThemeColorMeta(themeColor);
      } catch (error) {
        console.warn(`[useStatusBar] Failed to update status bar: ${error}`);
      }
    };

    updateStatusBar();
  }, [isDark]);
};
