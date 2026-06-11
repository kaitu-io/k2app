import { cloudApi } from './cloud-api';
import type { PrivateNodeListResponse } from './api-types';

/**
 * 拉取当前用户的专属节点订阅列表（GET /api/user/private-nodes）。
 *
 * 走 cloudApi（自动注入鉴权头 + 处理 token 刷新），并按惯例从
 * SResponse 解包 `data`；任何非成功响应或空数据回退为 `{ items: [] }`，
 * 以便上层（usePrivateNodes hook / 管理页）始终拿到稳定的数组。
 */
export async function getPrivateNodes(): Promise<PrivateNodeListResponse> {
  const resp = await cloudApi.get<PrivateNodeListResponse>('/api/user/private-nodes');
  return resp.data ?? { items: [] };
}
