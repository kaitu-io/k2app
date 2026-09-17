"use client";

export const dynamic = "force-dynamic";

import { useCallback, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { toast } from "sonner";
import { Loader2, RefreshCw } from "lucide-react";
import { api, ApiError, ErrorCode, type AdminPrivateNodeSubscriptionItem } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { formatBytes, quotaLevel } from "@/lib/router-edition";
import { Pagination } from "@/components/Pagination";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

const PAGE_SIZE = 50;

const ALL_STATUSES = [
  "pending",
  "provisioning",
  "active",
  "grace",
  "suspended",
  "deprovisioned",
  "failed",
] as const;

const STATUS_LABEL: Record<string, string> = {
  pending: "待开通",
  provisioning: "开通中",
  active: "服务中",
  grace: "宽限期",
  suspended: "已停机",
  deprovisioned: "已回收",
  failed: "开通失败",
};

const STATUS_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  pending: "outline",
  provisioning: "secondary",
  active: "default",
  grace: "secondary",
  suspended: "destructive",
  deprovisioned: "outline",
  failed: "destructive",
};

const EXTEND_MONTH_OPTIONS = [1, 3, 6, 12];

function formatDate(ts: number | undefined): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

// 延期：状态未回收也未失败，即还有恢复服务的意义。
function canExtend(status: string): boolean {
  return status === "active" || status === "grace" || status === "suspended";
}

// 停机：仅在服务中才需要人工掐断（宽限期路由器仍可用，同样可停）。
function canStop(status: string): boolean {
  return status === "active" || status === "grace";
}

export default function PrivateNodeSubscriptionsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const page = Math.max(1, Number(searchParams.get("page") || "1"));
  const status = searchParams.get("status") || "";
  const userIdParam = Number(searchParams.get("userId") || "0") || undefined;

  const [items, setItems] = useState<AdminPrivateNodeSubscriptionItem[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [busyId, setBusyId] = useState<number | null>(null);

  const [userIdInput, setUserIdInput] = useState(userIdParam ? String(userIdParam) : "");
  useEffect(() => {
    setUserIdInput(userIdParam ? String(userIdParam) : "");
  }, [userIdParam]);

  // 延期对话框
  const [extendTarget, setExtendTarget] = useState<AdminPrivateNodeSubscriptionItem | null>(null);
  const [extendMonths, setExtendMonths] = useState(1);
  const [extendReason, setExtendReason] = useState("");
  const [isExtending, setIsExtending] = useState(false);

  // 停机对话框
  const [stopTarget, setStopTarget] = useState<AdminPrivateNodeSubscriptionItem | null>(null);
  const [isStopping, setIsStopping] = useState(false);

  const setQuery = useCallback(
    (patch: Record<string, string | number | undefined>) => {
      const next = new URLSearchParams(searchParams.toString());
      for (const [k, v] of Object.entries(patch)) {
        if (v === undefined || v === "" || v === "all") next.delete(k);
        else next.set(k, String(v));
      }
      router.push(`/manager/private-node-subscriptions?${next.toString()}`);
    },
    [router, searchParams]
  );

  const load = useCallback(async () => {
    setIsLoading(true);
    try {
      const res = await api.listPrivateNodeSubscriptions({
        page,
        pageSize: PAGE_SIZE,
        status: status || undefined,
        userId: userIdParam,
      });
      setItems(res.items || []);
      setTotal(res.pagination?.total ?? (res.items || []).length);
    } catch {
      toast.error("获取线路订阅失败");
    } finally {
      setIsLoading(false);
    }
  }, [page, status, userIdParam]);

  useEffect(() => {
    load();
  }, [load]);

  const commitUserId = () => {
    const trimmed = userIdInput.trim();
    const num = trimmed ? Number(trimmed) : undefined;
    setQuery({ userId: num && num > 0 ? num : undefined, page: 1 });
  };

  const openExtendDialog = (item: AdminPrivateNodeSubscriptionItem) => {
    setExtendMonths(1);
    setExtendReason("");
    setExtendTarget(item);
  };

  const handleExtend = async () => {
    if (!extendTarget) return;
    setBusyId(extendTarget.id);
    setIsExtending(true);
    try {
      const reloaded = await api.extendPrivateNodeSubscription(extendTarget.id, {
        months: extendMonths,
        reason: extendReason.trim(),
      });
      toast.success(`已延期至 ${formatDate(reloaded.expiresAt)}`);
      setExtendTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof ApiError ? getApiErrorMessageZh(e.code, "延期失败") : "延期失败");
    } finally {
      setIsExtending(false);
      setBusyId(null);
    }
  };

  const handleStop = async () => {
    if (!stopTarget) return;
    setBusyId(stopTarget.id);
    setIsStopping(true);
    try {
      await api.createNodeOperation({ subId: stopTarget.id, action: "stop" });
      toast.success("已创建停机任务", {
        action: { label: "去节点运维", onClick: () => router.push("/manager/node-operations") },
      });
      setStopTarget(null);
      await load();
    } catch (e) {
      if (e instanceof ApiError && e.code === ErrorCode.Conflict) {
        toast.error("该线路已有未完成的运维任务");
      } else {
        toast.error(e instanceof ApiError ? getApiErrorMessageZh(e.code, "创建停机任务失败") : "创建停机任务失败");
      }
    } finally {
      setIsStopping(false);
      setBusyId(null);
    }
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div className="container mx-auto py-10">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-3xl font-bold">线路订阅</h1>
          <p className="text-muted-foreground">专属线路开通状态与用量台账；开通失败请到「节点运维」处理</p>
        </div>
        <Button onClick={load} disabled={isLoading}>
          {isLoading ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-2" />}
          刷新
        </Button>
      </div>

      {/* 筛选 */}
      <div className="flex items-center gap-4 mb-4">
        <Select name="status" value={status || "all"} onValueChange={(v) => setQuery({ status: v, page: 1 })}>
          <SelectTrigger className="w-[160px]" aria-label="状态筛选">
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {ALL_STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {STATUS_LABEL[s]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="flex items-center gap-2">
          <Label htmlFor="filter-user-id" className="text-sm text-muted-foreground whitespace-nowrap">
            用户 ID
          </Label>
          <Input
            id="filter-user-id"
            className="w-[140px]"
            value={userIdInput}
            onChange={(e) => setUserIdInput(e.target.value)}
            onBlur={commitUserId}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitUserId();
            }}
            placeholder="例如 1234"
          />
        </div>
      </div>

      {/* 表格 */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>用户</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>地区</TableHead>
              <TableHead>本月用量</TableHead>
              <TableHead>购买时间</TableHead>
              <TableHead>到期日</TableHead>
              <TableHead>节点 IP</TableHead>
              <TableHead>订单 ID</TableHead>
              <TableHead>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length > 0 ? (
              items.map((item) => {
                const rowBusy = busyId === item.id;
                const level = quotaLevel(item.trafficUsedBytes, item.trafficTotalBytes);
                return (
                  <TableRow key={item.id}>
                    <TableCell className="font-mono">{item.id}</TableCell>
                    <TableCell>
                      <div>{item.email}</div>
                      <div className="text-xs text-muted-foreground">{item.userId}</div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={STATUS_VARIANT[item.status] || "outline"}>
                        {STATUS_LABEL[item.status] || item.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm">{item.region || "—"}</TableCell>
                    <TableCell className="text-sm">
                      <span
                        className={
                          level === "danger" ? "text-destructive" : level === "warn" ? "text-yellow-600" : undefined
                        }
                      >
                        {formatBytes(item.trafficUsedBytes)} / {formatBytes(item.trafficTotalBytes)}
                      </span>
                      {item.quotaExhausted && <div className="text-xs text-destructive">本月已用尽</div>}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">{formatDate(item.purchasedAt)}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      <div>{formatDate(item.expiresAt)}</div>
                      {item.status === "grace" && <div className="text-xs">宽限至 {formatDate(item.graceUntil)}</div>}
                      {item.status === "suspended" && (
                        <div className="text-xs">保留至 {formatDate(item.suspendUntil)}</div>
                      )}
                    </TableCell>
                    <TableCell className="text-sm font-mono">{item.boundIpv4 || "—"}</TableCell>
                    <TableCell className="font-mono text-sm">{item.orderId}</TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        <Button variant="outline" size="sm" asChild>
                          <Link href={`/manager/router-fulfillments?userId=${item.userId}`}>查看台账</Link>
                        </Button>
                        {canExtend(item.status) && (
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={rowBusy}
                            onClick={() => openExtendDialog(item)}
                          >
                            延期
                          </Button>
                        )}
                        {canStop(item.status) && (
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={rowBusy}
                            onClick={() => setStopTarget(item)}
                          >
                            停机
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={10} className="h-24 text-center">
                  暂无线路订阅
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {totalPages > 1 && (
        <Pagination
          currentPage={page}
          totalPages={totalPages}
          onPageChange={(p) => setQuery({ page: p })}
          className="mt-4"
        />
      )}

      {/* 延期对话框 */}
      <Dialog open={!!extendTarget} onOpenChange={(open) => !isExtending && !open && setExtendTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>延期</DialogTitle>
            <DialogDescription>
              仅超级管理员可操作；延期后线路立即恢复服务，已停机的节点需在「节点运维」手工开机。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label>月数</Label>
              <Select
                name="extendMonths"
                value={String(extendMonths)}
                onValueChange={(v) => setExtendMonths(Number(v))}
              >
                <SelectTrigger aria-label="延期月数">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EXTEND_MONTH_OPTIONS.map((m) => (
                    <SelectItem key={m} value={String(m)}>
                      {m} 个月
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="extend-reason">原因</Label>
              <Textarea
                id="extend-reason"
                value={extendReason}
                onChange={(e) => setExtendReason(e.target.value)}
                placeholder="必填，将记入操作日志与 Slack 通知"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setExtendTarget(null)} disabled={isExtending}>
              取消
            </Button>
            <Button onClick={handleExtend} disabled={isExtending || !extendReason.trim()}>
              {isExtending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  提交中...
                </>
              ) : (
                "确认延期"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 停机对话框 */}
      <Dialog open={!!stopTarget} onOpenChange={(open) => !isStopping && !open && setStopTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>停机</DialogTitle>
            <DialogDescription>会在节点运维队列创建停机任务，路由器将断网。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStopTarget(null)} disabled={isStopping}>
              取消
            </Button>
            <Button variant="destructive" onClick={handleStop} disabled={isStopping}>
              {isStopping ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  提交中...
                </>
              ) : (
                "确认停机"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
