/**
 * 广告着陆页 /go/<code> 的归因码注册表与纯函数。
 *
 * 码语义不可变，换素材就发新码；码只做归因，不做门禁
 * （未知码照常渲染稳定版，只是归因里保留原码）。
 */
export type AdVariant = 'stable' | 'compare';

export interface AdCode {
  variant: AdVariant;
  channel: 'x' | 'google';
  via?: 'qr';
}

export const AD_CODES: Readonly<Record<string, AdCode>> = {
  xa1: { variant: 'stable', channel: 'x' },
  xb1: { variant: 'compare', channel: 'x' },
  xa1q: { variant: 'stable', channel: 'x', via: 'qr' },
  xb1q: { variant: 'compare', channel: 'x', via: 'qr' },
};

/** App Store Connect 的 provider token；为空时 App Store 链接不带归因参数 */
export const APP_STORE_PROVIDER_TOKEN = '';

const FALLBACK: AdCode = { variant: 'stable', channel: 'x' };

/** 小写化、去掉 %XX 转义、剔除 [a-z0-9-] 之外的字符；未知码回退稳定版但保留原码 */
export function resolveAdCode(raw: string): { code: string; entry: AdCode } {
  const code = raw
    .toLowerCase()
    .replace(/%[0-9a-f]{2}/g, '')
    .replace(/[^a-z0-9-]/g, '');
  return { code, entry: AD_CODES[code] ?? FALLBACK };
}

export type AdPlatform = 'ios' | 'android' | 'desktop';

/** iPadOS 的桌面版 UA 伪装成 Macintosh，靠触点数区分 */
export function detectAdPlatform(ua: string, maxTouchPoints: number): AdPlatform {
  if (/iPhone|iPad|iPod/.test(ua)) return 'ios';
  if (/Macintosh/.test(ua) && maxTouchPoints > 1) return 'ios';
  if (/Android/.test(ua)) return 'android';
  return 'desktop';
}

/** 有 provider token 时附带 pt / ct / mt=8 归因参数，否则返回原链接 */
export function appStoreUrl(base: string, code: string, providerToken: string): string {
  if (!providerToken) return base;
  return `${base}?pt=${providerToken}&ct=${code}&mt=8`;
}

/** 二维码专用码：已是注册的 qr 码则原样返回，否则加 q 后缀 */
export function qrCodeFor(code: string): string {
  if (AD_CODES[code]?.via === 'qr') return code;
  return `${code}q`;
}

/** 二维码目标地址：只保留 utm_*（不带 twclid 等点击 ID），路径换成 q 码 */
export function qrTargetUrl(baseUrl: string, locale: string, code: string, search: string): string {
  const kept = new URLSearchParams();
  new URLSearchParams(search).forEach((v, k) => {
    if (k.startsWith('utm_')) kept.append(k, v);
  });
  const qs = kept.toString();
  return `${baseUrl}/${locale}/go/${qrCodeFor(code)}${qs ? `?${qs}` : ''}`;
}
