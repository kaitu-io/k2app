/**
 * Kaitu Core - 核心模块入口
 *
 * 直接使用全局 window._k2 访问所有功能
 */

import type { IK2Vpn } from '../types/kaitu-core';

/**
 * 获取 K2 实例
 *
 * @throws 如果平台未注入 window._k2
 *
 * @example
 * const k2 = getK2();
 * await k2.core.exec('start');
 * await k2.api.exec('login', { email, code });
 * await k2.window._k2!.platform.openExternal('https://kaitu.io');
 */
export function getK2(): IK2Vpn {
  if (!window._k2) {
    throw new Error(
      'K2 not available. ' +
      'Platform should inject window._k2 before app starts.'
    );
  }
  return window._k2;
}

/**
 * 检查 K2 是否可用
 */
export function isK2Ready(): boolean {
  return !!window._k2;
}

/**
 * 等待 K2 可用
 *
 * @param timeout - 超时时间（毫秒），默认 5000
 * @returns 是否成功
 */
export async function waitForK2(timeout = 5000): Promise<boolean> {
  if (isK2Ready()) return true;

  const startTime = Date.now();
  while (Date.now() - startTime < timeout) {
    if (isK2Ready()) return true;
    await new Promise(r => setTimeout(r, 100));
  }
  return false;
}

// Re-export types
export type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';

// Re-export polling
export { useStatusPolling, pollStatusOnce } from './polling';
