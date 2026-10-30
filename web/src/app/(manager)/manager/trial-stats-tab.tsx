"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  api,
  TrialChainDetail,
  TrialChainState,
  TrialCohortResponse,
  TrialCohortRow,
} from "@/lib/api";
import type { BrandId } from "@/lib/brands";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const stateLabels: Record<TrialChainState, string> = {
  in_trial_renew_on: "试用中·续费开",
  in_trial_renew_off: "试用中·已关续费",
  awaiting_charge: "到期待扣费",
  billing_retry: "扣费重试",
  converted: "已转付费",
  ended_unpaid: "结束未付费",
  revoked: "退款/撤销",
};

export function formatTrialRate(rate: number | null | undefined): string {
  return rate == null ? "-" : `${(rate * 100).toFixed(1)}%`;
}

function formatCents(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

function formatDate(ts: number): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function Metric({ label, value, hint }: { label: string; value: string | number; hint?: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-3xl">{value}</CardTitle>
      </CardHeader>
      {hint && (
        <CardContent>
          <div className="text-sm text-muted-foreground">{hint}</div>
        </CardContent>
      )}
    </Card>
  );
}

function CohortCells({ row }: { row: TrialCohortRow }) {
  return (
    <>
      <TableCell className="text-right tabular-nums">{row.started}</TableCell>
      <TableCell className="text-right tabular-nums">
        {row.inTrialRenewOn + row.inTrialRenewOff}
        {row.inTrialRenewOff > 0 && (
          <span className="text-muted-foreground"> (关 {row.inTrialRenewOff})</span>
        )}
      </TableCell>
      <TableCell className="text-right tabular-nums">{row.awaitingCharge + row.billingRetry}</TableCell>
      <TableCell className="text-right tabular-nums">{row.converted}</TableCell>
      <TableCell className="text-right tabular-nums">{row.endedUnpaid}</TableCell>
      <TableCell className="text-right tabular-nums">{row.revoked}</TableCell>
      <TableCell className="text-right tabular-nums">{formatTrialRate(row.conversionRate)}</TableCell>
      <TableCell className="text-right tabular-nums">
        {formatCents(row.paidAmount)}
        {row.refundedAmount > 0 && (
          <span className="text-muted-foreground"> (退 {formatCents(row.refundedAmount)})</span>
        )}
      </TableCell>
    </>
  );
}

export function TrialStatsTab({ brand }: { brand?: BrandId }) {
  const [days, setDays] = useState(30);
  const [stats, setStats] = useState<TrialCohortResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [cohort, setCohort] = useState<string | null>(null);
  const [chains, setChains] = useState<TrialChainDetail[] | null>(null);

  useEffect(() => {
    setLoading(true);
    setCohort(null);
    api
      .getTrialCohorts({ brand, days })
      .then(setStats)
      .catch((err) => {
        console.error("Failed to load trial cohorts:", err);
        setStats(null);
      })
      .finally(() => setLoading(false));
  }, [brand, days]);

  useEffect(() => {
    setChains(null);
    if (!cohort) return;
    api
      .getTrialCohorts({ brand, days, cohort })
      .then((r) => setChains(r.chains ?? []))
      .catch((err) => console.error("Failed to load trial cohort chains:", err));
  }, [brand, days, cohort]);

  if (loading && !stats) {
    return <div className="text-muted-foreground text-center py-12">加载中...</div>;
  }

  if (!stats) {
    return <div className="text-muted-foreground text-center py-12">加载失败（需要营销权限）</div>;
  }

  const t = stats.totals;
  const rows = [...stats.rows].reverse();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">Apple 免费试用，按试用开始日（UTC）分批，不含沙盒</p>
        <Select value={String(days)} onValueChange={(v) => setDays(Number(v))}>
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7">最近 7 天</SelectItem>
            <SelectItem value="30">最近 30 天</SelectItem>
            <SelectItem value="90">最近 90 天</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 2xl:grid-cols-6 gap-4">
        <Metric label="开始试用" value={t.started} />
        <Metric
          label="试用中"
          value={t.inTrialRenewOn + t.inTrialRenewOff}
          hint={`续费开 ${t.inTrialRenewOn} | 已关 ${t.inTrialRenewOff}`}
        />
        <Metric
          label="待结算"
          value={t.awaitingCharge + t.billingRetry}
          hint={`到期待扣费 ${t.awaitingCharge} | 扣费重试 ${t.billingRetry}`}
        />
        <Metric label="已转付费" value={t.converted} hint={`付款 ${formatCents(t.paidAmount)}`} />
        <Metric
          label="结束未付费"
          value={t.endedUnpaid}
          hint={`退款/撤销 ${t.revoked}${t.refundedAmount > 0 ? ` (${formatCents(t.refundedAmount)})` : ""}`}
        />
        <Metric
          label="试用转化率"
          value={formatTrialRate(t.conversionRate)}
          hint={`已结算 ${t.settled} 条中付费 ${t.settledConverted}`}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>按开始日</CardTitle>
          <CardDescription>点一行查看这批试用的用户名单</CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>开始日</TableHead>
                <TableHead className="text-right">开始</TableHead>
                <TableHead className="text-right">试用中</TableHead>
                <TableHead className="text-right">待结算</TableHead>
                <TableHead className="text-right">已付费</TableHead>
                <TableHead className="text-right">未付费</TableHead>
                <TableHead className="text-right">退款</TableHead>
                <TableHead className="text-right">转化率</TableHead>
                <TableHead className="text-right">付款</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={9} className="text-center text-muted-foreground">
                    这段时间没有试用
                  </TableCell>
                </TableRow>
              )}
              {rows.map((row) => (
                <TableRow
                  key={row.cohort}
                  className={`cursor-pointer ${cohort === row.cohort ? "bg-muted" : ""}`}
                  onClick={() => setCohort(cohort === row.cohort ? null : row.cohort)}
                >
                  <TableCell>{row.cohort}</TableCell>
                  <CohortCells row={row} />
                </TableRow>
              ))}
              {rows.length > 0 && (
                <TableRow className="font-medium">
                  <TableCell>合计</TableCell>
                  <CohortCells row={t} />
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {cohort && (
        <Card>
          <CardHeader>
            <CardTitle>{cohort} 开始的试用</CardTitle>
          </CardHeader>
          <CardContent>
            {chains === null ? (
              <div className="text-muted-foreground text-center py-6">加载中...</div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>开始</TableHead>
                    <TableHead>试用到期</TableHead>
                    <TableHead>套餐</TableHead>
                    <TableHead className="text-right">付款</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {chains.map((ch) => (
                    <TableRow key={`${ch.userId}-${ch.startedAt}`}>
                      <TableCell className="font-mono text-xs">
                        <Link className="hover:underline" href={`/manager/users/detail?uuid=${ch.userUuid}`}>
                          {ch.userUuid}
                        </Link>
                      </TableCell>
                      <TableCell>
                        <Badge variant={ch.state === "converted" ? "default" : "outline"}>
                          {stateLabels[ch.state] ?? ch.state}
                        </Badge>
                      </TableCell>
                      <TableCell>{formatDate(ch.startedAt)}</TableCell>
                      <TableCell>{formatDate(ch.trialEndsAt)}</TableCell>
                      <TableCell>{ch.planTitle || "-"}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {ch.paidAmount > 0 ? formatCents(ch.paidAmount) : "-"}
                        {ch.refundedAmount > 0 && (
                          <span className="text-muted-foreground"> (退 {formatCents(ch.refundedAmount)})</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      <p className="text-xs text-muted-foreground">{stats.note}</p>
    </div>
  );
}
