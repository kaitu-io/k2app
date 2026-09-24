import type { UserRouterFulfillment, RouterStage } from './api';

export function buildInstallCommand(origin: string, url: string): string {
  const base = origin.replace(/\/+$/, '');
  return `wget -qO- ${base}/i/k2r | sh -s '${url}'`;
}

export function formatUsd(cents: number): string {
  const dollars = cents / 100;
  return Number.isInteger(dollars) ? `$${dollars}` : `$${dollars.toFixed(2)}`;
}

// 与 docs/router-edition-prod-deploy.md 的套餐 SQL 同源（测试锁定）。改价 = 改 SQL + 改这里。
// firstYear 是标价（发售后价）；预售期实际售价在 ROUTER_PRESALE。
export const EDITION_PRICE_CENTS = { firstYear: 39900, renewal: 29900 } as const;

// 预售：发售日（东八区零点）之前按 firstYear 卖，划线 EDITION_PRICE_CENTS.firstYear；
// 发货从 shipsFrom 起。与部署文档的预售 UPDATE 同源（测试锁定）。发售当天运营把套餐价改回标价，
// 网站按日期自动切换——数据库慢一步的失败方向是用户少付，不会多付。
export const ROUTER_PRESALE = { firstYear: 35900, shipsFrom: '2026-11-11' } as const;

export interface RouterOffer {
  /** 当前是否预售期。 */
  presale: boolean;
  /** 首年含路由器的实际售价（美分）。 */
  firstYear: number;
  /** 预售期的划线标价；非预售期不给。 */
  originFirstYear?: number;
  /** 续费一年（美分）。 */
  renewal: number;
  /** 发货起始日 ISO 日期（YYYY-MM-DD）。 */
  shipsFrom: string;
}

/** 发售日东八区零点的时间戳；预售 = 现在早于它。 */
export function presaleEndsAt(shipsFrom: string = ROUTER_PRESALE.shipsFrom): number {
  return Date.parse(`${shipsFrom}T00:00:00+08:00`);
}

export function isRouterPresale(now: Date | number = Date.now()): boolean {
  const ts = typeof now === 'number' ? now : now.getTime();
  return ts < presaleEndsAt();
}

export function routerOffer(now: Date | number = Date.now()): RouterOffer {
  const presale = isRouterPresale(now);
  return presale
    ? {
        presale,
        firstYear: ROUTER_PRESALE.firstYear,
        originFirstYear: EDITION_PRICE_CENTS.firstYear,
        renewal: EDITION_PRICE_CENTS.renewal,
        shipsFrom: ROUTER_PRESALE.shipsFrom,
      }
    : { presale, firstYear: EDITION_PRICE_CENTS.firstYear, renewal: EDITION_PRICE_CENTS.renewal, shipsFrom: ROUTER_PRESALE.shipsFrom };
}

/** 发货日的本地化展示（如「11月11日」/ "11 November"）。 */
export function formatShipsFrom(shipsFrom: string, locale: string): string {
  const [y, m, d] = shipsFrom.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { month: 'long', day: 'numeric', timeZone: 'UTC' }).format(Date.UTC(y, m - 1, d));
}

export type RouterProgressState = 'done' | 'current' | 'todo';

export interface RouterProgressStep {
  key: Exclude<RouterStage, 'expired'>;
  state: RouterProgressState;
}

const HARDWARE_FLOW: RouterProgressStep['key'][] = ['paid', 'provisioning', 'ready', 'shipped', 'online'];
const BYO_FLOW: RouterProgressStep['key'][] = ['paid', 'provisioning', 'ready', 'online'];

// 过期返回空数组：页面单独展示「已过期，续费后恢复」。
export function routerProgressSteps(f: UserRouterFulfillment): RouterProgressStep[] {
  if (f.stage === 'expired') return [];
  const flow = f.hardwareSku ? HARDWARE_FLOW : BYO_FLOW;
  const idx = flow.indexOf(f.stage as RouterProgressStep['key']);
  return flow.map((key, i) => {
    if (f.stage === 'online') return { key, state: 'done' };
    if (i < idx) return { key, state: 'done' };
    if (i === idx) return { key, state: 'current' };
    return { key, state: 'todo' };
  });
}

export function quotaLevel(used: number, total: number): 'normal' | 'warn' | 'danger' {
  if (total <= 0) return 'normal';
  const ratio = used / total;
  if (ratio >= 0.95) return 'danger';
  if (ratio >= 0.8) return 'warn';
  return 'normal';
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  let i = 0;
  let v = bytes;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const s = Number.isInteger(v) ? String(v) : v.toFixed(1).replace(/\.0$/, '');
  return `${s} ${UNITS[i]}`;
}
