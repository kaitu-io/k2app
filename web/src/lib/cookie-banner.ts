/**
 * Cookie 同意横幅占着视口右下角（手机上是整条底边），层级最高。它显示期间在 <html> 上公布
 * 这个 CSS 变量 = 横幅顶边到视口底边的距离（px）；不显示时变量不存在。
 *
 * 同一个角上的其他浮层（会话挂件的入口与面板）用 `var(--cookie-banner-offset,0px)` 把自己的
 * 底边垫高，既不被横幅盖住，也不去盖横幅的按钮。Tailwind 只认源码里的字面量类名，所以使用方
 * 的类名里是写死的变量名——测试锁住它与这里一致。
 */
export const COOKIE_BANNER_OFFSET_VAR = '--cookie-banner-offset';
