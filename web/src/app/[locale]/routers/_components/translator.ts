import type { getTranslations } from 'next-intl/server';

/**
 * 页面 await 一次 getTranslations('routers') 后传给各段组件——段组件保持同步，
 * 页面树里没有嵌套的 async 组件（jsdom 下的 SSR 守卫测试渲染不了嵌套 async 组件）。
 */
export type RoutersT = Awaited<ReturnType<typeof getTranslations<'routers'>>>;
