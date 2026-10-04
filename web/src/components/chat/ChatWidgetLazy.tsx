'use client';

import { Component, useEffect, useState, type ReactNode } from 'react';
import dynamic from 'next/dynamic';
import { siteBrand } from '@/lib/brands';
import { isEmbeddedPage } from '@/lib/funnel';
import { probeChatEnabled, shouldStartSession } from './gate';

// 挂件本体 + 客户端库单独成块、只在浏览器加载：对 SSR 输出与首屏体积零影响。
const ChatWidget = dynamic(() => import('./ChatWidget'), { ssr: false });

/**
 * 挂件自己的错误边界：渲染出错或代码块加载失败时整块变空。
 * 聊天坏了绝不能把定价 / 结账页带到全局错误页。吞掉不等于不知道：出错时上报一次
 *（边界进入 failed 后不再渲染子树，所以每次挂载至多一条）。
 */
export class ChatErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(error: unknown) {
    // 监控 SDK 按需引入：它在浏览器里早已由 instrumentation-client 加载，这里不再让每个
    // 挂了本组件的页面模块（含服务端渲染与单测）静态背上整个 SDK。上报失败不再抛。
    import('@sentry/nextjs')
      .then((Sentry) => Sentry.captureException(error, { tags: { component: 'chat-widget' } }))
      .catch(() => {});
  }
  render() {
    return this.state.failed ? null : this.props.children;
  }
}

/**
 * 全站挂载点（根布局里放一个）。先用便宜的判断决定要不要加载挂件，品牌没开或 App 内嵌页连探测都不发：
 * - 预览身份 / 继续对话令牌 / 建过会话的浏览器：直接加载，挂件立刻建会话；
 * - 其余访客：先探测会话开没开，开着才加载，挂件只画入口，访客展开面板时才建会话。
 * 判断放在 effect 里——服务端与首帧恒为空，不产生水合差异。
 */
export default function ChatWidgetLazy() {
  const [mode, setMode] = useState<'start' | 'deferred' | null>(null);
  useEffect(() => {
    const brand = siteBrand();
    if (!brand.chatEnabled || isEmbeddedPage()) return;
    if (shouldStartSession()) {
      setMode('start');
      return;
    }
    let cancelled = false;
    void probeChatEnabled(brand.id).then((enabled) => {
      if (!cancelled && enabled) setMode('deferred');
    });
    return () => {
      cancelled = true;
    };
  }, []);
  if (!mode) return null;
  return (
    <ChatErrorBoundary>
      <ChatWidget deferStart={mode === 'deferred'} />
    </ChatErrorBoundary>
  );
}
