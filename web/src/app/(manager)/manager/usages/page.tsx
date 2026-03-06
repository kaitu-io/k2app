"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { api, UsageOverviewResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";

export default function UsagesPage() {
  const [data, setData] = useState<UsageOverviewResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState("30d");
  const [os, setOs] = useState("all");
  const [tab, setTab] = useState("devices");

  useEffect(() => {
    loadData();
  }, [range, os]);

  async function loadData() {
    setLoading(true);
    try {
      const result = await api.getUsageOverview({ range, os: os === "all" ? undefined : os });
      setData(result);
    } catch (error) {
      console.error("Failed to load usage data:", error);
    } finally {
      setLoading(false);
    }
  }

  if (loading && !data) {
    return (
      <div className="p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-1/4"></div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-48 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">使用统计</h1>
        <div className="flex gap-2">
          <Select value={os} onValueChange={setOs}>
            <SelectTrigger className="w-32">
              <SelectValue placeholder="全部平台" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部平台</SelectItem>
              <SelectItem value="macos">macOS</SelectItem>
              <SelectItem value="windows">Windows</SelectItem>
              <SelectItem value="linux">Linux</SelectItem>
              <SelectItem value="ios">iOS</SelectItem>
              <SelectItem value="android">Android</SelectItem>
            </SelectContent>
          </Select>
          <Select value={range} onValueChange={setRange}>
            <SelectTrigger className="w-24">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="7d">7天</SelectItem>
              <SelectItem value="30d">30天</SelectItem>
              <SelectItem value="90d">90天</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>日活设备 (今日)</CardDescription>
            <CardTitle className="text-3xl">
              {data?.activeDevices?.length ? data.activeDevices[data.activeDevices.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>今日连接数</CardDescription>
            <CardTitle className="text-3xl">
              {data?.connections?.length ? data.connections[data.connections.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>活跃节点数</CardDescription>
            <CardTitle className="text-3xl">{data?.nodeUsage?.length ?? 0}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>k2s 今日下载</CardDescription>
            <CardTitle className="text-3xl">
              {data?.k2sDownloads?.length ? data.k2sDownloads[data.k2sDownloads.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Tabs value={tab} onValueChange={setTab} className="space-y-6">
        <TabsList>
          <TabsTrigger value="devices">活跃设备</TabsTrigger>
          <TabsTrigger value="connections">连接趋势</TabsTrigger>
          <TabsTrigger value="nodes">节点分布</TabsTrigger>
          <TabsTrigger value="downloads">k2s 下载</TabsTrigger>
        </TabsList>

        <TabsContent value="devices">
          <Card>
            <CardHeader>
              <CardTitle>日活跃设备趋势</CardTitle>
              <CardDescription>每日唯一设备数 (DAU)</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.activeDevices || []} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="connections">
          <Card>
            <CardHeader>
              <CardTitle>每日连接数趋势</CardTitle>
              <CardDescription>每日 VPN 连接次数</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.connections || []} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="nodes">
          <Card>
            <CardHeader>
              <CardTitle>节点使用分布</CardTitle>
              <CardDescription>按连接次数排名</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {data?.nodeUsage?.map((node, i) => {
                  const maxCount = data.nodeUsage[0]?.count || 1;
                  const pct = ((node.count / maxCount) * 100).toFixed(0);
                  return (
                    <div key={i} className="flex items-center gap-3">
                      <span className="font-mono text-sm w-32 truncate">
                        {node.nodeType === 'self-hosted' ? 'Self-Hosted' : node.nodeIpv4}
                      </span>
                      <Badge variant="outline" className="w-20 justify-center">
                        {node.nodeType}
                      </Badge>
                      <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                        <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
                      </div>
                      <span className="text-sm text-muted-foreground w-16 text-right">{node.count}</span>
                    </div>
                  );
                })}
                {(!data?.nodeUsage || data.nodeUsage.length === 0) && (
                  <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="downloads">
          <Card>
            <CardHeader>
              <CardTitle>k2s 每日下载趋势</CardTitle>
              <CardDescription>每日唯一 IP 下载数</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.k2sDownloads || []} />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function BarChart({ data }: { data: { date: string; count: number }[] }) {
  if (data.length === 0) {
    return <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>;
  }

  const maxCount = Math.max(...data.map(d => d.count), 1);

  return (
    <div className="flex items-end gap-1 h-40 overflow-x-auto">
      {data.map((item) => {
        const height = (item.count / maxCount) * 100;
        return (
          <div
            key={item.date}
            className="flex-shrink-0 flex flex-col items-center gap-1"
            style={{ width: data.length > 30 ? '12px' : '24px' }}
            title={`${item.date}: ${item.count}`}
          >
            <div className="text-xs text-muted-foreground">{item.count > 0 ? item.count : ''}</div>
            <div
              className="w-full bg-primary rounded-t transition-all hover:bg-primary/80"
              style={{ height: `${height}%`, minHeight: item.count > 0 ? '4px' : '0' }}
            />
            <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
              {item.date.slice(5)}
            </div>
          </div>
        );
      })}
    </div>
  );
}
