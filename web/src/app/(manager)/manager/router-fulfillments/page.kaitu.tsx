"use client";

export const dynamic = "force-dynamic";

import { useCallback, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { Loader2, RefreshCw } from "lucide-react";
import { api, ApiError, type AdminRouterFulfillmentItem, type AdminRouterStats, type RouterStage } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { formatBytes, quotaLevel } from "@/lib/router-edition";
import { Pagination } from "@/components/Pagination";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { STAGE_LABEL, STAGE_VARIANT, canMint, canShip, isStuck } from "./router-fulfillment-helpers";

const PAGE_SIZE = 50;
const ALL_STAGES: RouterStage[] = ["paid", "provisioning", "ready", "shipped", "online", "expired"];
const NOTE_TRUNCATE_LEN = 20;

function formatDate(ts: number | undefined): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function truncateNote(note: string): string {
  if (note.length <= NOTE_TRUNCATE_LEN) return note;
  return `${note.slice(0, NOTE_TRUNCATE_LEN)}…`;
}

export default function RouterFulfillmentsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const page = Math.max(1, Number(searchParams.get("page") || "1"));
  const stage = searchParams.get("stage") || "";
  const userIdParam = Number(searchParams.get("userId") || "0") || undefined;

  const [items, setItems] = useState<AdminRouterFulfillmentItem[]>([]);
  const [total, setTotal] = useState(0);
  const [stats, setStats] = useState<AdminRouterStats | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [busyId, setBusyId] = useState<number | null>(null);

  const [userIdInput, setUserIdInput] = useState(userIdParam ? String(userIdParam) : "");
  useEffect(() => {
    setUserIdInput(userIdParam ? String(userIdParam) : "");
  }, [userIdParam]);

  // 发货对话框
  const [shipTarget, setShipTarget] = useState<AdminRouterFulfillmentItem | null>(null);
  const [shipCarrier, setShipCarrier] = useState("顺丰");
  const [shipTrackingNo, setShipTrackingNo] = useState("");
  const [shipNote, setShipNote] = useState("");
  const [isShipping, setIsShipping] = useState(false);

  // 代铸凭证对话框
  const [mintTarget, setMintTarget] = useState<AdminRouterFulfillmentItem | null>(null);
  const [isMinting, setIsMinting] = useState(false);
  const [mintedUrl, setMintedUrl] = useState<string | null>(null);

  // 备注对话框
  const [noteTarget, setNoteTarget] = useState<AdminRouterFulfillmentItem | null>(null);
  const [noteText, setNoteText] = useState("");
  const [isSavingNote, setIsSavingNote] = useState(false);

  const setQuery = useCallback(
    (patch: Record<string, string | number | undefined>) => {
      const next = new URLSearchParams(searchParams.toString());
      for (const [k, v] of Object.entries(patch)) {
        if (v === undefined || v === "" || v === "all") next.delete(k);
        else next.set(k, String(v));
      }
      router.push(`/manager/router-fulfillments?${next.toString()}`);
    },
    [router, searchParams]
  );

  const load = useCallback(async () => {
    setIsLoading(true);
    const [list, st] = await Promise.allSettled([
      api.listRouterFulfillments({ page, pageSize: PAGE_SIZE, stage: stage || undefined, userId: userIdParam }),
      api.getRouterStats(),
    ]);
    if (list.status === "fulfilled") {
      setItems(list.value.items || []);
      setTotal(list.value.pagination?.total ?? (list.value.items || []).length);
    } else {
      toast.error("获取路由器订单失败");
    }
    if (st.status === "fulfilled") setStats(st.value);
    else toast.error("获取看板数据失败");
    setIsLoading(false);
  }, [page, stage, userIdParam]);

  useEffect(() => {
    load();
  }, [load]);

  const commitUserId = () => {
    const trimmed = userIdInput.trim();
    const num = trimmed ? Number(trimmed) : undefined;
    setQuery({ userId: num && num > 0 ? num : undefined, page: 1 });
  };

  const openShipDialog = (item: AdminRouterFulfillmentItem) => {
    setShipCarrier("顺丰");
    setShipTrackingNo("");
    setShipNote("");
    setShipTarget(item);
  };

  const handleShip = async () => {
    if (!shipTarget) return;
    setBusyId(shipTarget.id);
    setIsShipping(true);
    try {
      const body: { trackingNo: string; carrier: string; note?: string } = {
        trackingNo: shipTrackingNo.trim(),
        carrier: shipCarrier.trim(),
      };
      if (shipNote.trim()) body.note = shipNote.trim();
      await api.shipRouterFulfillment(shipTarget.id, body);
      toast.success("已标记发货");
      setShipTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof ApiError ? getApiErrorMessageZh(e.code, "标记发货失败") : "标记发货失败");
    } finally {
      setIsShipping(false);
      setBusyId(null);
    }
  };

  const openMintDialog = (item: AdminRouterFulfillmentItem) => {
    setMintedUrl(null);
    setMintTarget(item);
  };

  const closeMintDialog = () => {
    const hadResult = !!mintedUrl;
    setMintTarget(null);
    setMintedUrl(null);
    if (hadResult) load();
  };

  const handleMintConfirm = async () => {
    if (!mintTarget) return;
    setBusyId(mintTarget.id);
    setIsMinting(true);
    try {
      const res = await api.mintRouterCredential(mintTarget.id);
      setMintedUrl(res.url);
    } catch (e) {
      toast.error(e instanceof ApiError ? getApiErrorMessageZh(e.code, "生成凭证失败") : "生成凭证失败");
    } finally {
      setIsMinting(false);
      setBusyId(null);
    }
  };

  const openNoteDialog = (item: AdminRouterFulfillmentItem) => {
    setNoteText(item.note || "");
    setNoteTarget(item);
  };

  const handleSaveNote = async () => {
    if (!noteTarget) return;
    setBusyId(noteTarget.id);
    setIsSavingNote(true);
    try {
      await api.updateRouterFulfillmentNote(noteTarget.id, noteText.trim());
      toast.success("备注已保存");
      setNoteTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof ApiError ? getApiErrorMessageZh(e.code, "保存备注失败") : "保存备注失败");
    } finally {
      setIsSavingNote(false);
      setBusyId(null);
    }
  };

  const nowSec = Math.floor(Date.now() / 1000);
  const totalPages = Math.ceil(total / PAGE_SIZE);

  const pendingCount = stats ? stats.stageCounts.paid + stats.stageCounts.provisioning + stats.stageCounts.ready : 0;

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-3xl font-bold">路由器订单</h1>
            <p className="text-muted-foreground">路由器版发货、代铸凭证与线路状态台账</p>
          </div>
          <Button onClick={load} disabled={isLoading}>
            {isLoading ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-2" />}
            刷新
          </Button>
        </div>

        {/* 看板 */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
          <Card
            data-testid="stat-pending"
            className="cursor-pointer"
            onClick={() => setQuery({ stage: "ready", page: 1 })}
          >
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">待处理</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{pendingCount}</div>
            </CardContent>
          </Card>
          <Card
            data-testid="stat-shipped"
            className="cursor-pointer"
            onClick={() => setQuery({ stage: "shipped", page: 1 })}
          >
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">已发货待上线</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.stageCounts.shipped ?? 0}</div>
            </CardContent>
          </Card>
          <Card data-testid="stat-stuck">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">卡住超 48 小时</CardTitle>
            </CardHeader>
            <CardContent>
              <div className={`text-2xl font-bold ${stats && stats.stuck > 0 ? "text-destructive" : ""}`}>
                {stats?.stuck ?? 0}
              </div>
            </CardContent>
          </Card>
          <Card
            data-testid="stat-online"
            className="cursor-pointer"
            onClick={() => setQuery({ stage: "online", page: 1 })}
          >
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">在线路由器</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.onlineRouters ?? 0}</div>
            </CardContent>
          </Card>
        </div>
        <div className="grid grid-cols-2 gap-4 mb-6">
          <Card data-testid="stat-expiring">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">30 天内到期</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.expiringSoon ?? 0}</div>
            </CardContent>
          </Card>
          <Card
            data-testid="stat-expired"
            className="cursor-pointer"
            onClick={() => setQuery({ stage: "expired", page: 1 })}
          >
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">已过期</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats?.stageCounts.expired ?? 0}</div>
            </CardContent>
          </Card>
        </div>

        {/* 筛选 */}
        <div className="flex items-center gap-4 mb-4">
          <Select value={stage || "all"} onValueChange={(v) => setQuery({ stage: v, page: 1 })}>
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder="阶段" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部阶段</SelectItem>
              {ALL_STAGES.map((s) => (
                <SelectItem key={s} value={s}>
                  {STAGE_LABEL[s]}
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
                <TableHead>客户</TableHead>
                <TableHead>机型</TableHead>
                <TableHead>阶段</TableHead>
                <TableHead>下单时间</TableHead>
                <TableHead>收货信息</TableHead>
                <TableHead>快递</TableHead>
                <TableHead>线路</TableHead>
                <TableHead>本月用量</TableHead>
                <TableHead>到期日</TableHead>
                <TableHead>路由器</TableHead>
                <TableHead>备注</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length > 0 ? (
                items.map((item) => {
                  const rowBusy = busyId === item.id;
                  const stuck = isStuck(item, nowSec);
                  const level = item.line ? quotaLevel(item.line.trafficUsedBytes, item.line.trafficTotalBytes) : "normal";
                  return (
                    <TableRow key={item.id}>
                      <TableCell className="font-mono">{item.id}</TableCell>
                      <TableCell>
                        <div>{item.email}</div>
                        <div className="text-xs text-muted-foreground">{item.userId}</div>
                      </TableCell>
                      <TableCell>
                        {item.hardwareSku ? item.hardwareSku : <Badge variant="secondary">自备</Badge>}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1">
                          <Badge variant={STAGE_VARIANT[item.stage]}>{STAGE_LABEL[item.stage]}</Badge>
                          {stuck && <Badge variant="destructive">卡住</Badge>}
                        </div>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">{formatDate(item.createdAt)}</TableCell>
                      <TableCell className="text-sm">
                        {item.shipping ? (
                          <>
                            <div>{item.shipping.name}</div>
                            <div className="text-muted-foreground">{item.shipping.phone}</div>
                            <div className="text-muted-foreground">{item.shipping.address}</div>
                          </>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell className="text-sm">
                        {item.carrier || item.trackingNo ? `${item.carrier || ""} ${item.trackingNo || ""}`.trim() : "—"}
                      </TableCell>
                      <TableCell className="text-sm">
                        {item.line ? `${item.line.status} · ${item.line.region}` : "—"}
                      </TableCell>
                      <TableCell className="text-sm">
                        {item.line ? (
                          <span
                            className={
                              level === "danger"
                                ? "text-destructive"
                                : level === "warn"
                                  ? "text-yellow-600"
                                  : undefined
                            }
                          >
                            {formatBytes(item.line.trafficUsedBytes)} / {formatBytes(item.line.trafficTotalBytes)}
                          </span>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {item.line ? formatDate(item.line.expiresAt) : "—"}
                      </TableCell>
                      <TableCell className="text-sm">
                        {item.device ? (
                          <div className="flex items-center gap-1.5">
                            <span
                              className={`inline-block h-2 w-2 rounded-full ${
                                item.device.online ? "bg-green-500" : "bg-gray-400"
                              }`}
                            />
                            <div>
                              <div>{item.device.online ? "在线" : "离线"}</div>
                              <div className="text-xs text-muted-foreground">{formatDate(item.device.lastSeenAt)}</div>
                            </div>
                          </div>
                        ) : (
                          <span className="text-muted-foreground">未激活</span>
                        )}
                      </TableCell>
                      <TableCell className="text-sm max-w-[140px]">
                        {item.note ? (
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="truncate block cursor-default">{truncateNote(item.note)}</span>
                            </TooltipTrigger>
                            <TooltipContent>
                              <p className="max-w-xs">{item.note}</p>
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {canShip(item) && (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy}
                              onClick={() => openShipDialog(item)}
                            >
                              发货
                            </Button>
                          )}
                          {canMint(item) && (
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={rowBusy}
                              onClick={() => openMintDialog(item)}
                            >
                              代铸凭证
                            </Button>
                          )}
                          <Button variant="outline" size="sm" disabled={rowBusy} onClick={() => openNoteDialog(item)}>
                            备注
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              ) : (
                <TableRow>
                  <TableCell colSpan={13} className="h-24 text-center">
                    暂无路由器订单
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

        {/* 发货对话框 */}
        <Dialog open={!!shipTarget} onOpenChange={(open) => !isShipping && !open && setShipTarget(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>标记发货</DialogTitle>
              <DialogDescription>为订单 #{shipTarget?.orderId} 填写快递信息。</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-2">
                <Label htmlFor="ship-carrier">快递公司</Label>
                <Input id="ship-carrier" value={shipCarrier} onChange={(e) => setShipCarrier(e.target.value)} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="ship-tracking">快递单号</Label>
                <Input
                  id="ship-tracking"
                  value={shipTrackingNo}
                  onChange={(e) => setShipTrackingNo(e.target.value)}
                  placeholder="必填"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="ship-note">备注（可选）</Label>
                <Textarea id="ship-note" value={shipNote} onChange={(e) => setShipNote(e.target.value)} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShipTarget(null)} disabled={isShipping}>
                取消
              </Button>
              <Button onClick={handleShip} disabled={isShipping || !shipTrackingNo.trim()}>
                {isShipping ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    提交中...
                  </>
                ) : (
                  "确认发货"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* 代铸凭证对话框 */}
        <Dialog open={!!mintTarget} onOpenChange={(open) => !isMinting && !open && closeMintDialog()}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>代铸凭证</DialogTitle>
              {!mintedUrl && (
                <DialogDescription>
                  会为该用户生成新的网关凭证。该用户已装好的路由器会立即断开，需要重新烧录。
                </DialogDescription>
              )}
            </DialogHeader>
            {mintedUrl ? (
              <div className="space-y-4 py-2">
                <div className="space-y-2">
                  <Label>订阅凭证 URL</Label>
                  <Textarea readOnly value={mintedUrl} rows={2} />
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => navigator.clipboard?.writeText(mintedUrl)}
                  >
                    复制 URL
                  </Button>
                </div>
                <div className="space-y-2">
                  <Label>烧录命令</Label>
                  <Textarea readOnly value={`k2r setup '${mintedUrl}'`} rows={2} />
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => navigator.clipboard?.writeText(`k2r setup '${mintedUrl}'`)}
                  >
                    复制命令
                  </Button>
                </div>
                <p className="text-xs text-destructive">关闭后无法再次查看，请确认已复制。</p>
                <DialogFooter>
                  <Button onClick={closeMintDialog}>关闭</Button>
                </DialogFooter>
              </div>
            ) : (
              <DialogFooter>
                <Button variant="outline" onClick={closeMintDialog} disabled={isMinting}>
                  取消
                </Button>
                <Button onClick={handleMintConfirm} disabled={isMinting}>
                  {isMinting ? (
                    <>
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                      生成中...
                    </>
                  ) : (
                    "确认生成"
                  )}
                </Button>
              </DialogFooter>
            )}
          </DialogContent>
        </Dialog>

        {/* 备注对话框 */}
        <Dialog open={!!noteTarget} onOpenChange={(open) => !isSavingNote && !open && setNoteTarget(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>编辑备注</DialogTitle>
              <DialogDescription>为订单 #{noteTarget?.orderId} 填写内部备注。</DialogDescription>
            </DialogHeader>
            <div className="py-2">
              <Textarea value={noteText} onChange={(e) => setNoteText(e.target.value)} rows={4} />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setNoteTarget(null)} disabled={isSavingNote}>
                取消
              </Button>
              <Button onClick={handleSaveNote} disabled={isSavingNote || !noteText.trim()}>
                {isSavingNote ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    保存中...
                  </>
                ) : (
                  "保存"
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}
