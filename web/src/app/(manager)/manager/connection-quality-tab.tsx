"use client";

import { useEffect, useState } from "react";
import { api, ConnectionRatingStatisticsResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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

function formatRate(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

export function ConnectionQualityTab() {
  const [period, setPeriod] = useState<"7d" | "30d" | "90d">("30d");
  const [stats, setStats] = useState<ConnectionRatingStatisticsResponse | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .getConnectionRatingStatistics(period)
      .then(setStats)
      .catch((err) => console.error("Failed to load rating stats:", err))
      .finally(() => setLoading(false));
  }, [period]);

  if (loading && !stats) {
    return <div className="text-muted-foreground text-center py-12">加载中...</div>;
  }

  if (!stats) {
    return <div className="text-muted-foreground text-center py-12">暂无数据</div>;
  }

  return (
    <div className="space-y-6">
      {/* Period selector */}
      <div className="flex justify-end">
        <Select value={period} onValueChange={(v) => setPeriod(v as "7d" | "30d" | "90d")}>
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7d">最近 7 天</SelectItem>
            <SelectItem value="30d">最近 30 天</SelectItem>
            <SelectItem value="90d">最近 90 天</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">总评价</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.summary.total}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">好评率</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatRate(stats.summary.goodRate)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">好评</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{stats.summary.good}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">差评</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.summary.bad}</div>
          </CardContent>
        </Card>
      </div>

      {/* Trend — hand-rolled stacked bar chart */}
      <Card>
        <CardHeader>
          <CardTitle>好评率趋势</CardTitle>
        </CardHeader>
        <CardContent>
          {stats.trend.length === 0 ? (
            <div className="text-muted-foreground text-center py-8">暂无数据</div>
          ) : (
            <div className="flex items-end gap-1 h-48 overflow-x-auto">
              {stats.trend.map((item) => {
                const goodPct = item.total > 0 ? (item.good / item.total) * 100 : 0;
                const badPct = item.total > 0 ? (item.bad / item.total) * 100 : 0;
                return (
                  <div
                    key={item.date}
                    className="flex-shrink-0 flex flex-col items-center gap-1"
                    style={{ width: stats.trend.length > 30 ? '12px' : '24px' }}
                    title={`${item.date}: ${formatRate(item.goodRate)} (${item.good}/${item.total})`}
                  >
                    <div className="text-xs text-muted-foreground">
                      {item.total > 0 ? formatRate(item.goodRate) : ''}
                    </div>
                    <div className="w-full flex flex-col justify-end" style={{ height: '120px' }}>
                      <div
                        className="w-full bg-red-500 rounded-t"
                        style={{ height: `${badPct}%`, minHeight: item.bad > 0 ? '2px' : '0' }}
                      />
                      <div
                        className="w-full bg-green-500"
                        style={{ height: `${goodPct}%`, minHeight: item.good > 0 ? '2px' : '0' }}
                      />
                    </div>
                    <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
                      {item.date.slice(5, 10)}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* By Server */}
      <Card>
        <CardHeader>
          <CardTitle>按服务器</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>服务器</TableHead>
                <TableHead>地区</TableHead>
                <TableHead className="text-right">好评</TableHead>
                <TableHead className="text-right">差评</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byServer.map((item) => (
                <TableRow key={item.domain}>
                  <TableCell>{item.name || item.domain}</TableCell>
                  <TableCell>{item.country}</TableCell>
                  <TableCell className="text-right">{item.good}</TableCell>
                  <TableCell className="text-right">{item.bad}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By ISP */}
      <Card>
        <CardHeader>
          <CardTitle>按运营商</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>运营商</TableHead>
                <TableHead>国家</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byISP.map((item) => (
                <TableRow key={item.isp}>
                  <TableCell>{item.isp}</TableCell>
                  <TableCell>{item.country}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By Platform */}
      <Card>
        <CardHeader>
          <CardTitle>按平台与版本</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>系统</TableHead>
                <TableHead>版本</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byPlatform.map((item, i) => (
                <TableRow key={`${item.os}-${item.appVersion}-${i}`}>
                  <TableCell>{item.os}</TableCell>
                  <TableCell>{item.appVersion}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By User */}
      <Card>
        <CardHeader>
          <CardTitle>按用户 (低好评率 Top 50，最少 3 条评价)</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户</TableHead>
                <TableHead className="text-right">好评</TableHead>
                <TableHead className="text-right">差评</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byUser.map((item) => (
                <TableRow key={item.userId}>
                  <TableCell>{item.email}</TableCell>
                  <TableCell className="text-right">{item.good}</TableCell>
                  <TableCell className="text-right">{item.bad}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
