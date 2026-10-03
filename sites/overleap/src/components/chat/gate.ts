/**
 * 会话挂件的"要不要出现"判定、入口参数与页面内的开窗事件。
 * 独立成小文件：页面只为这点判断付体积，挂件本体与客户端库按需加载（ChatWidgetLazy）。
 */
import { CHAT_RESUME_GLOBAL, CHAT_RESUME_HASH_PREFIX } from './resume-script';

/** 本浏览器已经成功建过会话（服务端的访客 cookie 是 HttpOnly，前端看不到，所以另记一个标记）。 */
export const CHAT_KNOWN_FLAG = 'chat:known';
/** 本浏览器已经留过邮箱，不再弹留邮箱表单。 */
export const CHAT_EMAIL_FLAG = 'chat:email';
/** 本标签页见过 `?chat=preview`（sessionStorage）：预览身份跟着站内跳转走。 */
export const CHAT_PREVIEW_FLAG = 'chat:preview';

const OPEN_EVENT = 'k2chat:open';

function readStore(kind: 'localStorage' | 'sessionStorage', key: string): boolean {
  try {
    return window[kind].getItem(key) === '1';
  } catch {
    return false; // 存储被禁用（隐私模式等）按"没有"处理
  }
}

function writeStore(kind: 'localStorage' | 'sessionStorage', key: string): void {
  try {
    window[kind].setItem(key, '1');
  } catch {
    // 存不下只是下次多问一次，不影响聊天
  }
}

export const readFlag = (key: string) => readStore('localStorage', key);
export const writeFlag = (key: string) => writeStore('localStorage', key);

/**
 * 预览身份：URL 带 `?chat=preview`，或本标签页之前带过。
 * `?chat=` 的其他取值一律忽略——继续对话令牌只从 URL 片段（#chat=）来。
 */
export function isPreview(): boolean {
  if (new URLSearchParams(window.location.search).get('chat') === 'preview') {
    writeStore('sessionStorage', CHAT_PREVIEW_FLAG);
    return true;
  }
  return readStore('sessionStorage', CHAT_PREVIEW_FLAG);
}

type ResumeWindow = { [CHAT_RESUME_GLOBAL]?: unknown };

function hashToken(): string {
  const hash = window.location.hash;
  return hash.startsWith(CHAT_RESUME_HASH_PREFIX) ? hash.slice(CHAT_RESUME_HASH_PREFIX.length) : '';
}

/** 有没有待用的继续对话令牌（不取走）。 */
export function hasResumeToken(): boolean {
  const stashed = (window as unknown as ResumeWindow)[CHAT_RESUME_GLOBAL];
  return (typeof stashed === 'string' && stashed !== '') || hashToken() !== '';
}

/**
 * 取走继续对话令牌（只能取一次）。正常由根布局的内联脚本在统计脚本之前挪到 window 上；
 * 那段脚本没跑（被拦、站内跳转带片段）时自己从片段里读，并把片段从地址栏移除。
 */
export function takeResumeToken(): string | null {
  const w = window as unknown as ResumeWindow;
  let raw = typeof w[CHAT_RESUME_GLOBAL] === 'string' ? (w[CHAT_RESUME_GLOBAL] as string) : '';
  delete w[CHAT_RESUME_GLOBAL];
  const fromHash = hashToken();
  if (fromHash) {
    raw = raw || fromHash;
    window.history.replaceState(window.history.state, '', window.location.pathname + window.location.search);
  }
  if (!raw) return null;
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

/**
 * 挂载时要不要去问服务端 `session`（问了才知道 `enabled`，才决定画不画入口按钮）。
 *
 * 暗发布阶段的规则：预览身份、带着继续对话令牌，或者本浏览器已经建过会话。
 * 三者都没有 = 不渲染、不发任何请求、不种 cookie。
 *
 * 正式上线（对所有访客显示入口）不能只把这里改成恒返回 true：现在"问服务端"就是 `start()`，
 * 它会新建访客主体并种 cookie——对每个路过的访客都这么做不可接受。需要三件事一起改：
 * 1. 服务端加一个不建主体、可缓存的 `enabled` 探测接口；
 * 2. 挂件按探测结果先画入口，此时不调 `start()`；
 * 3. `start()`（会建访客、种 cookie）推迟到访客第一次展开面板，或本浏览器已有
 *    `chat:known` 标记（建过会话，要恢复它）时才调。
 */
export function shouldProbeSession(): boolean {
  return isPreview() || hasResumeToken() || readFlag(CHAT_KNOWN_FLAG);
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
