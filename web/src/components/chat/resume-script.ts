/**
 * 邮件回链 `/…#chat=<令牌>` 的继续对话令牌：页面一加载就把它从地址栏挪到 `window.__chatResume`。
 *
 * 作为内联脚本放在根布局里、排在统计脚本之前——令牌不进统计上报、不进历史记录、不进 Referer。
 * 与挂件代码块无关（挂件按需加载，来不及）；哪个品牌都执行，没有令牌时什么都不做。
 * 值按原样存（不解码），由 gate.ts 的 takeResumeToken() 取走并删除。
 */
export const CHAT_RESUME_GLOBAL = '__chatResume';
export const CHAT_RESUME_HASH_PREFIX = '#chat=';

export const CHAT_RESUME_SCRIPT = `(function(){try{var h=location.hash;if(h.indexOf(${JSON.stringify(
  CHAT_RESUME_HASH_PREFIX,
)})===0){window.${CHAT_RESUME_GLOBAL}=h.slice(${CHAT_RESUME_HASH_PREFIX.length});history.replaceState(null,'',location.pathname+location.search)}}catch(e){}})();`;
