/**
 * 会话挂件的"要不要出现"判定与页面内的开窗事件。
 * 独立成小文件：页面只为这点判断付体积，挂件本体与客户端库按需加载（ChatWidgetLazy）。
 */

/** 本浏览器已经成功建过会话（服务端的访客 cookie 是 HttpOnly，前端看不到，所以另记一个标记）。 */
export const CHAT_KNOWN_FLAG = 'chat:known';
/** 本浏览器已经留过邮箱，不再弹留邮箱表单。 */
export const CHAT_EMAIL_FLAG = 'chat:email';

const OPEN_EVENT = 'k2chat:open';

export function readFlag(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === '1';
  } catch {
    return false; // 存储被禁用（隐私模式等）按"没有"处理
  }
}

export function writeFlag(key: string): void {
  try {
    window.localStorage.setItem(key, '1');
  } catch {
    // 存不下只是下次多问一次，不影响聊天
  }
}

export type ChatParam = { kind: 'none' } | { kind: 'preview' } | { kind: 'resume'; token: string };

/** `?chat=preview` 是预览开关；其余非空值是邮件回链里的继续对话令牌。 */
export function parseChatParam(search: string): ChatParam {
  const value = new URLSearchParams(search).get('chat');
  if (!value) return { kind: 'none' };
  return value === 'preview' ? { kind: 'preview' } : { kind: 'resume', token: value };
}

/**
 * 挂载时要不要去问服务端 `session`（问了才知道 `enabled`，才决定画不画入口按钮）。
 *
 * 暗发布阶段的规则：URL 带 `?chat=`（预览或继续对话令牌），或者本浏览器已经建过会话。
 * 两者都没有 = 不渲染、不发任何请求、不种 cookie。
 *
 * 这是下一期唯一要改的地方：服务端开关打开后要"对所有访客显示入口"，需要一个便宜的公开
 * `enabled` 探测（或直接恒返回 true），改这里即可，挂件其余逻辑不动。
 */
export function shouldProbeSession(search: string = window.location.search): boolean {
  return parseChatParam(search).kind !== 'none' || readFlag(CHAT_KNOWN_FLAG);
}

/**
 * 页面上的"在线客服"按钮调用：挂件在场就由它展开并返回 true；
 * 返回 false 表示挂件没渲染，调用方走原来的客服入口。
 */
export function requestOpenChat(): boolean {
  if (typeof window === 'undefined') return false;
  return !window.dispatchEvent(new Event(OPEN_EVENT, { cancelable: true }));
}

/** 挂件登记开窗处理；返回注销函数。 */
export function onOpenChatRequest(handler: () => void): () => void {
  const listener = (ev: Event) => {
    ev.preventDefault();
    handler();
  };
  window.addEventListener(OPEN_EVENT, listener);
  return () => window.removeEventListener(OPEN_EVENT, listener);
}
