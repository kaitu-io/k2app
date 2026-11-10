"use client";

export const dynamic = "force-dynamic";

import { Fragment, type ReactNode, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
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
import {
  api,
  CloudAccount,
  CloudAccountUsage,
  CloudInstance,
  CloudRegion,
  NodeDistributionResponse,
  NodeDistributionStatus,
  NodeDistributionSummary,
  NodeDistributionVerdict,
} from "@/lib/api";
import { fleetCountries, FleetRow, mergeFleetRows, rowCountry } from "@/lib/node-fleet";
import { useManagerBrand } from "@/components/manager/brand";
import { toast } from "sonner";
import { RefreshCw, Globe, Loader2, AlertCircle, Plus, Trash2, Copy, ChevronDown, ChevronRight } from "lucide-react";
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

const LIVE_REFRESH_MS = 30_000;

// Provider display names
const providerNames: Record<string, string> = {
  bandwagon: "搬瓦工",
  vultr: "Vultr",
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

// Standalone rows are placeholders: they carry no provider traffic data.
function hasProviderTraffic(instance: CloudInstance): boolean {
  return instance.provider !== "ssh_standalone";
}

function UsageBar({ label, ratio }: { label: string; ratio: number }) {
  const percent = ratioToPercent(ratio);
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs text-muted-foreground w-6">{label}</span>
      <div className="flex-1 bg-muted rounded-full h-2 max-w-[80px]">
        <div
          className={`h-2 rounded-full ${
            percent > 90 ? "bg-red-500" : percent > 70 ? "bg-yellow-500" : "bg-green-500"
          }`}
          style={{ width: `${percent}%` }}
        />
      </div>
      <span className="text-xs text-muted-foreground w-10">{percent.toFixed(0)}%</span>
    </div>
  );
}

function UsageNote({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground">
      <span className="w-6">{label}</span>
      <span>{children}</span>
    </div>
  );
}

function ProviderUsage({ instance }: { instance: CloudInstance }) {
  if (!hasProviderTraffic(instance)) {
    return <UsageNote label="厂商">{"未接入"}</UsageNote>;
  }
  if (instance.traffic_total_gb <= 0) {
    return (
      <UsageNote label="厂商">
        {formatTraffic(instance.traffic_used_gb)}{" 出站 · 共享账号池"}
      </UsageNote>
    );
  }
  return (
    <div className="space-y-1">
      <UsageBar label="厂商" ratio={instance.traffic_ratio} />
      <div className="text-xs text-muted-foreground">
        {formatTraffic(instance.traffic_used_gb)}{" / "}{formatTraffic(instance.traffic_total_gb)}
      </div>
    </div>
  );
}

function MeterUsage({ instance }: { instance: CloudInstance }) {
  const meter = instance.meter;
  if (!meter) {
    return <UsageNote label="自测">{"无节点上报"}</UsageNote>;
  }
  if (meter.total_gb <= 0) {
    return <UsageNote label="自测">{formatTraffic(meter.used_gb)}{" · 不限量"}</UsageNote>;
  }
  return (
    <div className="space-y-1">
      <UsageBar label="自测" ratio={meter.ratio} />
      <div className="text-xs text-muted-foreground">
        {formatTraffic(meter.used_gb)}{" / "}{formatTraffic(meter.total_gb)}
      </div>
    </div>
  );
}

// Get region display name (prefer Chinese)
function getRegionName(region: string, regions: CloudRegion[]): string {
  const found = regions.find(r => r.slug === region || r.providerId === region);
  if (found) {
    return found.nameZh || found.nameEn || region;
  }
  return region || "-";
}

const statusLabels: Record<NodeDistributionStatus, { label: string; className: string }> = {
  overloaded: { label: "过载", className: "bg-red-100 text-red-800" },
  idle: { label: "空闲", className: "bg-yellow-100 text-yellow-800" },
  normal: { label: "正常", className: "bg-green-100 text-green-800" },
  exhausted: { label: "流量用尽", className: "bg-muted text-muted-foreground" },
  offline: { label: "离线", className: "bg-muted text-muted-foreground" },
  hidden: { label: "已下架", className: "bg-muted text-muted-foreground" },
  private: { label: "专属", className: "bg-blue-100 text-blue-800" },
};

const verdictLabels: Record<NodeDistributionVerdict, { label: string; className: string }> = {
  ok: { label: "均衡", className: "text-green-700" },
  skewed: { label: "偏斜", className: "text-yellow-700" },
  imbalanced: { label: "失衡", className: "text-red-700" },
};

function formatPercent(value: number | null): string {
  return value === null ? "-" : `${value.toFixed(0)}%`;
}

function formatRelativeTime(timestamp: number): string {
  if (!timestamp) return "从未";
  const diff = Date.now() / 1000 - timestamp;
  if (diff < 60) return "刚刚";
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`;
  return `${Math.floor(diff / 86400)}天前`;
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).then(() => toast.success("已复制"));
}

// Brand ids are rendered verbatim (lowercase registry ids, never display names —
// tests/brand-guard.test.ts scans this file).
function VisibilityBadge({ label, visible }: { label: string; visible: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-xs ${
        visible ? "bg-green-100 text-green-800" : "bg-muted text-muted-foreground"
      }`}
    >
      {label} {visible ? "✓" : "✗"}
    </span>
  );
}

