/**
 * 购买页跳转到支付的唯一入口。
 *
 * 为什么不直接用后端返回的 `payUrl`（Stripe Checkout URL）：
 *
 * 1. **可观测**。跳我们自己的 `/api/orders/:uuid/pay` 意味着"用户真的点了去支付"
 *    会在 Center 的 app.log 里留下 `[PayFunnel]` 一行。此前"下单"与"支付成功"之间
 *    是全黑的。客户端埋点解决不了这件事 —— GA / googletagmanager 在大陆被墙，
 *    恰好丢掉我们唯一关心的那批用户。
 * 2. **耐久**。Stripe Checkout Session 24h 过期、NextPay 订单 30 分钟后拒绝 confirm。
 *    端点会按需复用或重建 session；页面里存着的那个 URL 不会。
 * 3. **可重试**。同一个链接可以点第二次，这是"支付页没打开"的唯一自救手段
 *    （2026-09-23 实测：checkout.stripe.com 从贵州电信只有 56% 可达，
 *    而 hooks.stripe.com / js.stripe.com 分别是 100% / 84%）。
 *
 * `src` 用来把来源分开数，其中 `retry` 的占比就是"Stripe 页打不开"的直接代理指标。
 */
export type PaySource = 'web' | 'retry';

export function payLink(orderUuid: string, src: PaySource): string {
  return `/api/orders/${encodeURIComponent(orderUuid)}/pay?src=${src}`;
}

/**
 * 在新标签页打开支付页，失败时退回同页跳转。
 *
 * `preopened` 是**必须在点击的同一个 tick 里**用 `window.open('', '_blank')` 占好的窗口：
 * 下单要走一次 await，await 之后再 open 已经脱离用户手势，弹窗拦截器一定拦。
 * 拿不到窗口（被拦 / webview 不支持）就退回 `location.href` —— 那正是改动前的行为，
 * 所以最坏情况不会比以前差。
 *
 * 导航前把 `opener` 断开：否则被打开的页面能通过 `window.opener.location` 改写我们这一页
 * （reverse tabnabbing）。只有在它还停在 about:blank（同源）时才改得动，所以顺序不能颠倒。
 */
export function openPayLink(url: string, preopened?: Window | null): void {
  const win = preopened ?? window.open('', '_blank');
  if (win && !win.closed) {
    try {
      win.opener = null;
    } catch {
      // 某些 webview 不让写；断不开 opener 不值得为此放弃新标签页。
    }
    win.location.replace(url);
    return;
  }
  window.location.href = url;
}
