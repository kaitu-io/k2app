import type { BrandThemeTokens } from '../types';

/**
 * Theme palette: working values pending final design sign-off (see plan's
 * open questions) — distinct violet/teal family so a mis-branded build is
 * obvious.
 *
 * `surface` / `status` 是 2026-08-18 品牌 token 契约扩展时补齐的 —— 取值是
 * 扩展前实际生效的共享默认值的**等价拷贝**，因此本品牌视觉零变化：
 *   surface  ← theme.ts darkTheme.palette.background / divider
 *   status   ← theme/colors.ts APP_COLORS.dark 的 success* / info* 键
 * 改动这些值 = 改动 Overleap 的外观，不是"顺手统一"，请单独决策。
 */
export const OVERLEAP_THEME: BrandThemeTokens = {
  light: {
    primary: { main: '#5E35B1', light: '#7E57C2', dark: '#4527A0' },
    secondary: { main: '#00897B', light: '#26A69A', dark: '#00695C' },
  },
  dark: {
    primary: { main: '#9575CD', light: '#B39DDB', dark: '#673AB7' },
    secondary: { main: '#4DB6AC', light: '#80CBC4', dark: '#26A69A' },
  },
  surface: {
    background: '#0F0F13',
    paper: '#1A1A1D',
    border: 'rgba(255, 255, 255, 0.12)',
  },
  status: {
    // 已连接 = 柔绿（原 APP_COLORS.dark.success*）
    connected: {
      main: '#66bb6a',
      gradient: 'linear-gradient(135deg, #66bb6a 0%, #4caf50 100%)',
      glow: 'rgba(102, 187, 106, 0.3)',
      glowStrong: 'rgba(102, 187, 106, 0.5)',
    },
    // 待命 = 蓝（原 APP_COLORS.dark.info*）—— 与品牌紫无关，历史即如此
    idle: {
      main: '#42a5f5',
      gradient: 'linear-gradient(135deg, #42a5f5 0%, #2196f3 100%)',
      glow: 'rgba(66, 165, 245, 0.3)',
      glowStrong: 'rgba(66, 165, 245, 0.5)',
    },
    // 熄灭态为中性色，品牌无关
    dormant: {
      border: 'rgba(255, 255, 255, 0.10)',
      icon: 'rgba(255, 255, 255, 0.30)',
    },
  },
};
