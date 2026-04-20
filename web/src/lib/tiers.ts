/**
 * Tier 常量与类型
 *
 * 与后端 api/tier.go 的 TierQuotas 保持同步。
 * 档位值：lite / basic / family / business（pro 已废弃）。
 *
 * 配额真实来源是后端 `GET /app/tiers`（管理员版，含 inactive plans）
 * 或 `GET /api/tiers`（公开版）。本文件仅定义下拉选项标签与类型。
 */

export const TIER_OPTIONS = [
  { value: "lite", label: "Lite — 轻量版" },
  { value: "basic", label: "Basic — 标准版（默认）" },
  { value: "family", label: "Family — 家庭版" },
  { value: "business", label: "Business — 商业版" },
] as const;

export type TierValue = (typeof TIER_OPTIONS)[number]["value"];

export const DEFAULT_TIER: TierValue = "basic";

// TierQuota 配额信息（对应后端 api.TierQuota）
export interface TierQuota {
  maxDevice: number;
  maxRouterDevice: number;
  maxLanClient: number; // -1 表示无限
}

// TierInfo 元信息（对应后端 api.TierInfo）
export interface TierInfo extends TierQuota {
  name: string;
  rank: number;
}

// TierWithPlans GET /app/tiers 返回条目（对应后端 api.TierWithPlans）
export interface TierWithPlans extends TierInfo {
  plans: Array<{
    pid: string;
    label: string;
    tier?: string;
    price: number;
    originPrice: number;
    month: number;
    highlight: boolean;
    isActive?: boolean;
  }>;
}

export interface TiersResponse {
  tiers: TierWithPlans[];
}

/** 格式化 maxLanClient：-1 展示为"无限" */
export function formatLanClient(n: number): string {
  return n === -1 ? "无限" : String(n);
}
