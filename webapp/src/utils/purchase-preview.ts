/**
 * 「要不要预下单（preview）」的唯一判据。
 *
 * 预览结果只有一个消费者：WordGate 多套餐流的订单摘要（Purchase 里的 orderData）。
 * 其余形态都把整页替换掉了，拿不到也用不上这个结果：
 *   - iOS StoreKit：IosSubscribePanel / IosMembershipPanel（Apple 3.1.1，单一自动续订商品）
 *   - Stripe 订阅品牌（overleap）：StripePurchasePanel
 *   - 无支付渠道的兜底：指向网站的提示页
 *
 * 为什么要单独一个函数：这些早退发生在 **hook 执行之后**，所以"页面根本不渲染
 * WordGate UI"并不能阻止预览 effect 发请求。Purchase.tsx 里原本写着
 * 「WordGate order/preview flow below never runs on iOS」——那句话是错的，iOS 一直在
 * 发预览请求，也正因为这句话，2026-09-11 的预览死循环事故里没人第一时间去看 iOS。
 * 把判据摘成纯函数，才能对着它逐个组合写断言。
 */
export function previewOrderEnabled(opts: {
  /** window._platform?.iap 是否存在（iOS StoreKit 轨）。 */
  iap: boolean;
  /** brandConfig.features.wordgatePurchase */
  wordgatePurchase: boolean;
  /** brandConfig.features.stripeCheckout */
  stripeCheckout: boolean;
}): boolean {
  if (opts.iap) return false;
  if (opts.stripeCheckout) return false;
  return opts.wordgatePurchase;
}
