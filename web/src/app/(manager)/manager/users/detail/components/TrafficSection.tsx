"use client";

import { useEffect, useState } from "react";
import { api, TrafficUserDetailResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

function formatBytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GB`;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

function recentMonths(count: number): string[] {
  const now = new Date();
  const anchor = now.getFullYear() * 12 + now.getMonth();
  return Array.from({ length: count }, (_, i) => {
    const m = anchor - i;
    return `${Math.floor(m / 12)}-${String((m % 12) + 1).padStart(2, "0")}`;
  });
}

export function TrafficSection({ uuid }: { uuid: string }) {
  const [data, setData] = useState<TrafficUserDetailResponse | null>(null);
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7));
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api.getTrafficUserDetail({ uuid, month })
      .then((r) => { if (!cancelled) setData(r); })
      .catch((e) => console.error("Failed to load user traffic:", e))
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [uuid, month]);

  const maxDaily = Math.max(...(data?.daily?.map((d) => d.bytes) ?? [0]), 1);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle>{"流量（"}{formatBytes(data?.totalBytes ?? 0)}{"）"}</CardTitle>
          <CardDescription>自然月累计 · Asia/Shanghai</CardDescription>
        </div>
        <Select value={month} onValueChange={setMonth}>
          <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            {recentMonths(6).map((m) => (
              <SelectItem key={m} value={m}>{m}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </CardHeader>
      <CardContent className="space-y-6">
        {loading && <div className="text-muted-foreground text-sm py-4 text-center">加载中…</div>}
        {!loading && (
          <>
            <div className="flex items-end gap-1 h-32 overflow-x-auto">
              {data?.daily?.map((d) => (
                <div key={d.date} className="flex-shrink-0 flex flex-col items-center gap-1 w-6"
                  title={`${d.date}: ${formatBytes(d.bytes)}`}>
                  <div className="w-full bg-primary rounded-t"
                    style={{ height: `${(d.bytes / maxDaily) * 100}%`, minHeight: d.bytes > 0 ? "4px" : "0" }} />
                  <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
                    {d.date.slice(8, 10)}
                  </div>
                </div>
              ))}
              {(!data?.daily || data.daily.length === 0) && (
                <div className="text-muted-foreground text-sm py-4 w-full text-center">暂无数据</div>
              )}
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <div className="text-sm font-medium mb-2">按设备</div>
                {data?.devices?.map((d) => (
                  <div key={d.key} className="flex justify-between text-sm py-1">
                    <span className="font-mono truncate mr-2">{d.key || "未识别"}</span>
                    <span className="text-muted-foreground">{formatBytes(d.bytes)}</span>
                  </div>
                ))}
                {(!data?.devices || data.devices.length === 0) && (
                  <div className="text-muted-foreground text-sm py-2">暂无数据</div>
                )}
              </div>
              <div>
                <div className="text-sm font-medium mb-2">按节点</div>
                {data?.nodes?.map((n) => (
                  <div key={n.key} className="flex justify-between text-sm py-1">
                    <span className="font-mono">{n.key}</span>
                    <span className="text-muted-foreground">{formatBytes(n.bytes)}</span>
                  </div>
                ))}
                {(!data?.nodes || data.nodes.length === 0) && (
                  <div className="text-muted-foreground text-sm py-2">暂无数据</div>
                )}
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
