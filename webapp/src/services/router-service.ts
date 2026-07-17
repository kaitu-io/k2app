/**
 * router-service — LAN k2r 发现与控制（headless 路由器,app 是唯一 UI）。
 *
 * 发现=控制=同一锚点常量 URL(spec §3.4):k2r 在转发路径上 DNAT 拦截锚点流量,
 * 在网关链任意一层均可达;通即有路由器,不通即没有。无探测链/beacon/lanIP 缓存。
 * 传输:原生 HTTP 桥(_platform.routerRequest,绕 CORS/mixed-content)
 * + Bearer controlKey;401 时向 Center 强刷 key 重试一次(轮换收敛)。
 * Spec: docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md §3.4/§5
 */
import { cloudApi } from './cloud-api';
import type { RouterRequestOptions, RouterResponse } from '../types/kaitu-core';

/** 锚点常量——RFC1918 罕用段,17.79 呼应端口 1779。与 k2r DNAT 规则、B4/B5 SSRF 门共同构成契约。 */
export const ROUTER_ANCHOR = 'http://10.17.79.1:1779';

export interface RouterInfo {
  name: string;
  version: string;
  configured: boolean;
}

export interface RouterCoreResponse<T = unknown> {
  code: number;
  message?: string;
  data?: T;
}

const PROBE_TIMEOUT_MS = 1500;
const CORE_TIMEOUT_MS = 10000;
const STORAGE_LAST_ROUTER = 'k2.router.last';
const STORAGE_CONTROL_KEY = 'k2.router.control_key';

async function routerRequest(opts: RouterRequestOptions): Promise<RouterResponse> {
  const fn = window._platform?.routerRequest;
  if (!fn) throw new Error('router bridge unavailable');
  return fn(opts);
}

/** GET {ROUTER_ANCHOR}/ping,校验 k2r 发现签名。非 k2r(撞段真机)/超时/网络错误一律 null。 */
export async function probeRouter(): Promise<RouterInfo | null> {
  try {
    const resp = await routerRequest({
      url: `${ROUTER_ANCHOR}/ping`,
      timeoutMs: PROBE_TIMEOUT_MS,
    });
    if (resp.status !== 200) return null;
    const data = JSON.parse(resp.body);
    if (data?.k2r !== true) return null;
    return {
      name: typeof data.name === 'string' ? data.name : '',
      version: typeof data.version === 'string' ? data.version : '',
      configured: data.configured === true,
    };
  } catch {
    return null;
  }
}

/** 「曾配对」展示态(离线时 Router tab 保留置灰的判据)。只存 name,无网络语义。 */
export async function saveLastRouter(info: RouterInfo): Promise<void> {
  await window._platform?.storage?.set(STORAGE_LAST_ROUTER, JSON.stringify({ name: info.name }));
}

export async function loadLastRouter(): Promise<{ name: string } | null> {
  try {
    const raw = await window._platform?.storage?.get(STORAGE_LAST_ROUTER);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

export async function clearLastRouter(): Promise<void> {
  await window._platform?.storage?.remove(STORAGE_LAST_ROUTER);
}

/** controlKey:本地缓存优先;缺失/强刷时走 Center 幂等端点并回写缓存。 */
export async function getControlKey(forceRefresh = false): Promise<string | null> {
  if (!forceRefresh) {
    const cached = await window._platform?.storage?.get(STORAGE_CONTROL_KEY);
    if (cached) return cached;
  }
  const resp = await cloudApi.post<{ controlKey: string }>('/api/user/router-control-key', {});
  const key = resp.data?.controlKey ?? null;
  if (key) await window._platform?.storage?.set(STORAGE_CONTROL_KEY, key);
  return key;
}

/**
 * 共享请求 helper：{ROUTER_ANCHOR}{path};HTTP 401 → 强刷 key 重试一次
 * (k2r 侧 key 已被 Center 权威轮换)。routerCore 与 routerDevices* 共用。
 */
async function routerFetch<T = unknown>(
  path: string,
  method: 'GET' | 'POST',
  body?: unknown,
): Promise<RouterCoreResponse<T>> {
  const send = (key: string | null) =>
    routerRequest({
      url: `${ROUTER_ANCHOR}${path}`,
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(key ? { Authorization: `Bearer ${key}` } : {}),
      },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
      timeoutMs: CORE_TIMEOUT_MS,
    });

  let resp = await send(await getControlKey());
  if (resp.status === 401) {
    resp = await send(await getControlKey(true));
  }
  if (resp.status === 401) return { code: 401, message: 'unauthorized' };
  try {
    return JSON.parse(resp.body) as RouterCoreResponse<T>;
  } catch {
    return { code: -1, message: 'bad response' };
  }
}

/** POST {ROUTER_ANCHOR}/api/core;HTTP 401 → 强刷 key 重试一次(k2r 侧 key 已被 Center 权威轮换)。 */
export async function routerCore<T = unknown>(
  action: string,
  params?: Record<string, unknown>,
): Promise<RouterCoreResponse<T>> {
  return routerFetch<T>('/api/core', 'POST', { action, params: params ?? {} });
}

/** GET {ROUTER_ANCHOR}/api/router-devices — LAN 设备列表。同 routerCore 的鉴权+401 重试语义。 */
export async function routerDevicesGet<T = unknown>(): Promise<RouterCoreResponse<T>> {
  return routerFetch<T>('/api/router-devices', 'GET');
}

/** POST {ROUTER_ANCHOR}/api/router-devices{subPath} — 设备 mode/allow/remove 等写操作。 */
export async function routerDevicesPost<T = unknown>(
  subPath: string,
  body?: Record<string, unknown>,
): Promise<RouterCoreResponse<T>> {
  return routerFetch<T>(`/api/router-devices${subPath}`, 'POST', body ?? {});
}
