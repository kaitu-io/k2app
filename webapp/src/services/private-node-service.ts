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

/** A router candidate discovered on the local network for BYO onboarding. */
export interface RouterCandidate {
  lanIP: string;
  port: number;
}

/**
 * Mint a gateway k2subs credential for the current user
 * (POST /api/user/gateway-credential).
 *
 * Returns the `k2subs://` URL the router (k2r gateway) uses to fetch its
 * subscription. Empty string on any non-success / empty-data response so
 * callers can treat it as "no credential yet" without unwrapping SResponse.
 */
export async function mintGatewayCredential(): Promise<string> {
  const resp = await cloudApi.post<{ url: string }>('/api/user/gateway-credential', {});
  return resp.data?.url ?? '';
}

/**
 * Discover candidate routers on the local network for BYO onboarding
 * (GET /api/pair/discover).
 *
 * Returns the candidate list (LAN IP + port), or an empty array on any
 * non-success / empty-data response.
 */
export async function discoverRouter(): Promise<RouterCandidate[]> {
  const resp = await cloudApi.get<{ candidates: RouterCandidate[] }>('/api/pair/discover');
  return resp.data?.candidates ?? [];
}
