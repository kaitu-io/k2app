'use client';

import { Component, useEffect, useState, type ReactNode } from 'react';
import dynamic from 'next/dynamic';
import { siteBrand } from '@/lib/brands';
import { isEmbeddedPage } from '@/lib/funnel';
import { shouldProbeSession } from './gate';

// 挂件本体 + 客户端库单独成块、只在浏览器加载：对 SSR 输出与首屏体积零影响。
const ChatWidget = dynamic(() => import('./ChatWidget'), { ssr: false });

/**
 * 挂件自己的错误边界：渲染出错或代码块加载失败时整块变空。
 * 聊天坏了绝不能把定价 / 结账页带到全局错误页。
 */
export class ChatErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? null : this.props.children;
  }
}

/**
 * 页面上的挂载点（每页放一个）。先用便宜的本地判断决定要不要加载挂件：
 * 品牌没开、App 内嵌页、或暗发布阶段不该探测的访客，连代码块都不下载。
 * 判断放在 effect 里——服务端与首帧恒为空，不产生水合差异。挂件自己会再判一遍。
 */
export default function ChatWidgetLazy() {
  const [active, setActive] = useState(false);
  useEffect(() => {
    setActive(siteBrand().chatEnabled && !isEmbeddedPage() && shouldProbeSession());
  }, []);
  if (!active) return null;
  return (
    <ChatErrorBoundary>
      <ChatWidget />
    </ChatErrorBoundary>
  );
}
