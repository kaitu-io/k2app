/**
 * 邮件回链 `/…#chat=<令牌>` 的继续对话令牌：页面一加载就把它从地址栏挪到 `window.__chatResume`。
 *
 * 这一步管得住的：之后读 `location` 的一切——统计脚本、历史记录、后续请求的 Referer、
 * 监控 SDK 在 init 时记下的初始地址与导航面包屑。
 * 这一步管不住的：浏览器的导航性能条目（PerformanceNavigationTiming.name）永远保留加载时的
 * 完整地址，`replaceState` 改不了它。监控 SDK 的页面加载事务、独立 span 与会话回放的
 * performanceSpan 会读它——那几条路径靠 `lib/sentry-filters.ts` 的 scrubChatResume* 钩子擦除
 *（在 instrumentation-client.ts 里接入），不是靠这里。
 *
 * 同一段逻辑用在两处，谁先跑都安全（后跑的那个发现片段已被移除，什么都不做）：
 * 1. `src/instrumentation-client.ts` 的第一条语句调用 `stripChatResume`——必定先于监控 SDK 的 init；
 * 2. 根布局的内联脚本 `CHAT_RESUME_SCRIPT`——不经打包转译、直接进 HTML，所以是手写的 ES5，
 *    不能由函数源码拼出来（那会把 const、无绑定 catch 原样带给老浏览器，整段脚本解析失败）。
 * 两份实现由 ChatWidget 的测试逐用例比对，改一处必须改另一处。
 *
 * 值按原样存（不解码），由 gate.ts 的 takeResumeToken() 取走并删除。
 */
export const CHAT_RESUME_GLOBAL = '__chatResume';
export const CHAT_RESUME_HASH_PREFIX = '#chat=';

type ResumeHost = {
  location: { hash: string; pathname: string; search: string };
  history: { replaceState(data: unknown, unused: string, url: string): void };
};

/** 字面量（'#chat='、6、__chatResume）与内联脚本保持逐字一致；测试锁住它们与上面两个常量相符。 */
export function stripChatResume(w: ResumeHost): void {
  try {
    const h = w.location.hash;
    if (h.indexOf('#chat=') === 0) {
      (w as unknown as Record<string, unknown>).__chatResume = h.slice(6);
      w.history.replaceState(null, '', w.location.pathname + w.location.search);
    }
  } catch {
    // 地址栏改不了就算了：gate 取令牌时会再试一次
  }
}

/** 手写 ES5，行为与 `stripChatResume` 一致（见文件头）。 */
export const CHAT_RESUME_SCRIPT =
  '(function(w){try{var h=w.location.hash;if(h.indexOf("#chat=")===0){w.__chatResume=h.slice(6);w.history.replaceState(null,"",w.location.pathname+w.location.search);}}catch(e){}})(window);';
