/**
 * App → 官网的来源标记。
 *
 * App 里打开自家官网（brandConfig.baseURL 的站点）的链接一律带上
 * `utm_source=app&utm_medium=<os>&utm_campaign=<打开时所在的页面>`，官网的漏斗像素会把它记在
 * 进入记录上，Center 的「应用跳到网站」路径（api/logic_funnel_paths.go `app_to_web`）就靠它
 * 把「从 App 来的访问」与直接访问分开。没有它，App 用户跳到官网后只是一条匿名的直接访问。
 *
 * 只改自家站点：支付页、Stripe、Apple 订阅管理等第三方链接原样放行（它们不发我们的像素）。
 * 已经带 utm_source 的链接（比如运营在公告里自己打了活动标记）不覆盖。
 * App 内嵌页（`embed=true`）不发像素，也不加。
 *
 * 唯一调用点是各平台的 `openExternal`（tauri-k2 / capacitor-k2 / web-platform），
 * 所以任何入口打开官网都会被标记，不需要逐个调用点记得加。
 */
import { brandConfig } from '../brands';

/** 与 Center 的 funnelUtmSourceApp 一致（api/logic_funnel_paths_test.go 锁定这一行）。 */
export const APP_UTM_SOURCE = 'app';

function siteHost(host: string): string {
  return host.toLowerCase().replace(/^www\./, '');
}

/** 打开链接时所在的 App 页面：路由的第一段（`/account/xxx` → `account`），根路由为 `home`。 */
export function appLinkCampaign(pathname: string = window.location.pathname): string {
  const first = pathname.split('/').filter(Boolean)[0] ?? '';
  const clean = first.toLowerCase().replace(/[^a-z0-9_-]/g, '').slice(0, 32);
  return clean || 'home';
}

export function withAppAttribution(url: string, campaign: string = appLinkCampaign()): string {
  let parsed: URL;
  let site: URL;
  try {
    parsed = new URL(url);
    site = new URL(brandConfig.baseURL);
  } catch {
    return url;
  }
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return url;
  if (siteHost(parsed.hostname) !== siteHost(site.hostname)) return url;
  // 接口地址（如 /api/orders/:uuid/pay）不是页面，不发像素。
  if (/^\/(api|app)\//.test(parsed.pathname)) return url;
  if (parsed.searchParams.has('utm_source')) return url;
  if (parsed.searchParams.get('embed') === 'true') return url;
  parsed.searchParams.set('utm_source', APP_UTM_SOURCE);
  parsed.searchParams.set('utm_medium', window._platform?.os || 'unknown');
  parsed.searchParams.set('utm_campaign', campaign);
  return parsed.toString();
}
