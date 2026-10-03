/**
 * 把一段纯文本切成"文字 / http(s) 链接"片段。只认 http:// 与 https://；
 * 其余协议（javascript:、data: 等）不会被切成链接，原样当文字显示。
 */
export type LinkPart = string | { url: string };

// 只匹配 URL 里合法的 ASCII 字符（RFC 3986 的 unreserved + reserved + %）。
// 中文没有空格分词：按"非空白"匹配会把链接后面的整句中文吞进 href。
const URL_RE = /https?:\/\/[A-Za-z0-9\-._~:/?#[\]@!$&'()*+,;=%]+/g;
const TRAILING = new Set(['.', ',', ';', ':', '!', '?', "'", '*']);

const count = (s: string, ch: string) => s.split(ch).length - 1;

/** 去掉句末标点；右括号只在括号不配对时才算标点（保留 /wiki/Foo_(bar) 这类链接）。 */
function trimUrl(raw: string): string {
  let url = raw;
  for (;;) {
    const last = url[url.length - 1];
    if (TRAILING.has(last)) url = url.slice(0, -1);
    else if (last === ')' && count(url, ')') > count(url, '(')) url = url.slice(0, -1);
    else if (last === ']' && count(url, ']') > count(url, '[')) url = url.slice(0, -1);
    else return url;
  }
}

export function linkParts(text: string): LinkPart[] {
  const out: LinkPart[] = [];
  let last = 0;
  for (const match of text.matchAll(URL_RE)) {
    const start = match.index ?? 0;
    const url = trimUrl(match[0]);
    if (!/^https?:\/\/[^/?#]/.test(url)) continue; // 只剩协议头的不算链接
    if (start > last) out.push(text.slice(last, start));
    out.push({ url });
    last = start + url.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
