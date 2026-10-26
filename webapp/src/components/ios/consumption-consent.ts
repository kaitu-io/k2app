/**
 * 退款同意（Apple Consumption 信息）写入：PUT /api/user/apple-consumption-consent。
 * 成功后把返回的 DataUser 回写用户缓存（useUser 订阅该 key），弹层不会再次出现。
 * 失败返回 false 且不写任何状态。
 */
import { cloudApi } from '../../services/cloud-api';
import { cacheStore } from '../../services/cache-store';
import type { DataUser } from '../../services/api-types';

export async function setAppleConsumptionConsent(consented: boolean): Promise<boolean> {
  try {
    const res = await cloudApi.request<DataUser>('PUT', '/api/user/apple-consumption-consent', { consented });
    if (res.code === 0 && res.data) {
      cacheStore.set('api:user_info', res.data, { ttl: 3600 });
      return true;
    }
  } catch (e) {
    console.warn('[consumption-consent] PUT failed:', e);
  }
  return false;
}
