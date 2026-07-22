/**
 * Gateway core actions (k2r gateway / 路由器面板).
 *
 * 仅在 `window._platform.platformType === 'gateway'` 时调用。走 bridge 层
 * `window._k2.run(action, params)`（gateway 桥实现为 POST /api/core，见
 * `services/gateway-k2.ts`），不在 page/component 里直接 fetch /api/core，
 * 以遵守 webapp 的 bridge 边界宪法。
 */

import { getK2 } from '../core';

/**
 * 把用户从 App / 网页端 mint 出的 `k2subs://` 路由器连接地址提交给本地 k2r，
 * 完成专属线路连接（k2r 侧 `set-credential` action，见 Task 7）。
 *
 * 成功返回 `{ code: 0 }`；地址无效等失败返回 `{ code: 1, message }`。
 */
export async function gatewaySetCredential(
  url: string,
): Promise<{ code: number; message?: string }> {
  const resp = await getK2().run<unknown>('set-credential', { url });
  return { code: resp.code, message: resp.message };
}

/**
 * Customer self-service Wi-Fi rename for one enterprise slot (k2r
 * `set-slot-ssid` action). Persists a takeover flag on the router so
 * manifest convergence never clobbers a customer-set name.
 */
export async function gatewaySetSlotSsid(
  slot: number,
  ssid: string,
): Promise<{ code: number; message?: string }> {
  const resp = await getK2().run<unknown>('set-slot-ssid', { slot, ssid });
  return { code: resp.code, message: resp.message };
}

/**
 * Customer self-service Wi-Fi re-key for one enterprise slot (k2r
 * `set-slot-password` action).
 */
export async function gatewaySetSlotPassword(
  slot: number,
  password: string,
): Promise<{ code: number; message?: string }> {
  const resp = await getK2().run<unknown>('set-slot-password', { slot, password });
  return { code: resp.code, message: resp.message };
}
