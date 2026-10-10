/**
 * 会话挂件的"要不要出现"判定、入口参数与页面内的开窗事件。
 * 独立成小文件：页面只为这点判断付体积，挂件本体与客户端库按需加载（ChatWidgetLazy）。
 */
import { CHAT_RESUME_GLOBAL, CHAT_RESUME_HASH_PREFIX } from './resume-script';

/** 本浏览器已经成功建过会话（服务端的访客 cookie 是 HttpOnly，前端看不到，所以另记一个标记）。 */
export const CHAT_KNOWN_FLAG = 'chat:known';
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
 * 挂载时要不要立刻调 `start()`（问服务端 `session`，会新建访客主体并种 cookie）。
 *
 * 只有这三种访客立刻建会话：预览身份、带着继续对话令牌、本浏览器已经建过会话（要恢复它）。
 * 其余访客先用 `probeChatEnabled()` 问一下开没开（不建主体、不种 cookie），开着就只画入口按钮，
 * 等访客第一次展开面板才调 `start()`（裁定 R26）。
 */
export function shouldStartSession(): boolean {
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

/** 入口探测结果在本标签页内的缓存：开关极少变，不必每次站内跳转都问一遍。 */
export const CHAT_ENABLED_CACHE = 'chat:enabled';
const ENABLED_CACHE_MS = 5 * 60_000;

/**
 * 问服务端会话功能对普通访客开没开（`GET /api/chat/enabled`）：只读开关，不建访客、不种 cookie。
 * 结果在本标签页缓存 5 分钟。任何失败都按"没开"处理——探测坏了只是少一个入口。
 */
export async function probeChatEnabled(brandId: string, fetchImpl: typeof fetch = fetch): Promise<boolean> {
  try {
    const cached = JSON.parse(window.sessionStorage.getItem(CHAT_ENABLED_CACHE) ?? 'null') as
      | { enabled?: unknown; at?: unknown }
      | null;
    if (cached && typeof cached.enabled === 'boolean' && typeof cached.at === 'number'
      && Date.now() - cached.at >= 0 && Date.now() - cached.at < ENABLED_CACHE_MS) {
      return cached.enabled;
    }
  } catch {
    // 存储被禁用或内容损坏：照常去问
  }
  let enabled = false;
  try {
    const res = await fetchImpl('/api/chat/enabled', { headers: { 'X-K2-Brand': brandId } });
    if (!res.ok) return false;
    const body = (await res.json()) as { code?: number; data?: { enabled?: unknown } };
    enabled = body.code === 0 && body.data?.enabled === true;
  } catch {
    return false; // 网络错误不缓存，下一页再问
  }
  try {
    window.sessionStorage.setItem(CHAT_ENABLED_CACHE, JSON.stringify({ enabled, at: Date.now() }));
  } catch {
    // 存不下只是下一页多问一次
  }
  return enabled;
}
