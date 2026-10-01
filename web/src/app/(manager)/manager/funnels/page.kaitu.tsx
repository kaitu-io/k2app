"use client";

export const dynamic = "force-dynamic";

import { useEffect, useMemo, useState } from "react";
import {
  api,
  ApiError,
  type ActiveRetentionResult,
  type FunnelPathInfo,
  type FunnelResult,
  type PaidRetentionResult,
} from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { useManagerBrand } from "@/components/manager/brand";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const NO_GROUP = "__none__";
const RANGES = [
  { value: "7", label: "最近 7 天" },
  { value: "30", label: "最近 30 天" },
  { value: "90", label: "最近 90 天" },
];

// 分组维度的中文名；未登记的维度原样显示键名。
const DIM_LABELS: Record<string, string> = {
  source: "来源",
  utm_campaign: "投放活动",
  country: "国家",
  os: "系统",
  device: "设备",
  app_version: "App 版本",
  paywall_source: "付费墙入口",
  plan: "套餐",
  channel: "渠道",
};

// 服务端把第 50 名之后的分组合并成这一行；它不是一个真实的分组值，排序时始终垫底。
const OTHER_GROUP = "(other)";
// 分组值里的保留键换成中文；其余分组值（来源域名、国家代码、套餐 pid…）原样显示。
const GROUP_KEY_LABELS: Record<string, string> = {
  direct: "直接访问",
  unknown: "未知",
  [OTHER_GROUP]: "其他（第 50 名之后合并）",
};

type RetentionState<T> = { data: T | null; loading: boolean; error: string | null };
const RETENTION_IDLE = { data: null, loading: false, error: null };

