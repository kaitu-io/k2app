import type { BrandThemeTokens } from '../types';

/**
 * Terminal Dark —— 与官网 kaitu.io 对齐。
 *
 * 取值来自**生产环境 computed style 实测**（不是照抄 globals.css，
 * 避免源码与线上漂移）：
 *   --background #0a0a0f   --foreground #e0e0e0   --card #111118
 *   --primary #00ff88      --primary-foreground #0a0a0f
 *   --secondary #00d4ff    --muted-foreground #9ca3af
 *   --border rgba(0,255,136,.15)   --radius .625rem(10px)
 *   --success #47d17a  --warning #ebc247  --destructive #df3a3a
 *
 * 官网把 primary 绿同时用于 CTA 与「已连接」（首页 .text-primary 实测
 * rgb(0,255,136)），app 沿用同一分配 —— 绿 = 通了 / 可行动。
 *
 * app 比官网多一个官网没有的控件：节点列表的选中 Radio。它与「已连接」
 * 的区分不靠色相，靠量级与位置（20px 圆点 vs 220px 发光大圆，分处两个
 * 区域）—— 与官网首页同屏并存绿徽章、绿按钮、绿「已连接」是同一手法。
 */
export const KAITU_THEME: BrandThemeTokens = {
  // 与 theme.ts 迁移前的 fontFamily 数组拼接结果逐字相同（零视觉变化）
  typography: {
    fontFamily: '-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif',
  },
  // light 保持迁移前取值：dark 是唯一实际渲染的主题（ThemeContext 硬编码），
  // light 仅为将来重启切换器而保留在包内。
  light: {
    primary: { main: '#1565C0', light: '#42A5F5', dark: '#0D47A1' },
    secondary: { main: '#00838F', light: '#26C6DA', dark: '#006064' },
  },
  dark: {
    primary: { main: '#00ff88', light: '#5affb3', dark: '#00cc6a' },
    secondary: { main: '#00d4ff', light: '#5ce3ff', dark: '#00a8cc' },
  },
  surface: {
    background: '#0a0a0f',
    paper: '#111118',
    border: 'rgba(0, 255, 136, 0.15)',
    textPrimary: '#e0e0e0',
    textSecondary: '#9ca3af',
    radius: 10,
  },
  semantic: {
    success: { main: '#47d17a', light: '#6fdd96', dark: '#2fa85c' },
    warning: { main: '#ebc247', light: '#f2d275', dark: '#c99f2c' },
    error: { main: '#df3a3a', light: '#e86a6a', dark: '#b62a2a' },
  },
  status: {
    // 已连接 = 品牌绿，与官网「已连接」同色（官网实测 .text-primary）
    connected: {
      main: '#00ff88',
      gradient: 'linear-gradient(135deg, #00ff88 0%, #00cc6a 100%)',
      glow: 'rgba(0, 255, 136, 0.35)',
      glowStrong: 'rgba(0, 255, 136, 0.5)',
    },
    // 未连接 = 青（官网 --secondary）。官网没有这个状态，此处是 app 专有：
    // 连上前后必须有色相变化，否则「通了」这件事没有视觉反馈。
    idle: {
      main: '#00d4ff',
      gradient: 'linear-gradient(135deg, #00d4ff 0%, #00a8cc 100%)',
      glow: 'rgba(0, 212, 255, 0.3)',
      glowStrong: 'rgba(0, 212, 255, 0.5)',
    },
    dormant: {
      border: 'rgba(255, 255, 255, 0.10)',
      icon: 'rgba(255, 255, 255, 0.30)',
    },
  },
};
