"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import {
  api,
  DeviceStatisticsResponse,
  ActiveDeviceItem,
  UserStatisticsResponse,
  OrderStatisticsResponse
} from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

// Platform display names and icons
const platformInfo: Record<string, { name: string; icon: string; color: string }> = {
  darwin: { name: "macOS", icon: "", color: "bg-gray-500" },
  windows: { name: "Windows", icon: "", color: "bg-blue-500" },
  linux: { name: "Linux", icon: "", color: "bg-orange-500" },
  ios: { name: "iOS", icon: "", color: "bg-purple-500" },
  android: { name: "Android", icon: "", color: "bg-green-500" },
  unknown: { name: "未知", icon: "?", color: "bg-red-500" },
};

function getPlatformInfo(platform: string) {
  return platformInfo[platform] || platformInfo.unknown;
}

function formatTimestamp(ts: number): string {
  if (!ts) return "-";
  const date = new Date(ts * 1000);
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatTimeAgo(ts: number): string {
  if (!ts) return "-";
  const now = Date.now() / 1000;
  const diff = now - ts;

  if (diff < 60) return "刚刚";
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`;
  return `${Math.floor(diff / 86400)} 天前`;
}

function formatCurrency(cents: number): string {
  return `¥${(cents / 100).toFixed(2)}`;
}

function formatPercentage(value: number): string {
  return `${value.toFixed(1)}%`;
}

export default function ManagerDashboardPage() {
  const [deviceStats, setDeviceStats] = useState<DeviceStatisticsResponse | null>(null);
  const [userStats, setUserStats] = useState<UserStatisticsResponse | null>(null);
  const [orderStats, setOrderStats] = useState<OrderStatisticsResponse | null>(null);
  const [activeDevices, setActiveDevices] = useState<ActiveDeviceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [activePeriod, setActivePeriod] = useState<"24h" | "7d" | "30d">("7d");
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [activeTab, setActiveTab] = useState("overview");

  useEffect(() => {
    loadData();
  }, []);

  async function loadData() {
    setLoading(true);
    try {
      const [deviceData, userData, orderData] = await Promise.all([
        api.getDeviceStatistics(),
        api.getUserStatistics(),
        api.getOrderStatistics(),
      ]);
      setDeviceStats(deviceData);
      setUserStats(userData);
      setOrderStats(orderData);
    } catch (error) {
      console.error("Failed to load statistics:", error);
    } finally {
      setLoading(false);
    }
  }

  async function loadActiveDevices() {
    try {
      const response = await api.getActiveDevices({
        page,
        pageSize: 10,
        period: activePeriod,
      });
      setActiveDevices(response.items);
      setTotalPages(Math.ceil(response.pagination.total / response.pagination.pageSize));
    } catch (error) {
      console.error("Failed to load active devices:", error);
    }
  }

  useEffect(() => {
    loadActiveDevices();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activePeriod, page]);

  if (loading) {
    return (
      <div className="p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-1/4"></div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-32 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">数据统计</h1>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="users">用户统计</TabsTrigger>
          <TabsTrigger value="orders">订单统计</TabsTrigger>
          <TabsTrigger value="devices">设备统计</TabsTrigger>
        </TabsList>

        {/* Overview Tab */}
        <TabsContent value="overview" className="space-y-6">
          {/* Key Metrics */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总用户数</CardDescription>
                <CardTitle className="text-3xl">{userStats?.totalUsers ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  付费: {userStats?.paidUsers ?? 0} | 免费: {userStats?.freeUsers ?? 0}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>活跃会员</CardDescription>
                <CardTitle className="text-3xl">{userStats?.activePro ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  已过期: {userStats?.expiredPro ?? 0}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总收入</CardDescription>
                <CardTitle className="text-3xl">{formatCurrency(orderStats?.totalRevenue ?? 0)}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  30天: {formatCurrency(orderStats?.revenue30d ?? 0)}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>付费订单</CardDescription>
                <CardTitle className="text-3xl">{orderStats?.paidOrders ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  转化率: {formatPercentage(orderStats?.conversionRate ?? 0)}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Growth Metrics */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>新增用户</CardTitle>
                <CardDescription>不同时间段的用户增长</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">24小时</span>
                    <Badge variant="outline" className="text-lg px-3">{userStats?.new24h ?? 0}</Badge>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">7天</span>
                    <Badge variant="outline" className="text-lg px-3">{userStats?.new7d ?? 0}</Badge>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">30天</span>
                    <Badge variant="outline" className="text-lg px-3">{userStats?.new30d ?? 0}</Badge>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>收入趋势</CardTitle>
                <CardDescription>不同时间段的收入情况</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">24小时</span>
                    <div className="text-right">
                      <div className="font-medium">{formatCurrency(orderStats?.revenue24h ?? 0)}</div>
                      <div className="text-xs text-muted-foreground">{orderStats?.orders24h ?? 0} 单</div>
                    </div>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">7天</span>
                    <div className="text-right">
                      <div className="font-medium">{formatCurrency(orderStats?.revenue7d ?? 0)}</div>
                      <div className="text-xs text-muted-foreground">{orderStats?.orders7d ?? 0} 单</div>
                    </div>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">30天</span>
                    <div className="text-right">
                      <div className="font-medium">{formatCurrency(orderStats?.revenue30d ?? 0)}</div>
                      <div className="text-xs text-muted-foreground">{orderStats?.orders30d ?? 0} 单</div>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Device Quick Stats */}
          <Card>
            <CardHeader>
              <CardTitle>设备概览</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="text-center">
                  <div className="text-2xl font-bold">{deviceStats?.totalDevices ?? 0}</div>
                  <div className="text-sm text-muted-foreground">总设备</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold">{deviceStats?.desktopDevices ?? 0}</div>
                  <div className="text-sm text-muted-foreground">桌面</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold">{deviceStats?.mobileDevices ?? 0}</div>
                  <div className="text-sm text-muted-foreground">移动</div>
                </div>
                <div className="text-center">
                  <div className="text-2xl font-bold">{deviceStats?.active7d ?? 0}</div>
                  <div className="text-sm text-muted-foreground">7天活跃</div>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Users Tab */}
        <TabsContent value="users" className="space-y-6">
          {/* User Summary Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总用户数</CardDescription>
                <CardTitle className="text-3xl">{userStats?.totalUsers ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  分销商: {userStats?.totalRetailers ?? 0}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>付费用户</CardDescription>
                <CardTitle className="text-3xl">{userStats?.paidUsers ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  占比: {userStats?.totalUsers ? formatPercentage((userStats.paidUsers / userStats.totalUsers) * 100) : '0%'}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>活跃会员</CardDescription>
                <CardTitle className="text-3xl">{userStats?.activePro ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  有效期内的Pro用户
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>过期会员</CardDescription>
                <CardTitle className="text-3xl">{userStats?.expiredPro ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  曾购买但已过期
                </div>
              </CardContent>
            </Card>
          </div>

          {/* User Growth */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>用户增长</CardTitle>
                <CardDescription>不同时间段的新增用户数</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  <div className="flex items-center justify-between py-2 border-b">
                    <span>过去24小时</span>
                    <Badge className="bg-green-500">{userStats?.new24h ?? 0}</Badge>
                  </div>
                  <div className="flex items-center justify-between py-2 border-b">
                    <span>过去7天</span>
                    <Badge className="bg-blue-500">{userStats?.new7d ?? 0}</Badge>
                  </div>
                  <div className="flex items-center justify-between py-2">
                    <span>过去30天</span>
                    <Badge className="bg-purple-500">{userStats?.new30d ?? 0}</Badge>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>用户状态分布</CardTitle>
                <CardDescription>按会员状态分类</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {[
                    { label: "活跃会员", count: userStats?.activePro ?? 0, color: "bg-green-500" },
                    { label: "过期会员", count: userStats?.expiredPro ?? 0, color: "bg-yellow-500" },
                    { label: "从未购买", count: userStats?.neverHadPro ?? 0, color: "bg-gray-400" },
                  ].map((item) => {
                    const total = userStats?.totalUsers ?? 1;
                    const percentage = total > 0 ? ((item.count / total) * 100).toFixed(1) : 0;
                    return (
                      <div key={item.label} className="flex items-center gap-3">
                        <Badge variant="outline" className={`${item.color} text-white min-w-[100px] justify-center`}>
                          {item.label}
                        </Badge>
                        <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                          <div
                            className={`h-full ${item.color}`}
                            style={{ width: `${percentage}%` }}
                          />
                        </div>
                        <span className="text-sm text-muted-foreground w-24 text-right">
                          {item.count} ({percentage}%)
                        </span>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Monthly Registration Trend */}
          {userStats?.byRegistrationPeriod && userStats.byRegistrationPeriod.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>月度注册趋势</CardTitle>
                <CardDescription>近6个月的用户注册数量</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex items-end gap-2 h-40">
                  {userStats.byRegistrationPeriod.map((item) => {
                    const maxCount = Math.max(...userStats.byRegistrationPeriod.map(p => p.count));
                    const height = maxCount > 0 ? (item.count / maxCount) * 100 : 0;
                    return (
                      <div key={item.period} className="flex-1 flex flex-col items-center gap-1">
                        <div className="text-xs text-muted-foreground">{item.count}</div>
                        <div
                          className="w-full bg-primary rounded-t transition-all"
                          style={{ height: `${height}%`, minHeight: item.count > 0 ? '4px' : '0' }}
                        />
                        <div className="text-xs text-muted-foreground">{item.period.slice(5)}</div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        {/* Orders Tab */}
        <TabsContent value="orders" className="space-y-6">
          {/* Order Summary Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总订单数</CardDescription>
                <CardTitle className="text-3xl">{orderStats?.totalOrders ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  待付款: {orderStats?.unpaidOrders ?? 0}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>已付款订单</CardDescription>
                <CardTitle className="text-3xl">{orderStats?.paidOrders ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  转化率: {formatPercentage(orderStats?.conversionRate ?? 0)}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总收入</CardDescription>
                <CardTitle className="text-3xl">{formatCurrency(orderStats?.totalRevenue ?? 0)}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  来自所有付费订单
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>平均客单价</CardDescription>
                <CardTitle className="text-3xl">{formatCurrency(orderStats?.averageOrderValue ?? 0)}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  每笔付费订单平均金额
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Revenue by Period */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>收入统计</CardTitle>
                <CardDescription>不同时间段的收入详情</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <div className="font-medium">24小时</div>
                      <div className="text-sm text-muted-foreground">{orderStats?.orders24h ?? 0} 单</div>
                    </div>
                    <div className="text-right">
                      <div className="text-xl font-bold text-green-600">{formatCurrency(orderStats?.revenue24h ?? 0)}</div>
                    </div>
                  </div>
                  <div className="flex items-center justify-between py-3 border-b">
                    <div>
                      <div className="font-medium">7天</div>
                      <div className="text-sm text-muted-foreground">{orderStats?.orders7d ?? 0} 单</div>
                    </div>
                    <div className="text-right">
                      <div className="text-xl font-bold text-blue-600">{formatCurrency(orderStats?.revenue7d ?? 0)}</div>
                    </div>
                  </div>
                  <div className="flex items-center justify-between py-3">
                    <div>
                      <div className="font-medium">30天</div>
                      <div className="text-sm text-muted-foreground">{orderStats?.orders30d ?? 0} 单</div>
                    </div>
                    <div className="text-right">
                      <div className="text-xl font-bold text-purple-600">{formatCurrency(orderStats?.revenue30d ?? 0)}</div>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>订单转化</CardTitle>
                <CardDescription>订单状态分布</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  <div className="flex items-center gap-3">
                    <div className="w-4 h-4 rounded bg-green-500"></div>
                    <span className="flex-1">已付款</span>
                    <span className="font-medium">{orderStats?.paidOrders ?? 0}</span>
                    <span className="text-muted-foreground w-16 text-right">
                      {orderStats?.totalOrders ? formatPercentage((orderStats.paidOrders / orderStats.totalOrders) * 100) : '0%'}
                    </span>
                  </div>
                  <div className="flex items-center gap-3">
                    <div className="w-4 h-4 rounded bg-yellow-500"></div>
                    <span className="flex-1">待付款</span>
                    <span className="font-medium">{orderStats?.unpaidOrders ?? 0}</span>
                    <span className="text-muted-foreground w-16 text-right">
                      {orderStats?.totalOrders ? formatPercentage((orderStats.unpaidOrders / orderStats.totalOrders) * 100) : '0%'}
                    </span>
                  </div>
                  <div className="mt-4 pt-4 border-t">
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">转化率</span>
                      <span className="font-medium">{formatPercentage(orderStats?.conversionRate ?? 0)}</span>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Daily Revenue Trend */}
          {orderStats?.revenueByPeriod && orderStats.revenueByPeriod.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>每日收入趋势</CardTitle>
                <CardDescription>近30天的每日收入情况</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex items-end gap-1 h-40 overflow-x-auto">
                  {orderStats.revenueByPeriod.map((item) => {
                    const maxRevenue = Math.max(...orderStats.revenueByPeriod.map(p => p.revenue));
                    const height = maxRevenue > 0 ? (item.revenue / maxRevenue) * 100 : 0;
                    return (
                      <div key={item.period} className="flex-shrink-0 w-6 flex flex-col items-center gap-1" title={`${item.period}: ${formatCurrency(item.revenue)} (${item.orders}单)`}>
                        <div
                          className="w-full bg-primary rounded-t transition-all hover:bg-primary/80"
                          style={{ height: `${height}%`, minHeight: item.revenue > 0 ? '4px' : '0' }}
                        />
                        <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
                          {item.period.slice(5)}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        {/* Devices Tab */}
        <TabsContent value="devices" className="space-y-6">
          {/* Device Summary Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>总设备数</CardDescription>
                <CardTitle className="text-3xl">{deviceStats?.totalDevices ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  {deviceStats?.unknownDevices ?? 0} 未知类型
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>桌面设备</CardDescription>
                <CardTitle className="text-3xl">{deviceStats?.desktopDevices ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  macOS / Windows / Linux
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>移动设备</CardDescription>
                <CardTitle className="text-3xl">{deviceStats?.mobileDevices ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  iOS / Android
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-2">
                <CardDescription>活跃设备 (7天)</CardDescription>
                <CardTitle className="text-3xl">{deviceStats?.active7d ?? 0}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">
                  24小时: {deviceStats?.active24h ?? 0} | 30天: {deviceStats?.active30d ?? 0}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Platform Distribution */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>平台分布</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {deviceStats?.byPlatform?.map((p) => {
                    const info = getPlatformInfo(p.platform);
                    const percentage = deviceStats.totalDevices > 0
                      ? ((p.count / deviceStats.totalDevices) * 100).toFixed(1)
                      : 0;
                    return (
                      <div key={p.platform} className="flex items-center gap-3">
                        <Badge variant="outline" className={`${info.color} text-white min-w-[80px] justify-center`}>
                          {info.name}
                        </Badge>
                        <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                          <div
                            className={`h-full ${info.color}`}
                            style={{ width: `${percentage}%` }}
                          />
                        </div>
                        <span className="text-sm text-muted-foreground w-20 text-right">
                          {p.count} ({percentage}%)
                        </span>
                      </div>
                    );
                  })}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>版本分布</CardTitle>
                <CardDescription>前10个版本</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  {deviceStats?.byVersion?.map((v, i) => (
                    <div key={v.version} className="flex items-center justify-between">
                      <span className="font-mono text-sm">{v.version}</span>
                      <Badge variant={i === 0 ? "default" : "secondary"}>
                        {v.count}
                      </Badge>
                    </div>
                  ))}
                  {(!deviceStats?.byVersion || deviceStats.byVersion.length === 0) && (
                    <div className="text-muted-foreground text-sm">暂无版本数据</div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Architecture Distribution */}
          <Card>
            <CardHeader>
              <CardTitle>架构分布</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="flex flex-wrap gap-4">
                {deviceStats?.byArch?.map((a) => (
                  <div key={a.arch} className="flex items-center gap-2 bg-muted px-4 py-2 rounded-lg">
                    <span className="font-mono font-medium">{a.arch}</span>
                    <Badge variant="secondary">{a.count}</Badge>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* Active Devices Table */}
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>活跃设备</CardTitle>
                <CardDescription>最近活跃的设备列表</CardDescription>
              </div>
              <Select value={activePeriod} onValueChange={(v) => { setActivePeriod(v as typeof activePeriod); setPage(1); }}>
                <SelectTrigger className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="24h">最近24小时</SelectItem>
                  <SelectItem value="7d">最近7天</SelectItem>
                  <SelectItem value="30d">最近30天</SelectItem>
                </SelectContent>
              </Select>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>用户</TableHead>
                    <TableHead>平台</TableHead>
                    <TableHead>版本</TableHead>
                    <TableHead>架构</TableHead>
                    <TableHead>最后活跃</TableHead>
                    <TableHead>注册时间</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {activeDevices.map((device) => {
                    const info = getPlatformInfo(device.appPlatform);
                    return (
                      <TableRow key={device.udid}>
                        <TableCell>
                          <div className="max-w-[200px] truncate" title={device.userEmail}>
                            {device.userEmail || "-"}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className={`${info.color} text-white`}>
                            {info.name}
                          </Badge>
                        </TableCell>
                        <TableCell className="font-mono text-sm">
                          {device.appVersion || "-"}
                        </TableCell>
                        <TableCell className="font-mono text-sm">
                          {device.appArch || "-"}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {formatTimeAgo(device.tokenLastUsedAt)}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {formatTimestamp(device.createdAt)}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                  {activeDevices.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={6} className="text-center text-muted-foreground py-8">
                        该时间段内无活跃设备
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-center gap-2 mt-4">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => Math.max(1, p - 1))}
                    disabled={page === 1}
                  >
                    上一页
                  </Button>
                  <span className="text-sm text-muted-foreground">
                    第 {page} 页，共 {totalPages} 页
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                    disabled={page === totalPages}
                  >
                    下一页
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
