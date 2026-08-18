import { describe, it, expect } from 'vitest';
import { lightTheme, darkTheme } from './theme';
import { brandConfig } from './brands';
import { KAITU_BRAND } from './brands/kaitu';
import { OVERLEAP_BRAND } from './brands/overleap';

describe('MUI theme derives from brand tokens', () => {
  it('dark palette primary/secondary come from brandConfig.theme.dark', () => {
    expect(darkTheme.palette.primary.main).toBe(brandConfig.theme.dark.primary.main);
    expect(darkTheme.palette.primary.light).toBe(brandConfig.theme.dark.primary.light);
    expect(darkTheme.palette.primary.dark).toBe(brandConfig.theme.dark.primary.dark);
    expect(darkTheme.palette.secondary.main).toBe(brandConfig.theme.dark.secondary.main);
  });

  it('light palette primary/secondary come from brandConfig.theme.light', () => {
    expect(lightTheme.palette.primary.main).toBe(brandConfig.theme.light.primary.main);
    expect(lightTheme.palette.secondary.main).toBe(brandConfig.theme.light.secondary.main);
  });

  // describe.runIf, not an early `return`: a bare return makes the assertions
  // vanish under K2_BRAND=overleap while the test still reports green — a
  // hollow pass. Skipping is honest; the closed-gate case gets its own real
  // assertions below. (webapp/CLAUDE.md — brand-adaptive test rule.)
  // 下面两组锚点刻意写死字面量，不从 brandConfig 派生 —— 从生产值派生的断言
  // 恒为真，等于没有守卫。它们的作用是：任何人改动配色都必须同时改这里，
  // 于是「改了什么颜色」在 diff 里显式可见，无法顺手溜过 review。
  describe.runIf(brandConfig.id === 'kaitu')('kaitu', () => {
    it('Terminal Dark 色彩语义契约（改动必须同步更新本断言）', () => {
      // 青 = 可操作。刻意不是官网的 #00ff88：primary 会渗透到 Radio/Tab/
      // 导航/Badge/CircularProgress，设为绿会与「已连接」同色。
      // 见 docs/superpowers/specs/2026-08-18-kaitu-terminal-dark-theme-design.md §2
      expect(darkTheme.palette.primary.main).toBe('#00d4ff');
      // 绿 = 品牌 accent，仅供已连接与品牌点缀
      expect(darkTheme.palette.secondary.main).toBe('#00ff88');
      // 与官网 --background / --card 精确一致
      expect(darkTheme.palette.background.default).toBe('#0a0a0f');
      expect(darkTheme.palette.background.paper).toBe('#111118');
      // 边框有意偏离官网的 rgba(0,255,136,.15)：带绿边框会让绿沦为背景噪音
      expect(darkTheme.palette.divider).toBe('rgba(255, 255, 255, 0.12)');
      // success 保持柔绿，不换品牌绿：RecommendBar 每行都有，用 accent 会抢戏
      expect(darkTheme.palette.success.main).toBe('#66BB6A');
      // light 保持迁移前取值（dark-only，light 仅为将来重启切换器保留）
      expect(lightTheme.palette.primary.main).toBe('#1565C0');
    });

    it('连接状态色与交互色是两套 token，不可互相塌陷', () => {
      const status = KAITU_BRAND.theme.status;
      expect(status.connected.main).toBe('#00ff88');
      expect(status.idle.main).toBe('#00d4ff');
      // 这条是本次设计的核心不变量：已连接色 ≠ Radio/Tab/按钮的选中色。
      // 若有人把 primary 改回绿，此断言立刻红。
      expect(status.connected.main).not.toBe(darkTheme.palette.primary.main);
    });
  });

  describe.runIf(brandConfig.id === 'overleap')('overleap', () => {
    it('uses its own palette and never falls back to the kaitu blues', () => {
      expect(darkTheme.palette.primary.main).toBe(OVERLEAP_BRAND.theme.dark.primary.main);
      expect(lightTheme.palette.primary.main).toBe(OVERLEAP_BRAND.theme.light.primary.main);
      // Guards the failure mode this whole gate exists to prevent: a silent
      // fallback to the other brand's tokens would still satisfy the generic
      // "derives from brandConfig" assertions above.
      expect(darkTheme.palette.primary.main).not.toBe(KAITU_BRAND.theme.dark.primary.main);
      expect(lightTheme.palette.primary.main).not.toBe(KAITU_BRAND.theme.light.primary.main);
    });

    it('surface/status 仍是开途 Terminal Dark 迁移前的取值（零视觉回归）', () => {
      // 2026-08-18 品牌 token 契约扩展时，surface/status 从共享默认值搬进了
      // 各品牌文件。Overleap 填的是**等价拷贝**，本断言锁住这一点：改动
      // 这些值 = 改动 Overleap 外观，必须是显式决策而非顺手统一。
      expect(darkTheme.palette.background.default).toBe('#0F0F13');
      expect(darkTheme.palette.background.paper).toBe('#1A1A1D');
      expect(darkTheme.palette.divider).toBe('rgba(255, 255, 255, 0.12)');

      const status = OVERLEAP_BRAND.theme.status;
      // 迁移前 = APP_COLORS.dark.successGradient / infoGradient
      expect(status.connected.gradient).toBe('linear-gradient(135deg, #66bb6a 0%, #4caf50 100%)');
      expect(status.idle.gradient).toBe('linear-gradient(135deg, #42a5f5 0%, #2196f3 100%)');
      // 没有沾染开途的霓虹色
      expect(status.connected.main).not.toBe(KAITU_BRAND.theme.status.connected.main);
      expect(status.idle.main).not.toBe(KAITU_BRAND.theme.status.idle.main);
    });
  });
});
