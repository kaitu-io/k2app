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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api, NodeOperation, ApiError, ErrorCode } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";
import { RefreshCw, Loader2, Plus } from "lucide-react";
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

// Action display labels (Chinese admin UI)
const actionLabels: Record<string, string> = {
  provision: "开通",
  change_ip: "换 IP",
  stop: "停机",
  destroy: "销毁",
};

// Status display labels + badge variant
const statusLabels: Record<string, string> = {
  queued: "排队中",
  claimed: "已认领",
  in_progress: "执行中",
  done: "完成",
  failed: "失败",
  canceled: "已取消",
};

const statusVariant: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  queued: "outline",
  claimed: "secondary",
  in_progress: "default",
  done: "default",
  failed: "destructive",
  canceled: "outline",
};

const ACTION_FILTERS = ["provision", "change_ip", "stop", "destroy"];
const STATUS_FILTERS = ["queued", "claimed", "in_progress", "done", "failed", "canceled"];

// Format unix seconds for display
function formatDate(timestamp: number): string {
  if (!timestamp) return "-";
  return new Date(timestamp * 1000).toLocaleString("zh-CN");
}

// Age = now − createdAt, as a coarse human string
function formatAge(createdAt: number): string {
  if (!createdAt) return "-";
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - createdAt));
  if (seconds < 60) return `${seconds}秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}小时前`;
  const days = Math.floor(hours / 24);
  return `${days}天前`;
}

