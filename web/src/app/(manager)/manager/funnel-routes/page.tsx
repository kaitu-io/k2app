"use client";

export const dynamic = "force-dynamic";

/**
 * 转化路线：按品牌把所有漏斗路径分成「App 内路线」与「网站路线」两列，一屏看完。
 *
 * 每张卡片就是转化漏斗页的一条路径（同一个接口、同一个口径），只是压缩成步骤清单；
 * 卡片上标出每一步发生在哪（网站 / App / 账户事实），中途跨面的那一跳单独标注，
 * 并原样显示服务端注册表里写的「这一跳靠什么接上」（path.hop）——确定性还是推断，必须一眼可见。
 * 明细（按天、分组）点卡片进转化漏斗页。
 */
import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { api, ApiError, type FunnelPathInfo, type FunnelResult, type FunnelStepWhere } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import type { BrandId } from "@/lib/brands";
import { brandLabel, MANAGER_BRAND_IDS, useManagerBrand } from "@/components/manager/brand";
import { dateRange, formatWindow, FUNNEL_RANGES, hopsOf, pct, WHERE_LABELS } from "@/components/manager/funnel-format";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";

// 同时在途的漏斗查询上限：两个品牌 × 全部路径一次性打过去会让 Center 同时装载太多事件。
const MAX_IN_FLIGHT = 4;

const LANES: { lane: FunnelPathInfo["lane"]; title: string; desc: string }[] = [
  { lane: "app", title: "App 内路线", desc: "从打开 App 开始：激活、App 内购买，以及从 App 跳到网页（支付页 / 官网）之后的部分" },
  { lane: "web", title: "网站路线", desc: "从访问官网开始：网站购买、下载，以及下载之后进入 App 的部分" },
];

const WHERE_STYLES: Record<FunnelStepWhere, string> = {
  web: "bg-sky-500/10 text-sky-700 dark:text-sky-300 border-sky-500/30",
  app: "bg-violet-500/10 text-violet-700 dark:text-violet-300 border-violet-500/30",
  any: "bg-muted text-muted-foreground border-border",
  facts: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border-emerald-500/30",
};

type CardState = { data: FunnelResult | null; error: string | null };

