import type { BrandThemeTokens } from '../types';

/**
 * Terminal Dark —— 与官网 web/src/app/globals.css 同源的调性。
 *
 * 色彩语义契约（每个颜色只有一个 job）：
 *   霓虹绿 #00ff88  通了 / 受保护   → status.connected、品牌 accent
 *   青     #00d4ff  可操作 / 你在哪 → palette.primary（导航/Tab/Radio/按钮/focus）
 *   柔绿   #66BB6A  健康度可视化    → palette.success（RecommendBar，见 theme.ts）
 *
 * 刻意不照搬官网的 `--primary: #00ff88`：官网是内容站，没有「连接状态」这个
 * 维度；app 有。primary 设为绿会让「已选中某节点」与「已连接」同色，六处控件
 * 同屏泛绿。官网自身也未合并 —— 其 --primary 与 --success 是两个独立 token。
 * 详见 docs/superpowers/specs/2026-08-18-kaitu-terminal-dark-theme-design.md
 */
export const KAITU_THEME: BrandThemeTokens = {
  // light 保持迁移前取值：dark 模式是唯一实际渲染的主题（ThemeContext 硬编码），
  // light 仅为将来重新启用切换器而保留在包内。
  light: {
    primary: { main: '#1565C0', light: '#42A5F5', dark: '#0D47A1' },
    secondary: { main: '#00838F', light: '#26C6DA', dark: '#006064' },
  },
  dark: {
    primary: { main: '#00d4ff', light: '#5ce3ff', dark: '#00a8cc' },
    // 品牌绿的语义入口。当前 `color="secondary"` 无消费者，此处不渲染任何
    // 现有元素；将来需要品牌绿点缀时从这里取，不要散落硬编码。
    secondary: { main: '#00ff88', light: '#5affb3', dark: '#00cc6a' },
  },
  surface: {
    background: '#0a0a0f', // 官网 --background
    paper: '#111118',      // 官网 --card
    // 刻意不用官网的 rgba(0,255,136,.15)：边框带绿会让绿沦为背景噪音，
    // 削弱「已连接」的信号强度。中性灰承重，绿只在主按钮出现。
    border: 'rgba(255, 255, 255, 0.12)',
  },
  status: {
    connected: {
      main: '#00ff88',
      gradient: 'linear-gradient(135deg, #00ff88 0%, #00cc6a 100%)',
      glow: 'rgba(0, 255, 136, 0.35)',
      glowStrong: 'rgba(0, 255, 136, 0.5)',
    },
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
