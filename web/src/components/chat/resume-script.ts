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
 * 同一段逻辑用在两处（只有这一份源）：
 * 1. `src/instrumentation-client.ts` 的第一条语句直接调用——它先于监控 SDK 的 init 执行，是主路径；
 * 2. 根布局的内联脚本（下面由函数源码拼出的字符串）——兜底，片段已被移除时什么都不做。
 *
 * 值按原样存（不解码），由 gate.ts 的 takeResumeToken() 取走并删除。
 */
export const CHAT_RESUME_GLOBAL = '__chatResume';
export const CHAT_RESUME_HASH_PREFIX = '#chat=';

type ResumeHost = {
  location: { hash: string; pathname: string; search: string };
  history: { replaceState(data: unknown, unused: string, url: string): void };
};

/**
 * 必须自包含：函数体不能引用任何模块级绑定（上面两个常量在这里写成字面量），
 * 因为内联脚本就是它的源码字符串。gate 的测试锁住字面量与常量一致。
 */
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

export const CHAT_RESUME_SCRIPT = `(${stripChatResume.toString()})(window);`;
