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
    // 锚点取自 kaitu.io 生产环境 computed style 实测（非照抄 globals.css，
    // 避免源码与线上漂移）。app 与官网不一致时，这里就是对照表。
    it('与官网 kaitu.io 的 token 逐项对齐（改动必须同步更新本断言）', () => {
      expect(darkTheme.palette.primary.main).toBe('#00ff88');      // --primary
      expect(darkTheme.palette.secondary.main).toBe('#00d4ff');    // --secondary
      expect(darkTheme.palette.background.default).toBe('#0a0a0f'); // --background
      expect(darkTheme.palette.background.paper).toBe('#111118');   // --card
      expect(darkTheme.palette.divider).toBe('rgba(0, 255, 136, 0.15)'); // --border
      expect(darkTheme.palette.text.primary).toBe('#e0e0e0');       // --foreground
      expect(darkTheme.palette.text.secondary).toBe('#9ca3af');     // --muted-foreground
      expect(darkTheme.palette.success.main).toBe('#47d17a');       // --success
      expect(darkTheme.palette.warning.main).toBe('#ebc247');       // --warning
      expect(darkTheme.palette.error.main).toBe('#df3a3a');         // --destructive
      expect(darkTheme.shape.borderRadius).toBe(10);                // --radius .625rem
      // light 保持迁移前取值（dark-only，light 仅为将来重启切换器保留）
      expect(lightTheme.palette.primary.main).toBe('#1565C0');
    });

    it('已连接沿用官网的 primary 绿；未连接是 app 专有的青', () => {
      const status = KAITU_BRAND.theme.status;
      // 官网首页「已连接」实测为 .text-primary → rgb(0,255,136)，app 同源
      expect(status.connected.main).toBe('#00ff88');
      expect(status.connected.main).toBe(darkTheme.palette.primary.main);
      // 官网没有「未连接」这个状态。app 必须让连上前后有色相变化，
      // 否则「通了」没有视觉反馈 —— 用官网的 --secondary 青。
      expect(status.idle.main).toBe('#00d4ff');
      expect(status.idle.main).not.toBe(status.connected.main);
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

    it('Overleap 正式视觉 token（spec 2026-09-04 §1.1；改色必须同步改这里）', () => {
      expect(darkTheme.palette.primary.main).toBe('#7C5CFF');
      expect(darkTheme.palette.secondary.main).toBe('#2DD4BF');
      expect(darkTheme.palette.background.default).toBe('#0B0E14');
      expect(darkTheme.palette.background.paper).toBe('#141926');
      expect(darkTheme.palette.divider).toBe('rgba(124, 92, 255, 0.18)');
      expect(darkTheme.palette.text.primary).toBe('#E6E8F0');
      expect(darkTheme.palette.text.secondary).toBe('#9AA0B4');
      expect(darkTheme.palette.success.main).toBe('#34D399');
      expect(darkTheme.palette.warning.main).toBe('#FBBF24');
      expect(darkTheme.palette.error.main).toBe('#F87171');
      expect(darkTheme.shape.borderRadius).toBe(12);

      const status = OVERLEAP_BRAND.theme.status;
      // 已连接 = 薄荷青，待命 = 品牌紫：与开途「绿=通了 / 青=待命」错开
      expect(status.connected.main).toBe('#2DD4BF');
      expect(status.idle.main).toBe('#7C5CFF');
      expect(status.connected.main).not.toBe(KAITU_BRAND.theme.status.connected.main);
      expect(status.idle.main).not.toBe(KAITU_BRAND.theme.status.idle.main);
    });
  });

  it('typography.fontFamily comes from brand tokens and differs across brands', () => {
    expect(darkTheme.typography.fontFamily).toBe(brandConfig.theme.typography.fontFamily);
    expect(lightTheme.typography.fontFamily).toBe(brandConfig.theme.typography.fontFamily);
    expect(OVERLEAP_BRAND.theme.typography.fontFamily).not.toBe(KAITU_BRAND.theme.typography.fontFamily);
    expect(OVERLEAP_BRAND.theme.typography.fontFamily).toMatch(/^Inter,/);
  });

  it('the two brands never share a primary, a connected colour, or a radius', () => {
    expect(OVERLEAP_BRAND.theme.dark.primary.main).not.toBe(KAITU_BRAND.theme.dark.primary.main);
    expect(OVERLEAP_BRAND.theme.status.connected.main).not.toBe(KAITU_BRAND.theme.status.connected.main);
    expect(OVERLEAP_BRAND.theme.surface.radius).not.toBe(KAITU_BRAND.theme.surface.radius);
  });
});