// Safely parse a JSON-string column; returns {} on any failure.
function safeParse(raw: string | undefined | null): Record<string, unknown> {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

// Render the params JSON as a compact one-line summary (skip when "{}").
function summarizeJson(raw: string | undefined | null): string {
  const obj = safeParse(raw);
  const keys = Object.keys(obj);
  if (keys.length === 0) return "";
  return keys.map((k) => `${k}: ${String(obj[k])}`).join(", ");
}

export default function NodeOperationsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [operations, setOperations] = useState<NodeOperation[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [pagination, setPagination] = useState({ page: 0, pageSize: 50, total: 0 });

  // Filter state from URL
  const page = searchParams.get("page") ? parseInt(searchParams.get("page") as string, 10) : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 50;
  const action = searchParams.get("action") || "";
  const status = searchParams.get("status") || "";

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false);
  const [createSubId, setCreateSubId] = useState("");
  const [createAction, setCreateAction] = useState<"change_ip" | "stop" | "destroy">("change_ip");
  const [createReason, setCreateReason] = useState("");
  const [createTargetRegion, setCreateTargetRegion] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  // Mark-failed dialog state
  const [failOp, setFailOp] = useState<NodeOperation | null>(null);
  const [failReason, setFailReason] = useState("");
  const [isFailing, setIsFailing] = useState(false);

  // Per-row busy guard (keyed by operation id)
  const [busyId, setBusyId] = useState<number | null>(null);

  const fetchOperations = async () => {
    setIsLoading(true);
    try {
      const response = await api.listNodeOperations({
        page,
        pageSize,
        action: action || undefined,
        status: status || undefined,
      });
      setOperations(response.items || []);
      setPagination(
        response.pagination || { page: 0, pageSize, total: (response.items || []).length }
      );
    } catch (error) {
      console.error("Failed to fetch node operations:", error);
      toast.error("获取运维任务列表失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchOperations();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize, action, status]);

  // Push a single filter into the URL (resets to page 0). "all" clears it.
  const setFilter = (key: "action" | "status", value: string) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("page", "0");
    if (!value || value === "all") {
      params.delete(key);
    } else {
      params.set(key, value);
    }
    router.push(`/manager/node-operations?${params.toString()}`);
  };

  const handlePageChange = (newPage: number) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("page", (newPage - 1).toString());
    router.push(`/manager/node-operations?${params.toString()}`);
  };

  const runUpdate = async (
    op: NodeOperation,
    newStatus: string,
    body?: { result?: Record<string, unknown>; error?: string }
  ) => {
    setBusyId(op.id);
    try {
      await api.updateNodeOperation(op.id, { status: newStatus, ...body });
      toast.success(`任务 #${op.id} 已更新为「${statusLabels[newStatus] || newStatus}」`);
      await fetchOperations();
    } catch (error) {
      console.error("Failed to update node operation:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "更新任务失败");
    } finally {
      setBusyId(null);
    }
  };

  const handleClaim = async (op: NodeOperation) => {
    setBusyId(op.id);
    try {
      await api.claimNodeOperation(op.id);
      toast.success(`任务 #${op.id} 已认领`);
      await fetchOperations();
    } catch (error) {
      console.error("Failed to claim node operation:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "认领任务失败");
    } finally {
      setBusyId(null);
    }
  };

  const handleCreate = async () => {
    const subId = parseInt(createSubId, 10);
    if (!subId || subId <= 0) {
      toast.error("请输入有效的订阅 ID");
      return;
    }
    const params: Record<string, unknown> = {};
    if (createReason.trim()) params.reason = createReason.trim();
    if (createAction === "change_ip" && createTargetRegion.trim()) {
      params.targetRegion = createTargetRegion.trim();
    }
    setIsCreating(true);
    try {
      await api.createNodeOperation({
        subId,
        action: createAction,
        params: Object.keys(params).length > 0 ? params : undefined,
      });
      toast.success("运维任务已创建");
      setCreateOpen(false);
      setCreateSubId("");
      setCreateReason("");
      setCreateTargetRegion("");
      setCreateAction("change_ip");
      await fetchOperations();
    } catch (error) {
      console.error("Failed to create node operation:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      if (code === ErrorCode.Conflict) {
        toast.error("该订阅已有同类型的未结任务，请勿重复创建");
      } else {
        toast.error(code ? getApiErrorMessageZh(code) : "创建运维任务失败");
      }
    } finally {
      setIsCreating(false);
    }
  };

  const handleMarkFailed = async () => {
    if (!failOp) return;
    setIsFailing(true);
    try {
      await api.updateNodeOperation(failOp.id, {
        status: "failed",
        error: failReason.trim() || "manual fail",
      });
      toast.success(`任务 #${failOp.id} 已标记为失败`);
      setFailOp(null);
      setFailReason("");
      await fetchOperations();
    } catch (error) {
      console.error("Failed to mark node operation failed:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "标记失败失败");
    } finally {
      setIsFailing(false);
    }
  };

  const totalPages = Math.ceil(pagination.total / pagination.pageSize);

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-3xl font-bold">节点运维</h1>
            <p className="text-muted-foreground">专属线路运维任务队列（开通 / 换 IP / 停机 / 销毁）</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4 mr-2" />
              新建运维任务
            </Button>
            <Button onClick={fetchOperations} disabled={isLoading}>
              {isLoading ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4 mr-2" />
              )}
              刷新
            </Button>
          </div>
        </div>

        {/* Filter toolbar */}
        <div className="flex items-center gap-4 mb-4">
          <Select value={action || "all"} onValueChange={(v) => setFilter("action", v)}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="动作" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部动作</SelectItem>
              {ACTION_FILTERS.map((a) => (
                <SelectItem key={a} value={a}>
                  {actionLabels[a]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={status || "all"} onValueChange={(v) => setFilter("status", v)}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="状态" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              {STATUS_FILTERS.map((s) => (
                <SelectItem key={s} value={s}>
                  {statusLabels[s]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
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
                  <TableHead>ID</TableHead>
                  <TableHead>动作</TableHead>
                  <TableHead>订阅 ID</TableHead>
                  <TableHead>云实例</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>认领人</TableHead>
                  <TableHead>创建者</TableHead>
                  <TableHead>创建时间</TableHead>
                  <TableHead>错误 / 备注</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {operations.length > 0 ? (
                  operations.map((op) => {
                    const isProvision = op.action === "provision";
                    const rowBusy = busyId === op.id;
                    const paramSummary = summarizeJson(op.params);
                    const resultSummary = summarizeJson(op.result);
                    return (
                      <TableRow key={op.id}>
                        <TableCell className="font-mono">{op.id}</TableCell>
                        <TableCell>
                          <Badge variant="outline">{actionLabels[op.action] || op.action}</Badge>
                          {paramSummary && (
                            <div className="text-xs text-muted-foreground mt-1 max-w-[160px] truncate" title={paramSummary}>
                              {paramSummary}
                            </div>
                          )}
                        </TableCell>
                        <TableCell className="font-mono">{op.subId}</TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {op.cloudInstanceId ?? "-"}
                        </TableCell>
                        <TableCell>
                          <Badge variant={statusVariant[op.status] || "outline"}>
                            {statusLabels[op.status] || op.status}
                          </Badge>
                          {resultSummary && (
                            <div className="text-xs text-muted-foreground mt-1 max-w-[160px] truncate" title={resultSummary}>
                              {resultSummary}
                            </div>
                          )}
                        </TableCell>
                        <TableCell className="text-sm">{op.holder || "-"}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {op.createdBy || "-"}
                        </TableCell>
                        <TableCell>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="text-sm text-muted-foreground">
                                {formatAge(op.createdAt)}
                              </span>
                            </TooltipTrigger>
                            <TooltipContent>{formatDate(op.createdAt)}</TooltipContent>
                          </Tooltip>
                        </TableCell>
                        <TableCell>
                          {op.lastError ? (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="text-xs text-destructive max-w-[160px] truncate block">
                                  {op.lastError}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>
                                <p className="max-w-xs">{op.lastError}</p>
                              </TooltipContent>
                            </Tooltip>
                          ) : (
                            <span className="text-xs text-muted-foreground">-</span>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy || op.status !== "queued"}
                              onClick={() => handleClaim(op)}
                            >
                              认领
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy}
                              onClick={() => runUpdate(op, "in_progress")}
                            >
                              执行中
                            </Button>
                            {isProvision ? (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  {/* span wrapper so tooltip works on a disabled button */}
                                  <span tabIndex={0}>
                                    <Button variant="outline" size="sm" disabled>
                                      完成
                                    </Button>
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent>完成由节点自注册</TooltipContent>
                              </Tooltip>
                            ) : (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={rowBusy}
                                onClick={() => runUpdate(op, "done")}
                              >
                                完成
                              </Button>
                            )}
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy}
                              onClick={() => {
                                setFailReason("");
                                setFailOp(op);
                              }}
                            >
                              失败
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy}
                              onClick={() => runUpdate(op, "canceled")}
                            >
                              取消
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })
                ) : (
                  <TableRow>
                    <TableCell colSpan={10} className="h-24 text-center">
                      暂无运维任务
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

        {/* Create operation dialog */}
        <Dialog open={createOpen} onOpenChange={(open) => !isCreating && setCreateOpen(open)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>新建运维任务</DialogTitle>
              <DialogDescription>
                手动派发一条运维任务（开通由订单触发，此处不可选）。
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-2">
                <Label htmlFor="create-sub-id">订阅 ID</Label>
                <Input
                  id="create-sub-id"
                  type="number"
                  value={createSubId}
                  onChange={(e) => setCreateSubId(e.target.value)}
                  placeholder="例如 1234"
                />
              </div>
              <div className="space-y-2">
                <Label>动作</Label>
                <Select
                  value={createAction}
                  onValueChange={(v) => setCreateAction(v as "change_ip" | "stop" | "destroy")}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="change_ip">{actionLabels.change_ip}</SelectItem>
                    <SelectItem value="stop">{actionLabels.stop}</SelectItem>
                    <SelectItem value="destroy">{actionLabels.destroy}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              {createAction === "change_ip" && (
                <div className="space-y-2">
                  <Label htmlFor="create-region">目标区域（可选）</Label>
                  <Input
                    id="create-region"
                    value={createTargetRegion}
                    onChange={(e) => setCreateTargetRegion(e.target.value)}
                    placeholder="例如 ap-northeast-1"
                  />
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="create-reason">备注 / 原因（可选）</Label>
                <Input
                  id="create-reason"
                  value={createReason}
                  onChange={(e) => setCreateReason(e.target.value)}
                  placeholder="将写入 params.reason"
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={isCreating}>
                取消
              </Button>
              <Button onClick={handleCreate} disabled={isCreating}>
                {isCreating ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    创建中...
                  </>
                ) : (
                  "创建"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Mark-failed dialog */}
        <Dialog open={!!failOp} onOpenChange={(open) => !isFailing && !open && setFailOp(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>标记任务失败</DialogTitle>
              <DialogDescription>
                为任务 <strong>#{failOp?.id}</strong>（{failOp ? actionLabels[failOp.action] || failOp.action : ""}）
                填写失败原因。
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2 py-2">
              <Label htmlFor="fail-reason">失败原因</Label>
              <Input
                id="fail-reason"
                value={failReason}
                onChange={(e) => setFailReason(e.target.value)}
                placeholder="例如 provider 配额不足"
              />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setFailOp(null)} disabled={isFailing}>
                取消
              </Button>
              <Button variant="destructive" onClick={handleMarkFailed} disabled={isFailing}>
                {isFailing ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    提交中...
                  </>
                ) : (
                  "确认失败"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}
