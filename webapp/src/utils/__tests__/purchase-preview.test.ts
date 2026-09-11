import { describe, it, expect } from 'vitest';
import { previewOrderEnabled } from '../purchase-preview';

// 8 种组合逐个钉住：预览请求只在"结果真有人消费"的那一种形态下发出。
describe('previewOrderEnabled', () => {
  const cases: Array<[{ iap: boolean; wordgatePurchase: boolean; stripeCheckout: boolean }, boolean, string]> = [
    [{ iap: false, wordgatePurchase: true, stripeCheckout: false }, true, 'kaitu web/desktop：WordGate 多套餐流，唯一消费者'],
    [{ iap: true, wordgatePurchase: true, stripeCheckout: false }, false, 'kaitu iOS：整页换成 StoreKit 面板'],
    [{ iap: false, wordgatePurchase: false, stripeCheckout: true }, false, 'overleap web/desktop：整页换成 Stripe 面板'],
    [{ iap: true, wordgatePurchase: false, stripeCheckout: true }, false, 'overleap iOS：StoreKit 面板'],
    [{ iap: false, wordgatePurchase: false, stripeCheckout: false }, false, '无渠道兜底：只给一个去网站的按钮'],
    [{ iap: true, wordgatePurchase: false, stripeCheckout: false }, false, 'iap 优先级最高'],
    [{ iap: false, wordgatePurchase: true, stripeCheckout: true }, false, '两个渠道都开时 Stripe 面板先 return，WordGate UI 到不了'],
    [{ iap: true, wordgatePurchase: true, stripeCheckout: true }, false, 'iOS 下其余都不作数'],
  ];

  for (const [opts, expected, why] of cases) {
    it(`${JSON.stringify(opts)} → ${expected}（${why}）`, () => {
      expect(previewOrderEnabled(opts)).toBe(expected);
    });
  }
});