export default function FunnelsPage() {
  const { brandParam } = useManagerBrand();
  const [paths, setPaths] = useState<FunnelPathInfo[]>([]);
  const [groupDims, setGroupDims] = useState<string[]>([]);
  const [pathKey, setPathKey] = useState("");
  const [days, setDays] = useState("30");
  const [groupBy, setGroupBy] = useState(NO_GROUP);
  const [tab, setTab] = useState("funnel");

  const [funnel, setFunnel] = useState<FunnelResult | null>(null);
  const [funnelLoading, setFunnelLoading] = useState(false);
  const [funnelError, setFunnelError] = useState<string | null>(null);

  // 两个留存指标各取各的：一个失败或还没回来，不影响另一张表。
  const [paid, setPaid] = useState<RetentionState<PaidRetentionResult>>(RETENTION_IDLE);
  const [active, setActive] = useState<RetentionState<ActiveRetentionResult>>(RETENTION_IDLE);

  const [pathsLoading, setPathsLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    api
      .getFunnelPaths()
      .then((r) => {
        if (cancelled) return;
        setPaths(r.paths);
        setGroupDims(r.groupDims);
        setPathKey(r.paths[0]?.key ?? "");
      })
      .catch((e) => !cancelled && setFunnelError(errorText(e)))
      .finally(() => !cancelled && setPathsLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!pathKey) return;
    let cancelled = false;
    const { from, to } = dateRange(Number(days));
    setFunnel(null); // 不让上一次选择的数字在新请求落地前继续显示
    setFunnelLoading(true);
    setFunnelError(null);
    api
      .getFunnel(pathKey, { brand: brandParam, from, to, groupBy: groupBy === NO_GROUP ? undefined : groupBy })
      .then((r) => !cancelled && setFunnel(r))
      .catch((e) => {
        if (cancelled) return;
        setFunnel(null);
        setFunnelError(errorText(e));
      })
      .finally(() => !cancelled && setFunnelLoading(false));
    return () => {
      cancelled = true;
    };
  }, [pathKey, days, groupBy, brandParam]);

  useEffect(() => {
    if (tab !== "retention") return;
    let cancelled = false;
    setPaid({ data: null, loading: true, error: null });
    api
      .getRetention({ brand: brandParam, metric: "paid" })
      .then((data) => !cancelled && setPaid({ data, loading: false, error: null }))
      .catch((e) => !cancelled && setPaid({ data: null, loading: false, error: errorText(e) }));
    return () => {
      cancelled = true;
    };
  }, [tab, brandParam]);

  useEffect(() => {
    if (tab !== "retention") return;
    let cancelled = false;
    setActive({ data: null, loading: true, error: null });
    api
      .getRetention({ brand: brandParam, metric: "active" })
      .then((data) => !cancelled && setActive({ data, loading: false, error: null }))
      .catch((e) => !cancelled && setActive({ data: null, loading: false, error: errorText(e) }));
    return () => {
      cancelled = true;
    };
  }, [tab, brandParam]);

  const current = paths.find((p) => p.key === pathKey);

  return (
    <div className="p-6 space-y-6">
      {/* 路径 / 时段 / 分组只作用于漏斗；留存页签下它们什么都不改变，所以不显示。 */}
      {tab === "funnel" && (
      <div className="flex flex-wrap items-start gap-4">
        <div className="space-y-1">
          <Select name="path" value={pathKey} onValueChange={setPathKey}>
            <SelectTrigger className="w-56"><SelectValue placeholder="选择路径" /></SelectTrigger>
            <SelectContent>
              {paths.map((p) => (
                <SelectItem key={p.key} value={p.key}>{p.title}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {current && <p className="text-xs text-muted-foreground max-w-56">{current.question}</p>}
        </div>
        <Select name="range" value={days} onValueChange={setDays}>
          <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
          <SelectContent>
            {RANGES.map((r) => (
              <SelectItem key={r.value} value={r.value}>{r.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select name="group" value={groupBy} onValueChange={setGroupBy}>
          <SelectTrigger className="w-40"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NO_GROUP}>不分组</SelectItem>
            {groupDims.map((d) => (
              <SelectItem key={d} value={d}>{DIM_LABELS[d] ?? d}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      )}

      {tab === "funnel" && funnelError && (
        <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 text-destructive px-4 py-3 text-sm">
          {funnelError}
        </div>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="funnel">漏斗</TabsTrigger>
          <TabsTrigger value="retention">留存</TabsTrigger>
        </TabsList>

        <TabsContent value="funnel" className="space-y-6">
          {pathsLoading || funnelLoading ? (
            <div className="text-muted-foreground text-sm py-8 text-center">加载中…</div>
          ) : funnel ? (
            <FunnelView
              funnel={funnel}
              windowHours={current?.windowHours}
              dimLabel={groupBy === NO_GROUP ? "" : DIM_LABELS[groupBy] ?? groupBy}
            />
          ) : null}
        </TabsContent>

        <TabsContent value="retention" className="space-y-6">
          <PaidTable state={paid} />
          <ActiveTable state={active} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function FunnelView({ funnel, windowHours, dimLabel }: { funnel: FunnelResult; windowHours?: number; dimLabel: string }) {
  const first = funnel.steps[0]?.count ?? 0;
  if (first === 0) {
    return (
      <Card>
        <CardContent className="text-muted-foreground text-sm py-12 text-center">该时间段内没有数据</CardContent>
      </Card>
    );
  }
  const last = funnel.steps[funnel.steps.length - 1]?.count ?? 0;
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>转化漏斗</CardTitle>
          <CardDescription>
            {`总转化率 ${pct(last / first)}${windowHours ? ` · 归因窗口 ${formatWindow(windowHours)}` : ""}`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-[8rem_1fr_5rem_5rem_5rem_6rem] items-center gap-x-3 gap-y-2 text-sm">
            <span className="text-xs text-muted-foreground">步骤</span>
            <span className="text-xs text-muted-foreground">占第 1 步</span>
            <span className="text-xs text-muted-foreground text-right">人数</span>
            <span className="text-xs text-muted-foreground text-right">较上一步</span>
            <span className="text-xs text-muted-foreground text-right">流失</span>
            <span className="text-xs text-muted-foreground text-right">耗时中位数</span>
            {funnel.steps.map((s, i) => {
              const prev = i > 0 ? funnel.steps[i - 1].count : null;
              const width = Math.min(100, (s.count / first) * 100);
              return (
                <div key={i} className="contents">
                  <span className="truncate" title={s.label}>{s.label}</span>
                  <div
                    data-testid="funnel-bar"
                    role="img"
                    aria-label={`${s.label}：${s.count} 人，占第 1 步 ${pct(s.count / first)}`}
                    className="bg-muted rounded-full h-3 overflow-hidden"
                    title={`占第 1 步 ${pct(s.count / first)}`}
                  >
                    <div className="h-full bg-primary" style={{ width: `${width}%` }} />
                  </div>
                  <span className="text-right tabular-nums">{s.count}</span>
                  <span className="text-right tabular-nums">{prev === null ? "—" : pct(s.rateFromPrev)}</span>
                  <span className="text-right tabular-nums">{prev === null ? "—" : <span>{Math.max(0, prev - s.count)}</span>}</span>
                  <span className="text-right tabular-nums">{formatDuration(s.medianSecFromPrev)}</span>
                </div>
              );
            })}
          </div>
          <div className="mt-4 space-y-1 text-xs text-muted-foreground">
            <p>每人按其在所选时间段内走得最远的一次进入计算</p>
            <p>最近进入的访客可能还没走完（归因窗口未结束），近几天的转化率会偏低</p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>按天趋势</CardTitle>
          <CardDescription>柱高为当日进入第 1 步的人数</CardDescription>
        </CardHeader>
        <CardContent>
          <DailyBars daily={funnel.daily} />
        </CardContent>
      </Card>

      {funnel.groups.length > 0 && <GroupTable funnel={funnel} dimLabel={dimLabel} />}
    </>
  );
}

// 标注每根柱子人数的上限天数；更长的时段柱子太窄，数字会互相压住，改看悬停提示。
const DAILY_COUNT_LABEL_MAX_DAYS = 14;
// 横轴大约保留的日期标签个数。
const DAILY_AXIS_LABELS = 8;

function DailyBars({ daily }: { daily: FunnelResult["daily"] }) {
  if (daily.length === 0) {
    return <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>;
  }
  const peak = Math.max(...daily.map((d) => d.entered));
  const max = Math.max(peak, 1);
  const total = daily.reduce((sum, d) => sum + d.entered, 0);
  const completed = daily.reduce((sum, d) => sum + d.completed, 0);
  const step = Math.ceil(daily.length / DAILY_AXIS_LABELS);
  const showCounts = daily.length <= DAILY_COUNT_LABEL_MAX_DAYS;
  const day = (d: { date: string }) => d.date.slice(5, 10);
  const summary =
    `按天趋势：${day(daily[0])} 至 ${day(daily[daily.length - 1])} 共 ${daily.length} 天，` +
    `进入 ${total} 人，完成 ${completed} 人，单日最高 ${peak} 人`;
  return (
    <div data-testid="daily-chart" role="img" aria-label={summary}>
      <p className="text-xs text-muted-foreground mb-2">{`单日最高 ${peak} 人`}</p>
      {/* 柱区：高度百分比只相对这一行；顶部留白给 14 天以内的人数标注。日期在下面单独一行，不参与柱高。 */}
      <div className={`flex items-end gap-px h-40 ${showCounts ? "pt-5" : ""}`}>
        {daily.map((d) => {
          const rate = d.entered > 0 ? pct(d.completed / d.entered) : "—";
          return (
            <div
              key={d.date}
              className="flex-1 min-w-0 h-full flex items-end"
              title={`${d.date.slice(0, 10)}：进入 ${d.entered} · 完成 ${d.completed} · 转化 ${rate}`}
            >
              <div
                data-testid="daily-bar"
                className="relative w-full bg-primary rounded-t transition-all hover:bg-primary/80"
                style={{ height: `${(d.entered / max) * 100}%`, minHeight: d.entered > 0 ? "4px" : "0" }}
              >
                {showCounts && d.entered > 0 && (
                  <span
                    data-testid="daily-count"
                    className="absolute -top-4 inset-x-0 text-center text-xs leading-none text-muted-foreground"
                  >
                    {d.entered}
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>
      <div className="flex gap-px mt-1 h-4">
        {daily.map((d, i) => (
          <div key={d.date} data-testid="daily-label" className="flex-1 min-w-0 text-[10px] leading-4 text-muted-foreground whitespace-nowrap">
            {i % step === 0 ? day(d) : ""}
          </div>
        ))}
      </div>
    </div>
  );
}

type SortKey = "key" | "rate" | number;

function GroupTable({ funnel, dimLabel }: { funnel: FunnelResult; dimLabel: string }) {
  const [sortKey, setSortKey] = useState<SortKey>(0);
  const [desc, setDesc] = useState(true);

  const rows = useMemo(() => {
    const val = (g: FunnelResult["groups"][number]): number | string => {
      if (sortKey === "key") return g.key;
      if (sortKey === "rate") return rateOf(g.steps);
      return g.steps[sortKey] ?? 0;
    };
    return [...funnel.groups].sort((a, b) => {
      // 合并行不参与比较：无论按哪一列、升序还是降序，都排在最后。
      if ((a.key === OTHER_GROUP) !== (b.key === OTHER_GROUP)) return a.key === OTHER_GROUP ? 1 : -1;
      const x = val(a);
      const y = val(b);
      const c = typeof x === "string" ? x.localeCompare(y as string) : (x as number) - (y as number);
      return desc ? -c : c;
    });
  }, [funnel.groups, sortKey, desc]);

  const click = (k: SortKey) => {
    if (k === sortKey) setDesc(!desc);
    else {
      setSortKey(k);
      setDesc(k !== "key");
    }
  };
  const mark = (k: SortKey) => (k === sortKey ? (desc ? " ↓" : " ↑") : "");
  const ariaSort = (k: SortKey) => (k === sortKey ? (desc ? "descending" : "ascending") : "none");
  const sortButton = (k: SortKey, label: string) => (
    <button type="button" className="cursor-pointer hover:text-foreground" onClick={() => click(k)}>
      {`${label}${mark(k)}`}
    </button>
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>{`按${dimLabel}分组`}</CardTitle>
        <CardDescription>点击列头排序</CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-xs text-muted-foreground">
              <th scope="col" aria-sort={ariaSort("key")} className="text-left font-normal py-2">{sortButton("key", dimLabel)}</th>
              {funnel.steps.map((s, i) => (
                <th key={i} scope="col" aria-sort={ariaSort(i)} className="text-right font-normal py-2">{sortButton(i, s.label)}</th>
              ))}
              <th scope="col" aria-sort={ariaSort("rate")} className="text-right font-normal py-2">{sortButton("rate", "总转化率")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((g) => (
              <tr key={g.key} className="border-t">
                <td className="py-2">{GROUP_KEY_LABELS[g.key] ?? g.key}</td>
                {funnel.steps.map((_, i) => (
                  <td key={i} className="text-right tabular-nums">{g.steps[i] ?? 0}</td>
                ))}
                <td className="text-right tabular-nums">{pct(rateOf(g.steps))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CardContent>
    </Card>
  );
}

function PaidTable({ state }: { state: RetentionState<PaidRetentionResult> }) {
  const { data } = state;
  const rows = data?.rows ?? [];
  return (
    <Card data-testid="retention-paid">
      <CardHeader>
        <CardTitle>付费留存</CardTitle>
        <CardDescription>按首次付费月分群</CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        {state.loading ? (
          <div className="text-muted-foreground text-sm py-8 text-center">加载中…</div>
        ) : state.error ? (
          <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 text-destructive px-4 py-3 text-sm">
            {state.error}
          </div>
        ) : rows.length === 0 ? (
          <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs text-muted-foreground">
                {["首付月", "人数", "M1", "M3", "M6", "M12", "退款数"].map((h, i) => (
                  <th key={h} className={`font-normal py-2 ${i === 0 ? "text-left" : "text-right"}`}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.cohort} className="border-t">
                  <td className="py-2">{r.cohort}</td>
                  <td className="text-right tabular-nums">{r.size}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.retained.m1)}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.retained.m3)}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.retained.m6)}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.retained.m12)}</td>
                  <td className="text-right tabular-nums">{r.refunded}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {data?.note && <p className="text-xs text-muted-foreground mt-3">{data.note}</p>}
      </CardContent>
    </Card>
  );
}

function ActiveTable({ state }: { state: RetentionState<ActiveRetentionResult> }) {
  const { data } = state;
  const rows = data?.rows ?? [];
  return (
    <Card data-testid="retention-active">
      <CardHeader>
        <CardTitle>活跃留存</CardTitle>
        <CardDescription>按首次活跃日分群</CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        {state.loading ? (
          <div className="text-muted-foreground text-sm py-8 text-center">加载中…</div>
        ) : state.error ? (
          <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 text-destructive px-4 py-3 text-sm">
            {state.error}
          </div>
        ) : rows.length === 0 ? (
          <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs text-muted-foreground">
                {["首次活跃日", "人数", "D1", "D7", "D30"].map((h, i) => (
                  <th key={h} className={`font-normal py-2 ${i === 0 ? "text-left" : "text-right"}`}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.cohort} className="border-t">
                  <td className="py-2">{r.cohort}</td>
                  <td className="text-right tabular-nums">{r.size}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.d1)}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.d7)}</td>
                  <td className="text-right tabular-nums">{pctOrDash(r.d30)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {data?.note && <p className="text-xs text-muted-foreground mt-3">{data.note}</p>}
      </CardContent>
    </Card>
  );
}

function errorText(e: unknown): string {
  return getApiErrorMessageZh(e instanceof ApiError ? e.code : 0);
}

function pct(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

function pctOrDash(v: number | null | undefined): string {
  return v === null || v === undefined ? "—" : pct(v);
}

function rateOf(steps: number[]): number {
  const first = steps[0] ?? 0;
  return first > 0 ? (steps[steps.length - 1] ?? 0) / first : 0;
}

// 归因窗口：满 24 小时按天显示（336 小时 → 14 天），不足一天才用小时。
function formatWindow(hours: number): string {
  if (hours < 24) return `${hours} 小时`;
  return `${(hours / 24).toFixed(1).replace(/\.0$/, "")} 天`;
}

function formatDuration(sec: number | null): string {
  if (sec === null || sec === undefined) return "—";
  const units: [number, string][] = [
    [86400, "天"],
    [3600, "小时"],
    [60, "分"],
    [1, "秒"],
  ];
  const [size, name] = units.find(([s]) => sec >= s) ?? units[units.length - 1];
  return `${(sec / size).toFixed(1).replace(/\.0$/, "")} ${name}`;
}

// 日期按 UTC 计算：to = 今天，from = 往前 days-1 天（含今天共 days 天）。
function dateRange(days: number): { from: string; to: string } {
  const now = new Date();
  const to = now.toISOString().slice(0, 10);
  const from = new Date(now.getTime() - (days - 1) * 86400000).toISOString().slice(0, 10);
  return { from, to };
}
