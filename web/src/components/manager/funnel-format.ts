// 转化漏斗 / 转化路线两页共用的格式化与时段计算——两页的数字必须按同一口径显示。
import type { FunnelPathInfo, FunnelStepWhere } from "@/lib/api";

export function pct(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

// 归因窗口：满 24 小时按天显示（336 小时 → 14 天），不足一天才用小时。
export function formatWindow(hours: number): string {
  if (hours < 24) return `${hours} 小时`;
  return `${(hours / 24).toFixed(1).replace(/\.0$/, "")} 天`;
}

// 日期按 UTC 计算：to = 今天，from = 往前 days-1 天（含今天共 days 天）。
export function dateRange(days: number): { from: string; to: string } {
  const now = new Date();
  const to = now.toISOString().slice(0, 10);
  const from = new Date(now.getTime() - (days - 1) * 86400000).toISOString().slice(0, 10);
  return { from, to };
}

export const FUNNEL_RANGES = [
  { value: "7", label: "最近 7 天" },
  { value: "30", label: "最近 30 天" },
  { value: "90", label: "最近 90 天" },
];

// 推断归因的首开匹配结局（服务端 logic_funnel_attr.go 的 funnelAttr* 常量 → 中文）。
export const INFERRED_OUTCOME_LABELS: [string, string][] = [
  ["matched", "配上"],
  ["ambiguous", "同出口多人·丢弃"],
  ["none", "无对应点击"],
  ["late", "上报迟到·跳过"],
  ["noip", "出口无效"],
  ["node", "出口是我方节点·跳过"],
];

export const WHERE_LABELS: Record<FunnelStepWhere, string> = {
  web: "网站",
  app: "App",
  any: "网站或 App",
  facts: "账户",
};

/**
 * 每一步前面是否跨面：路线本身就是进入的面，只有明确的「网站」/「App」步骤改变当前所在面；
 * 「网站或 App」与账户事实不改变。返回与 steps 等长的数组，跨面的位置是「跳到哪」。
 */
export function hopsOf(path: Pick<FunnelPathInfo, "lane" | "stepWhere">): (FunnelStepWhere | null)[] {
  let at: FunnelStepWhere = path.lane;
  return path.stepWhere.map((w) => {
    if ((w === "web" || w === "app") && w !== at) {
      at = w;
      return w;
    }
    return null;
  });
}

