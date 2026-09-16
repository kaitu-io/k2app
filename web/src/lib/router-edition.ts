import type { UserRouterFulfillment, RouterStage } from './api';

export function buildInstallCommand(origin: string, url: string): string {
  const base = origin.replace(/\/+$/, '');
  return `wget -qO- ${base}/i/k2r | sh -s '${url}'`;
}

export function formatUsd(cents: number): string {
  const dollars = cents / 100;
  return Number.isInteger(dollars) ? `$${dollars}` : `$${dollars.toFixed(2)}`;
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