function ScopeCard({
  summary,
  active,
  onClick,
}: {
  summary: NodeDistributionSummary;
  active: boolean;
  onClick: () => void;
}) {
  const verdict = verdictLabels[summary.verdict];
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-md border px-3 py-2 text-left min-w-[150px] ${active ? "border-primary" : ""}`}
    >
      <div className="flex items-center justify-between gap-3 text-sm font-medium">
        <span>{summary.scope === "all" ? "全部" : summary.scope}</span>
        <span className={verdict.className}>{verdict.label}</span>
      </div>
      <div className="text-xs text-muted-foreground mt-1">
        {summary.activeDevices}{" 台设备 · "}{summary.eligibleNodes}{" 节点"}
      </div>
      <div className="text-xs text-muted-foreground">
        {"空闲 "}{summary.idleNodes}{" · 过载 "}{summary.overloadedNodes}{" · 最高 "}{summary.maxDevices}
      </div>
    </button>
  );
}

function DevicesCell({ row, maxDevices }: { row: FleetRow; maxDevices: number }) {
  const node = row.node;
  if (!node) return <span className="text-xs text-muted-foreground">-</span>;
  const percent = maxDevices > 0 ? (node.activeDevices / maxDevices) * 100 : 0;
  return (
    <div className="min-w-[110px]">
      <div className="text-lg font-semibold leading-none">{node.activeDevices}</div>
      <div className="mt-1 h-1.5 max-w-[100px] rounded-full bg-muted">
        <div className="h-1.5 rounded-full bg-primary" style={{ width: `${percent}%` }} />
      </div>
      <div className="mt-1 text-xs text-muted-foreground">
        {"会话 "}{node.activeSessions}{" · 24h峰值 "}{node.peakDevices24h}
      </div>
    </div>
  );
}

function LoadCell({ row }: { row: FleetRow }) {
  const node = row.node;
  if (!node) return <span className="text-xs text-muted-foreground">-</span>;
  if (node.metricsVersion < 1) {
    return (
      <div className="text-xs text-muted-foreground">
        <div>{"sidecar 待升级"}</div>
        {node.memoryPercent !== null && <div>{"内存 "}{formatPercent(node.memoryPercent)}</div>}
      </div>
    );
  }
  return (
    <div className="space-y-0.5 text-xs">
      <div>
        {"CPU "}{formatPercent(node.cpuPercent)}{" · steal "}{formatPercent(node.stealPercent)}
        {node.cpuCores > 0 && ` · ${node.cpuCores}核`}
      </div>
      <div>
        {"带宽 "}{node.bandwidthMbps === null ? "-" : node.bandwidthMbps.toFixed(1)}
        {node.capacityMbps !== null && ` / ${node.capacityMbps.toFixed(0)}`}{" Mbps"}
      </div>
      <div>{"压力 "}{node.pressureRatio === null ? "-" : node.pressureRatio.toFixed(2)}</div>
    </div>
  );
}

function RowDetails({ row, regions }: { row: FleetRow; regions: CloudRegion[] }) {
  const { node, instance } = row;
  return (
    <div className="grid gap-4 py-2 md:grid-cols-2 text-sm">
      <div className="space-y-2">
        {node?.tunnels.map((tunnel) => (
          <div key={tunnel.id} className="min-w-0">
            <div className="text-xs text-muted-foreground">{tunnel.name}</div>
            <button
              type="button"
              className="flex max-w-full items-center gap-1 font-mono text-xs hover:text-primary"
              title={tunnel.serverUrl ?? ""}
              onClick={() => tunnel.serverUrl && copyToClipboard(tunnel.serverUrl)}
            >
              <span className="truncate">{tunnel.serverUrl ?? `${tunnel.domain}:${tunnel.port}`}</span>
              {tunnel.serverUrl && <Copy className="h-3 w-3 shrink-0" />}
            </button>
          </div>
        ))}
        {!node && <div className="text-xs text-muted-foreground">{"尚未注册为节点"}</div>}
      </div>
      <div className="space-y-1 text-xs text-muted-foreground">
        {node && (
          <>
            <div className="font-mono">{(node.brands ?? []).join(", ") || "—"}</div>
            <div className="flex gap-1">
              <VisibilityBadge label="kaitu" visible={node.visibleKaitu ?? true} />
              <VisibilityBadge label="overleap" visible={node.visibleOverleap ?? false} />
            </div>
            {node.ipv6 && <div className="font-mono">{node.ipv6}</div>}
            <div>{"用量上报 "}{formatRelativeTime(node.usageReportAt)}{" · 负载上报 "}{formatRelativeTime(node.metricsAt)}</div>
          </>
        )}
        {instance && (
          <>
            <div className="font-mono">{instance.instance_id}</div>
            <div>{"📍"}{getRegionName(instance.region, regions)}</div>
            {instance.expires_at > 0 && <div>{"到期 "}{formatDateISO(instance.expires_at)}</div>}
            {hasProviderTraffic(instance) && instance.traffic_reset_at > 0 && (
              <div>{"厂商重置 "}{formatDateISO(instance.traffic_reset_at)}</div>
            )}
            {instance.meter && instance.meter.reset_at > 0 && (
              <div>{"自测重置 "}{formatDateISO(instance.meter.reset_at)}</div>
            )}
            <div>{"最后同步 "}{formatDate(instance.last_synced_at)}</div>
          </>
        )}
      </div>
    </div>
  );
}

async function fetchAllCloudInstances(): Promise<CloudInstance[]> {
  const pageSize = 100; // the API caps pageSize at 100
  const all: CloudInstance[] = [];
  for (let page = 1; ; page++) {
    const res = await api.getCloudInstances({ page, pageSize });
    const items = res.items || [];
    all.push(...items);
    if (items.length === 0 || all.length >= res.pagination.total) return all;
  }
}

export default function NodesPage() {
  const router = useRouter();
  const { brandParam } = useManagerBrand();

  const [distribution, setDistribution] = useState<NodeDistributionResponse | null>(null);
  const [liveFailed, setLiveFailed] = useState(false);
  const [instances, setInstances] = useState<CloudInstance[]>([]);
  const [accounts, setAccounts] = useState<CloudAccount[]>([]);
  const [accountUsages, setAccountUsages] = useState<CloudAccountUsage[]>([]);
  const [regions, setRegions] = useState<CloudRegion[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isSyncing, setIsSyncing] = useState(false);
  const [scope, setScope] = useState("all");
  const [account, setAccount] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [changeIPInstance, setChangeIPInstance] = useState<CloudInstance | null>(null);
  const [deleteInstance, setDeleteInstance] = useState<CloudInstance | null>(null);
  const [isChangingIP, setIsChangingIP] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

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

  const fetchDistribution = async () => {
    try {
      setDistribution(await api.getNodeDistribution(brandParam));
      setLiveFailed(false);
    } catch (error) {
      // The banner carries this: a toast every 30 seconds would only be noise.
      console.error("Failed to fetch node distribution:", error);
      setLiveFailed(true);
    }
  };

  const fetchCloud = async () => {
    try {
      // The pool card is optional: its failure must not take the instance list down.
      const [items, usagesRes] = await Promise.all([
        fetchAllCloudInstances(),
        api.listCloudAccountUsages().catch((error) => {
          console.error("Failed to fetch cloud account usages:", error);
          return { items: [] };
        }),
      ]);
      setInstances(items);
      setAccountUsages(usagesRes.items || []);
    } catch (error) {
      console.error("Failed to fetch cloud instances:", error);
      toast.error("获取云实例列表失败");
    }
  };

  const fetchAll = async () => {
    setIsLoading(true);
    await Promise.all([fetchDistribution(), fetchCloud()]);
    setIsLoading(false);
  };

  useEffect(() => {
    fetchAll();
    const timer = setInterval(() => {
      if (!document.hidden) fetchDistribution();
    }, LIVE_REFRESH_MS);
    return () => clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [brandParam]);

  const handleSync = async () => {
    setIsSyncing(true);
    try {
      await api.syncCloudInstances();
      toast.success("同步请求已发送，请稍后刷新查看结果");
      setTimeout(fetchCloud, 2000);
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
      setTimeout(fetchCloud, 3000);
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
      setTimeout(fetchCloud, 3000);
    } catch (error) {
      console.error("Failed to delete instance:", error);
      toast.error("删除实例失败");
    } finally {
      setIsDeleting(false);
      setDeleteInstance(null);
    }
  };

  const rows = useMemo(
    () =>
      mergeFleetRows(distribution?.items ?? [], instances, {
        // A brand view lists that brand's nodes; an instance without a node has no brand.
        includeUnregistered: !brandParam,
      }),
    [distribution, instances, brandParam],
  );
  const visibleRows = useMemo(
    () =>
      rows.filter(
        (row) =>
          (scope === "all" || rowCountry(row) === scope) &&
          (account === "all" || row.instance?.account_name === account),
      ),
    [rows, scope, account],
  );
  const maxDevices = visibleRows.reduce((max, row) => Math.max(max, row.node?.activeDevices ?? 0), 0);
  const summaries = distribution?.summaries ?? [];
  const countries = fleetCountries(rows);
  const scopeCards = summaries.filter((s) => s.scope === "all" || countries.includes(s.scope));

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-3xl font-bold">节点管理</h1>
            <p className="text-muted-foreground">
              {"实例、节点与实时分布 · 活跃设备按近 5 分钟有流量统计，每 30 秒刷新"}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => router.push("/manager/cloud/create")}>
              <Plus className="h-4 w-4 mr-2" />
              创建实例
            </Button>
            <Button variant="outline" onClick={handleSync} disabled={isSyncing}>
              {isSyncing ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4 mr-2" />
              )}
              同步实例
            </Button>
            <Button onClick={fetchAll} disabled={isLoading}>
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
              刷新
            </Button>
          </div>
        </div>

        {liveFailed && (
          <div className="mb-4 flex items-center gap-2 rounded-md border border-destructive px-4 py-3 text-sm text-destructive">
            <AlertCircle className="h-4 w-4 shrink-0" />
            {distribution
              ? "实时数据获取失败，下方是上次的数据"
              : "实时数据获取失败：云实例操作不受影响，但节点列表不完整，状态未知"}
          </div>
        )}

        <div className="mb-4 flex flex-wrap gap-2">
          {scopeCards.map((summary) => (
            <ScopeCard
              key={summary.scope}
              summary={summary}
              active={scope === summary.scope}
              onClick={() => setScope(summary.scope)}
            />
          ))}
        </div>

        {accountUsages.length > 0 && (
          <div className="mb-4 flex flex-wrap gap-3">
            {accountUsages.map((usage) => (
              <div key={usage.account_name} className="rounded-md border px-4 py-3 min-w-[260px]">
                <div className="text-sm font-medium">
                  {providerNames[usage.provider] || usage.provider}{" 账号池 · "}{usage.account_name}
                </div>
                <div className="mt-2">
                  <UsageBar label="出站" ratio={usage.ratio} />
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  {formatTraffic(usage.used_gb)}{" / "}{formatTraffic(usage.total_gb)}
                  {" · 🔄"}{formatDateISO(usage.reset_at)}
                </div>
                {usage.sync_error && (
                  <div className="text-xs text-destructive mt-1">{"同步错误，显示的是上次数据"}</div>
                )}
              </div>
            ))}
          </div>
        )}

        <div className="flex items-center gap-4 mb-4">
          <Select value={account} onValueChange={setAccount}>
            <SelectTrigger className="w-[220px]">
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
        </div>

        {isLoading && !distribution ? (
          <div className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
          </div>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead>节点</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>活跃设备</TableHead>
                  <TableHead>负载</TableHead>
                  <TableHead>流量使用</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleRows.length > 0 ? (
                  visibleRows.map((row) => {
                    const { node, instance } = row;
                    const status = node ? statusLabels[node.status] : null;
                    const timePercent = instance ? ratioToPercent(instance.time_ratio) : 0;
                    const isOpen = expanded === row.key;
                    return (
                      <Fragment key={row.key}>
                        <TableRow className="align-top">
                          <TableCell>
                            <button
                              type="button"
                              aria-label={isOpen ? "收起" : "展开"}
                              onClick={() => setExpanded(isOpen ? null : row.key)}
                            >
                              {isOpen ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                            </button>
                          </TableCell>
                          <TableCell>
                            <div className="font-medium">{node?.name || instance?.name || instance?.instance_id}</div>
                            <div className="font-mono text-xs text-muted-foreground">{row.ipv4 || "-"}</div>
                            <div className="flex flex-wrap items-center gap-1 mt-1 text-xs text-muted-foreground">
                              {node?.country && <span>{node.country}{node.region && ` · ${node.region}`}</span>}
                              {instance && (
                                <Badge variant="outline">{providerNames[instance.provider] || instance.provider}</Badge>
                              )}
                              {instance?.account_name && <span>{instance.account_name}</span>}
                            </div>
                            {instance?.sync_error && (
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
                            {status ? (
                              <span className={`inline-block whitespace-nowrap rounded px-1.5 py-0.5 text-xs ${status.className}`}>
                                {status.label}
                              </span>
                            ) : (
                              <span className="text-xs text-muted-foreground">{distribution ? "未注册" : "未知"}</span>
                            )}
                          </TableCell>
                          <TableCell>
                            <DevicesCell row={row} maxDevices={maxDevices} />
                          </TableCell>
                          <TableCell>
                            <LoadCell row={row} />
                          </TableCell>
                          <TableCell>
                            {instance ? (
                          <div className="space-y-1">
                            <ProviderUsage instance={instance} />
                            <MeterUsage instance={instance} />
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
                          </div>
                                                    ) : (
                              <span className="text-xs text-muted-foreground">无云实例</span>
                            )}
                          </TableCell>
                          <TableCell>
                            {instance && (
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
                            )}
                          </TableCell>
                        </TableRow>
                        {isOpen && (
                          <TableRow>
                            <TableCell />
                            <TableCell colSpan={6}>
                              <RowDetails row={row} regions={regions} />
                            </TableCell>
                          </TableRow>
                        )}
                      </Fragment>
                    );
                  })
                ) : (
                  <TableRow>
                    <TableCell colSpan={7} className="h-24 text-center">
                      暂无节点数据
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
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
