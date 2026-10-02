'use client';

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import { siteBrand } from '@/lib/brands';
import { isEmbeddedPage } from '@/lib/funnel';
import { shouldProbeSession } from './gate';

// 挂件本体 + 客户端库单独成块、只在浏览器加载：对 SSR 输出与首屏体积零影响。
const ChatWidget = dynamic(() => import('./ChatWidget'), { ssr: false });

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
  return active ? <ChatWidget /> : null;
}
