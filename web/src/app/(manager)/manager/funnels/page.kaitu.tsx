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
  const [retentionError, setRetentionError] = useState<string | null>(null);

  const [paid, setPaid] = useState<PaidRetentionResult | null>(null);
  const [active, setActive] = useState<ActiveRetentionResult | null>(null);
  const [retentionLoading, setRetentionLoading] = useState(false);

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
    setPaid(null);
    setActive(null);
    setRetentionLoading(true);
    setRetentionError(null);
    Promise.all([
      api.getRetention({ brand: brandParam, metric: "paid" }),
      api.getRetention({ brand: brandParam, metric: "active" }),
    ])
      .then(([p, a]) => {
        if (cancelled) return;
        setPaid(p);
        setActive(a);
      })
      .catch((e) => !cancelled && setRetentionError(errorText(e)))
      .finally(() => !cancelled && setRetentionLoading(false));
    return () => {
      cancelled = true;
    };
  }, [tab, brandParam]);

  const current = paths.find((p) => p.key === pathKey);

  return (
    <div className="p-6 space-y-6">
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

      {(tab === "funnel" ? funnelError : retentionError) && (
        <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 text-destructive px-4 py-3 text-sm">
          {tab === "funnel" ? funnelError : retentionError}
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
          {retentionLoading ? (
            <div className="text-muted-foreground text-sm py-8 text-center">加载中…</div>
          ) : (
            <>
              <PaidTable data={paid} />
              <ActiveTable data={active} />
            </>
          )}
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
            {`总转化率 ${pct(last / first)}${windowHours ? ` · 归因窗口 ${windowHours} 小时` : ""}`}
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
                  <div className="bg-muted rounded-full h-3 overflow-hidden" title={`占第 1 步 ${pct(s.count / first)}`}>
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

function DailyBars({ daily }: { daily: FunnelResult["daily"] }) {
  if (daily.length === 0) {
    return <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>;
  }
  const max = Math.max(...daily.map((d) => d.entered), 1);
  return (
    <div className="flex items-end gap-1 h-40 overflow-x-auto">
      {daily.map((d) => {
        const height = (d.entered / max) * 100;
        const rate = d.entered > 0 ? pct(d.completed / d.entered) : "—";
        return (
          <div
            key={d.date}
            className="flex-shrink-0 flex flex-col items-center gap-1 h-full justify-end"
            style={{ width: daily.length > 30 ? "12px" : "24px" }}
            title={`${d.date.slice(0, 10)}：进入 ${d.entered} · 完成 ${d.completed} · 转化 ${rate}`}
          >
            <div className="text-xs text-muted-foreground">{d.entered > 0 ? d.entered : ""}</div>
            <div
              className="w-full bg-primary rounded-t transition-all hover:bg-primary/80"
              style={{ height: `${height}%`, minHeight: d.entered > 0 ? "4px" : "0" }}
            />
            <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">{d.date.slice(5, 10)}</div>
          </div>
        );
      })}
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
              <th className="text-left font-normal py-2 cursor-pointer" onClick={() => click("key")}>{`${dimLabel}${mark("key")}`}</th>
              {funnel.steps.map((s, i) => (
                <th key={i} className="text-right font-normal py-2 cursor-pointer" onClick={() => click(i)}>{`${s.label}${mark(i)}`}</th>
              ))}
              <th className="text-right font-normal py-2 cursor-pointer" onClick={() => click("rate")}>{`总转化率${mark("rate")}`}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((g) => (
              <tr key={g.key} className="border-t">
                <td className="py-2">{g.key}</td>
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

function PaidTable({ data }: { data: PaidRetentionResult | null }) {
  const rows = data?.rows ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>付费留存</CardTitle>
        <CardDescription>按首次付费月分群</CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        {rows.length === 0 ? (
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

function ActiveTable({ data }: { data: ActiveRetentionResult | null }) {
  const rows = data?.rows ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>活跃留存</CardTitle>
        <CardDescription>按首次活跃日分群</CardDescription>
      </CardHeader>
      <CardContent className="overflow-x-auto">
        {rows.length === 0 ? (
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