export default function FunnelRoutesPage() {
  const { brandParam } = useManagerBrand();
  const brands = useMemo<readonly BrandId[]>(() => (brandParam ? [brandParam] : MANAGER_BRAND_IDS), [brandParam]);
  const [paths, setPaths] = useState<FunnelPathInfo[]>([]);
  const [pathsError, setPathsError] = useState<string | null>(null);
  const [days, setDays] = useState("30");
  // 推断归因默认关：与转化漏斗页一致，开了之后卡片上会标「含推断」。
  const [inferred, setInferred] = useState(false);
  const [cards, setCards] = useState<Record<string, CardState>>({});

  useEffect(() => {
    let cancelled = false;
    api
      .getFunnelPaths()
      .then((r) => !cancelled && setPaths(r.paths))
      .catch((e) => !cancelled && setPathsError(errorText(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (paths.length === 0) return;
    let cancelled = false;
    setCards({}); // 换了时段 / 品牌 / 推断开关：旧数字不在新请求落地前继续显示
    const { from, to } = dateRange(Number(days));
    const jobs = brands.flatMap((brand) => paths.map((p) => ({ brand, key: p.key })));
    let next = 0;
    const worker = async () => {
      while (!cancelled && next < jobs.length) {
        const { brand, key } = jobs[next++];
        let state: CardState;
        try {
          state = { data: await api.getFunnel(key, { brand, from, to, inferred }), error: null };
        } catch (e) {
          state = { data: null, error: errorText(e) };
        }
        if (!cancelled) setCards((prev) => ({ ...prev, [cardId(brand, key)]: state }));
      }
    };
    for (let i = 0; i < Math.min(MAX_IN_FLIGHT, jobs.length); i++) void worker();
    return () => {
      cancelled = true;
    };
  }, [paths, brands, days, inferred]);

  return (
    <div className="p-6 space-y-6">
      <div className="flex flex-wrap items-center gap-4">
        <Select name="range" value={days} onValueChange={setDays}>
          <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
          <SelectContent>
            {FUNNEL_RANGES.map((r) => (
              <SelectItem key={r.value} value={r.value}>{r.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <label className="flex items-center gap-2 text-sm h-9">
          <Switch aria-label="含推断归因" checked={inferred} onCheckedChange={setInferred} />
          含推断归因
        </label>
      </div>

      <div className="text-xs text-muted-foreground space-y-1">
        <p>每张卡片是一条转化路径：人数按「每人在所选时段内走得最远的一次进入」计，后续步骤须落在归因窗口内；与「转化漏斗」页同一口径，点卡片看按天趋势和分组。</p>
        <p>
          步骤标签表示这一步发生在哪：
          {(Object.keys(WHERE_LABELS) as FunnelStepWhere[]).map((w) => (
            <WhereBadge key={w} where={w} className="ml-1" />
          ))}
          （账户 = 服务端按账户记录的注册 / 付款，与在哪个面完成无关）。标着「跳到…」的那一步换了面，旁边写明它靠什么接上。
        </p>
        <p>最近进入的人可能还没走完（归因窗口未结束），近几天的转化率会偏低。</p>
      </div>

      {pathsError && (
        <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 text-destructive px-4 py-3 text-sm">
          {pathsError}
        </div>
      )}

      {brands.map((brand) => (
        <section key={brand} aria-label={brandLabel(brand)} className="space-y-3">
          <h2 className="text-lg font-semibold">{brandLabel(brand)}</h2>
          <div className="grid gap-6 lg:grid-cols-2">
            {LANES.map(({ lane, title, desc }) => (
              <div key={lane} className="space-y-4" data-testid={`lane-${brand}-${lane}`}>
                <div>
                  <h3 className="font-medium">{title}</h3>
                  <p className="text-xs text-muted-foreground">{desc}</p>
                </div>
                {paths
                  .filter((p) => p.lane === lane)
                  .map((p) => (
                    <RouteCard key={p.key} path={p} state={cards[cardId(brand, p.key)]} />
                  ))}
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function RouteCard({ path, state }: { path: FunnelPathInfo; state: CardState | undefined }) {
  const hops = hopsOf(path);
  const data = state?.data ?? null;
  const first = data?.steps[0]?.count ?? 0;
  const last = data?.steps[data.steps.length - 1]?.count ?? 0;
  return (
    <Card data-testid={`route-${path.key}`} className="gap-3 py-4">
      <CardHeader className="px-4">
        <CardTitle className="text-base">
          <Link href={`/manager/funnels?path=${encodeURIComponent(path.key)}`} className="hover:underline">
            {path.title}
          </Link>
        </CardTitle>
        <CardDescription>{path.question}</CardDescription>
      </CardHeader>
      <CardContent className="px-4 space-y-3">
        {state === undefined ? (
          <p className="text-sm text-muted-foreground">加载中…</p>
        ) : state.error ? (
          <p role="alert" className="text-sm text-destructive">{state.error}</p>
        ) : (
          <>
            <ol className="space-y-1.5 text-sm">
              {path.steps.map((label, i) => {
                const step = data?.steps[i];
                const count = step?.count ?? 0;
                const hop = hops[i];
                return (
                  <li key={i} className="space-y-1">
                    {hop && (
                      <div className="text-xs font-medium text-amber-700 dark:text-amber-400" data-testid="hop">
                        {`↪ 跳到${WHERE_LABELS[hop]}`}
                      </div>
                    )}
                    <div className="grid grid-cols-[auto_1fr_auto_4.5rem] items-center gap-2">
                      <WhereBadge where={path.stepWhere[i]} />
                      <span className="truncate" title={label}>{label}</span>
                      <span className="tabular-nums text-right">{count}</span>
                      <span className="tabular-nums text-right text-xs text-muted-foreground">
                        {i === 0 ? "" : first > 0 ? pct(step?.rateFromPrev ?? 0) : "—"}
                      </span>
                    </div>
                    <div className="bg-muted rounded-full h-1.5 overflow-hidden" aria-hidden>
                      <div className="h-full bg-primary" style={{ width: `${first > 0 ? Math.min(100, (count / first) * 100) : 0}%` }} />
                    </div>
                  </li>
                );
              })}
            </ol>
            <p className="text-xs text-muted-foreground">
              {first > 0 ? `总转化率 ${pct(last / first)}` : "该时间段内没有人进入这条路径"}
              {` · 归因窗口 ${formatWindow(path.windowHours)}`}
              {data?.inferred && ` · 含推断（${data.inferred.method}，并入 ${data.inferred.applied} 条）`}
            </p>
            {path.hop && (
              <p className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs leading-relaxed">
                <span className="font-medium">跨端说明：</span>
                {path.hop}
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function WhereBadge({ where, className = "" }: { where: FunnelStepWhere; className?: string }) {
  return (
    <span className={`inline-flex items-center rounded border px-1.5 py-0.5 text-[10px] leading-none ${WHERE_STYLES[where]} ${className}`}>
      {WHERE_LABELS[where]}
    </span>
  );
}

function cardId(brand: BrandId, key: string): string {
  return `${brand}/${key}`;
}

function errorText(e: unknown): string {
  return getApiErrorMessageZh(e instanceof ApiError ? e.code : 0);
}
