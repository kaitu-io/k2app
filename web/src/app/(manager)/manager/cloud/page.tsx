"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api, CloudInstance, CloudAccount, CloudRegion } from "@/lib/api";
import { toast } from "sonner";
import { RefreshCw, Globe, Loader2, AlertCircle, Plus, Trash2, Server } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Pagination } from "@/components/Pagination";

// Provider display names
const providerNames: Record<string, string> = {
  bandwagon: "搬瓦工",
  aws_lightsail: "AWS Lightsail",
  alibaba_swas: "阿里云轻量(国际)",
  aliyun_swas: "阿里云轻量(国内)",
  tencent_lighthouse: "腾讯云轻量(国际)",
  qcloud_lighthouse: "腾讯云轻量(国内)",
  ssh_standalone: "独立主机",
};

// Format GB to human readable
function formatTraffic(gb: number): string {
  if (gb === 0) return "0 GB";
  if (gb >= 1024) {
    return `${(gb / 1024).toFixed(2)} TB`;
  }
  return `${gb.toFixed(2)} GB`;
}

// Format timestamp for display
function formatDate(timestamp: number): string {
  if (!timestamp) return "-";
  return new Date(timestamp * 1000).toLocaleString("zh-CN");
}

// Format date as YYYY-MM-DD
function formatDateISO(timestamp: number): string {
  if (!timestamp) return "-";
  const date = new Date(timestamp * 1000);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

// Convert ratio (0-1) to percentage (0-100)
function ratioToPercent(ratio: number): number {
  return Math.min(100, Math.max(0, ratio * 100));
}

// Get region display name (prefer Chinese)
function getRegionName(region: string, regions: CloudRegion[]): string {
  const found = regions.find(r => r.slug === region || r.providerId === region);
  if (found) {
    return found.nameZh || found.nameEn || region;
  }
  return region || "-";
}

export default function CloudInstancesPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [instances, setInstances] = useState<CloudInstance[]>([]);
  const [accounts, setAccounts] = useState<CloudAccount[]>([]);
  const [regions, setRegions] = useState<CloudRegion[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isSyncing, setIsSyncing] = useState(false);
  const [changeIPInstance, setChangeIPInstance] = useState<CloudInstance | null>(null);
  const [deleteInstance, setDeleteInstance] = useState<CloudInstance | null>(null);
  const [isChangingIP, setIsChangingIP] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [pagination, setPagination] = useState({
    page: 0,
    pageSize: 50,
    total: 0,
  });

  // Filter state from URL
  const page = searchParams.get("page")
    ? parseInt(searchParams.get("page") as string, 10)
    : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 50;
  const provider = searchParams.get("provider") || "";
  const account = searchParams.get("account") || "";

  // Local filter state
  const [localAccount, setLocalAccount] = useState(account);

  // Fetch accounts and regions on mount
  useEffect(() => {
    const fetchMeta = async () => {
      try {
        const [accountsRes, regionsRes] = await Promise.all([
          api.listCloudAccounts(),
          api.listCloudRegions(),
        ]);
        setAccounts(accountsRes.items || []);
        setRegions(regionsRes.items || []);
      } catch (error) {
        console.error("Failed to fetch cloud metadata:", error);
      }
    };
    fetchMeta();
  }, []);

  const fetchInstances = async () => {
    setIsLoading(true);
    try {
      const response = await api.getCloudInstances({
        page,
        pageSize,
        provider: provider || undefined,
      });
      setInstances(response.items || []);
      setPagination(response.pagination);
    } catch (error) {
      console.error("Failed to fetch cloud instances:", error);
      toast.error("获取云实例列表失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchInstances();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize, provider, account]);

  const handleSync = async () => {
    setIsSyncing(true);
    try {
      await api.syncCloudInstances();
      toast.success("同步请求已发送，请稍后刷新查看结果");
      // Refresh after a short delay
      setTimeout(() => {
        fetchInstances();
      }, 2000);
    } catch (error) {
      console.error("Failed to sync cloud instances:", error);
      toast.error("同步云实例失败");
    } finally {
      setIsSyncing(false);
    }
  };

  const handleChangeIP = async () => {
    if (!changeIPInstance) return;

    setIsChangingIP(true);
    try {
      const response = await api.changeCloudInstanceIP(changeIPInstance.id);
      toast.success(`换 IP 任务已提交，任务 ID: ${response.task_id}`);
      setTimeout(() => {
        fetchInstances();
      }, 3000);
    } catch (error) {
      console.error("Failed to change IP:", error);
      toast.error("换 IP 操作失败");
    } finally {
      setIsChangingIP(false);
      setChangeIPInstance(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteInstance) return;

    setIsDeleting(true);
    try {
      const response = await api.deleteCloudInstance(deleteInstance.id);
      toast.success(`删除任务已提交，任务 ID: ${response.task_id}`);
      setTimeout(() => {
        fetchInstances();
      }, 3000);
    } catch (error) {
      console.error("Failed to delete instance:", error);
      toast.error("删除实例失败");
    } finally {
      setIsDeleting(false);
      setDeleteInstance(null);
    }
  };

  const handleFilter = () => {
    const params = new URLSearchParams();
    params.set("page", "0");
    params.set("pageSize", pageSize.toString());
    if (localAccount && localAccount !== "all") {
      params.set("account", localAccount);
    }
    router.push(`/manager/cloud?${params.toString()}`);
  };

  const handleClearFilters = () => {
    setLocalAccount("");
    router.push("/manager/cloud");
  };

  const handlePageChange = (newPage: number) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("page", (newPage - 1).toString());
    router.push(`/manager/cloud?${params.toString()}`);
  };

  const totalPages = Math.ceil(pagination.total / pagination.pageSize);

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-3xl font-bold">节点部署</h1>
            <p className="text-muted-foreground">管理所有云服务商的 VPS 实例</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => router.push("/manager/cloud/create")}>
              <Plus className="h-4 w-4 mr-2" />
              创建实例
            </Button>
            <Button onClick={handleSync} disabled={isSyncing}>
              {isSyncing ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4 mr-2" />
              )}
              同步实例
            </Button>
          </div>
        </div>

        {/* Filter toolbar */}
        <div className="flex items-center gap-4 mb-4">
          <Select value={localAccount} onValueChange={setLocalAccount}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="选择账号" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部账号</SelectItem>
              {accounts.map((acc) => (
                <SelectItem key={acc.name} value={acc.name}>
                  {acc.name} ({providerNames[acc.provider] || acc.provider})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Button onClick={handleFilter}>筛选</Button>
          <Button variant="outline" onClick={handleClearFilters}>
            清除筛选
          </Button>
        </div>

        {/* Table */}
        {isLoading ? (
          <div className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
          </div>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>实例信息</TableHead>
                  <TableHead>服务商/账号</TableHead>
                  <TableHead>关联节点</TableHead>
                  <TableHead>流量使用</TableHead>
                  <TableHead>周期截止</TableHead>
                  <TableHead>最后同步</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {instances.length > 0 ? (
                  instances.map((instance) => {
                    // Use API-provided ratios (0-1) converted to percentages (0-100)
                    const trafficPercent = ratioToPercent(instance.traffic_ratio);
                    const timePercent = ratioToPercent(instance.time_ratio);
                    return (
                      <TableRow key={instance.id}>
                        <TableCell>
                          <div className="font-medium">{instance.name || instance.instance_id}</div>
                          {instance.name && (
                            <div className="text-xs text-muted-foreground font-mono">
                              {instance.instance_id}
                            </div>
                          )}
                          <div className="text-xs text-muted-foreground">
                            📍{getRegionName(instance.region, regions)}
                          </div>
                          <div className="font-mono text-xs text-muted-foreground">
                            {instance.ip_address || "-"}
                          </div>
                          {instance.ipv6_address && (
                            <div className="font-mono text-xs text-muted-foreground truncate max-w-[180px]" title={instance.ipv6_address}>
                              {instance.ipv6_address}
                            </div>
                          )}
                          {instance.sync_error && (
                            <Tooltip>
                              <TooltipTrigger>
                                <div className="flex items-center gap-1 text-xs text-destructive">
                                  <AlertCircle className="h-3 w-3" />
                                  同步错误
                                </div>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p className="max-w-xs">{instance.sync_error}</p>
                              </TooltipContent>
                            </Tooltip>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="space-y-1">
                            <Badge variant="outline">
                              {providerNames[instance.provider] || instance.provider}
                            </Badge>
                            <div className="text-xs text-muted-foreground">
                              {instance.account_name}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          {instance.node_name ? (
                            <div className="flex items-center gap-2">
                              <Server className="h-4 w-4 text-muted-foreground" />
                              <div className="text-sm">{instance.node_name}</div>
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">未关联</span>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="space-y-1">
                            {/* Traffic progress bar */}
                            <div className="flex items-center gap-2">
                              <span className="text-xs text-muted-foreground w-6">流量</span>
                              <div className="flex-1 bg-muted rounded-full h-2 max-w-[80px]">
                                <div
                                  className={`h-2 rounded-full ${
                                    trafficPercent > 90
                                      ? "bg-red-500"
                                      : trafficPercent > 70
                                      ? "bg-yellow-500"
                                      : "bg-green-500"
                                  }`}
                                  style={{ width: `${trafficPercent}%` }}
                                />
                              </div>
                              <span className="text-xs text-muted-foreground w-10">
                                {trafficPercent.toFixed(0)}%
                              </span>
                            </div>
                            {/* Time progress bar - fills from left */}
                            <div className="flex items-center gap-2">
                              <span className="text-xs text-muted-foreground w-6">时间</span>
                              <div className="flex-1 bg-muted rounded-full h-2 max-w-[80px] overflow-hidden">
                                <div
                                  className="h-2 rounded-full bg-blue-500"
                                  style={{ width: `${timePercent}%` }}
                                />
                              </div>
                              <span className="text-xs text-muted-foreground w-10">
                                {timePercent.toFixed(0)}%
                              </span>
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {formatTraffic(instance.traffic_used_gb)} /{" "}
                              {formatTraffic(instance.traffic_total_gb)}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="space-y-0.5 text-sm">
                            {instance.expires_at > 0 && (
                              <div className={instance.expires_at * 1000 < Date.now() ? 'text-red-500 font-medium' : ''}>
                                📅{formatDateISO(instance.expires_at)}
                              </div>
                            )}
                            {instance.traffic_reset_at > 0 && (
                              <div className="text-muted-foreground">
                                🔄{formatDateISO(instance.traffic_reset_at)}
                              </div>
                            )}
                            {!instance.expires_at && !instance.traffic_reset_at && "-"}
                          </div>
                        </TableCell>
                        <TableCell>
                          <span className="text-sm text-muted-foreground">
                            {formatDate(instance.last_synced_at)}
                          </span>
                        </TableCell>
                        <TableCell>
                          <div className="flex gap-1">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button
                                  variant="outline"
                                  size="sm"
                                  onClick={() => setChangeIPInstance(instance)}
                                  disabled={instance.provider !== "aws_lightsail" && instance.provider !== "bandwagon"}
                                >
                                  <Globe className="h-4 w-4" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>
                                {(instance.provider === "aws_lightsail" || instance.provider === "bandwagon")
                                  ? "更换 IP"
                                  : "此服务商不支持换 IP"}
                              </TooltipContent>
                            </Tooltip>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button
                                  variant="outline"
                                  size="sm"
                                  onClick={() => setDeleteInstance(instance)}
                                  disabled={instance.provider === "bandwagon"}
                                >
                                  <Trash2 className="h-4 w-4 text-destructive" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>
                                {instance.provider === "bandwagon"
                                  ? "搬瓦工不支持删除"
                                  : "删除实例"}
                              </TooltipContent>
                            </Tooltip>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })
                ) : (
                  <TableRow>
                    <TableCell colSpan={8} className="h-24 text-center">
                      暂无云实例数据
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        )}

        {/* Pagination */}
        {!isLoading && totalPages > 1 && (
          <Pagination
            currentPage={pagination.page + 1}
            totalPages={totalPages}
            onPageChange={handlePageChange}
            className="mt-4"
          />
        )}

        {/* Change IP Confirmation Dialog */}
        <Dialog open={!!changeIPInstance} onOpenChange={() => setChangeIPInstance(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>确认更换 IP</DialogTitle>
              <DialogDescription>
                您确定要为实例 <strong>{changeIPInstance?.instance_id}</strong> 更换 IP 地址吗？
                <br />
                当前 IP: <span className="font-mono">{changeIPInstance?.ip_address}</span>
                <br />
                <br />
                此操作将：
                <ul className="list-disc list-inside mt-2">
                  <li>释放当前静态 IP</li>
                  <li>分配新的静态 IP</li>
                  <li>可能导致短暂的连接中断</li>
                </ul>
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => setChangeIPInstance(null)}
                disabled={isChangingIP}
              >
                取消
              </Button>
              <Button onClick={handleChangeIP} disabled={isChangingIP}>
                {isChangingIP ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    更换中...
                  </>
                ) : (
                  "确认更换"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Delete Instance Confirmation Dialog */}
        <Dialog open={!!deleteInstance} onOpenChange={() => setDeleteInstance(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>确认删除实例</DialogTitle>
              <DialogDescription>
                您确定要删除实例 <strong>{deleteInstance?.instance_id}</strong> 吗？
                <br />
                IP: <span className="font-mono">{deleteInstance?.ip_address}</span>
                <br />
                区域: {deleteInstance ? getRegionName(deleteInstance.region, regions) : ""}
                <br />
                <br />
                <span className="text-destructive font-medium">
                  此操作不可逆，实例及其数据将被永久删除！
                </span>
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => setDeleteInstance(null)}
                disabled={isDeleting}
              >
                取消
              </Button>
              <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
                {isDeleting ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    删除中...
                  </>
                ) : (
                  "确认删除"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

      </div>
    </TooltipProvider>
  );
}
