"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ColumnDef, flexRender, getCoreRowModel, useReactTable,
} from "@tanstack/react-table";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import {
  api, LicenseKeyBatch, LicenseKeyBatchDetail, LicenseKeyItem,
  CreateLicenseKeyBatchRequest, BatchStats,
} from "@/lib/api";
import { toast } from "sonner";
import { Plus, Trash2, Copy, Eye } from "lucide-react";

function formatDate(ts: number) {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function pct(n: number) {
  return (n * 100).toFixed(1) + "%";
}

export default function LicenseKeyBatchesPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [batches, setBatches] = useState<LicenseKeyBatch[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [stats, setStats] = useState<BatchStats[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<CreateLicenseKeyBatchRequest>({
    name: "", sourceTag: "", recipientMatcher: "all", planDays: 30, quantity: 100, expiresInDays: 30,
  });
  const [isCreating, setIsCreating] = useState(false);

  const [detailOpen, setDetailOpen] = useState(false);
  const [detail, setDetail] = useState<LicenseKeyBatchDetail | null>(null);
  const [detailKeys, setDetailKeys] = useState<LicenseKeyItem[]>([]);
  const [detailKeysTotal, setDetailKeysTotal] = useState(0);
  const [detailKeyStatus, setDetailKeyStatus] = useState("all");
  const [detailKeyPage, setDetailKeyPage] = useState(1);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const page = parseInt(searchParams.get("page") || "1", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "20", 10);

  const columns: ColumnDef<LicenseKeyBatch>[] = [
    { accessorKey: "name", header: "批次名称" },
    { accessorKey: "sourceTag", header: "渠道", cell: ({ row }) => row.original.sourceTag || <span className="text-muted-foreground">-</span> },
    {
      accessorKey: "quantity", header: "兑换/总量",
      cell: ({ row }) => {
        const b = row.original;
        return <span>{b.redeemedCount}<span className="text-muted-foreground">/{b.quantity}</span></span>;
      },
    },
    { accessorKey: "planDays", header: "天数", cell: ({ row }) => `${row.original.planDays} 天` },
    {
      accessorKey: "recipientMatcher", header: "限制",
      cell: ({ row }) => row.original.recipientMatcher === "never_paid"
        ? <Badge variant="secondary">未付费</Badge>
        : <Badge variant="default">全部</Badge>,
    },
    { accessorKey: "expiresAt", header: "过期", cell: ({ row }) => formatDate(row.original.expiresAt) },
    { accessorKey: "createdAt", header: "创建", cell: ({ row }) => formatDate(row.original.createdAt) },
    {
      id: "actions", header: "操作",
      cell: ({ row }) => (
        <div className="flex gap-1">
          <Button variant="ghost" size="sm" onClick={() => openDetail(row.original.id)}><Eye className="h-4 w-4" /></Button>
          <Button variant="ghost" size="sm" onClick={() => { setDeletingId(row.original.id); setDeleteOpen(true); }}><Trash2 className="h-4 w-4" /></Button>
        </div>
      ),
    },
  ];

  const table = useReactTable({ data: batches, columns, pageCount, getCoreRowModel: getCoreRowModel(), manualPagination: true });

  const fetchBatches = useCallback(async () => {
    setIsLoading(true);
    try {
      const res = await api.listLicenseKeyBatches({ page, pageSize });
      setBatches(res.items || []);
      setTotal(res.total);
      setPageCount(Math.ceil(res.total / pageSize));
    } catch { toast.error("获取批次列表失败"); }
    finally { setIsLoading(false); }
  }, [page, pageSize]);

  const fetchStats = useCallback(async () => {
    try { setStats(await api.getLicenseKeyBatchStats()); } catch {}
  }, []);

  useEffect(() => { fetchBatches(); }, [fetchBatches]);
  useEffect(() => { fetchStats(); }, [fetchStats]);

  const handleCreate = async () => {
    setIsCreating(true);
    try {
      await api.createLicenseKeyBatch(createForm);
      setCreateOpen(false);
      setCreateForm({ name: "", sourceTag: "", recipientMatcher: "all", planDays: 30, quantity: 100, expiresInDays: 30 });
      toast.success("批次创建已提交（等待审批）");
      fetchBatches();
      fetchStats();
    } catch { toast.error("创建批次失败"); }
    finally { setIsCreating(false); }
  };

  const handleDelete = async () => {
    if (!deletingId) return;
    try {
      await api.deleteLicenseKeyBatch(deletingId);
      toast.success("删除已提交");
      setDeleteOpen(false);
      setDeletingId(null);
      fetchBatches();
      fetchStats();
    } catch { toast.error("删除失败"); }
  };

  const openDetail = async (id: number) => {
    try {
      const d = await api.getLicenseKeyBatch(id);
      setDetail(d);
      setDetailKeyPage(1);
      setDetailKeyStatus("all");
      const keys = await api.listLicenseKeyBatchKeys(id, { page: 1, pageSize: 50 });
      setDetailKeys(keys.items || []);
      setDetailKeysTotal(keys.total);
      setDetailOpen(true);
    } catch { toast.error("获取详情失败"); }
  };

  const fetchDetailKeys = useCallback(async () => {
    if (!detail) return;
    try {
      const keys = await api.listLicenseKeyBatchKeys(detail.id, {
        status: detailKeyStatus === "all" ? undefined : detailKeyStatus,
        page: detailKeyPage, pageSize: 50,
      });
      setDetailKeys(keys.items || []);
      setDetailKeysTotal(keys.total);
    } catch {}
  }, [detail, detailKeyStatus, detailKeyPage]);

  useEffect(() => { if (detailOpen && detail) fetchDetailKeys(); }, [detailOpen, detail, fetchDetailKeys]);

  const totalKeys = stats.reduce((s, r) => s + r.totalKeys, 0);
  const totalRedeemed = stats.reduce((s, r) => s + r.redeemed, 0);
  const totalConverted = stats.reduce((s, r) => s + r.convertedUsers, 0);
  const overallRedeemRate = totalKeys > 0 ? totalRedeemed / totalKeys : 0;
  const overallConvRate = totalRedeemed > 0 ? totalConverted / totalRedeemed : 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">授权码批次</h1>
          <p className="text-muted-foreground">管理授权码批次、查看转化统计</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4 mr-2" />创建批次</Button>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">总 Keys</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{totalKeys}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">已兑换</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{totalRedeemed}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">兑换率</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{pct(overallRedeemRate)}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">兑换→付费</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{pct(overallConvRate)}</div><p className="text-xs text-muted-foreground">{totalConverted} 人</p></CardContent></Card>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>{table.getHeaderGroups().map(hg => (<TableRow key={hg.id}>{hg.headers.map(h => (<TableHead key={h.id}>{h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext())}</TableHead>))}</TableRow>))}</TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow><TableCell colSpan={columns.length} className="h-24 text-center"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" /></TableCell></TableRow>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map(row => (<TableRow key={row.id}>{row.getVisibleCells().map(cell => (<TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>))}</TableRow>))
            ) : (
              <TableRow><TableCell colSpan={columns.length} className="h-24 text-center">无数据</TableCell></TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">总计: {total}</span>
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => router.push(`/manager/license-key-batches?page=${page - 1}&pageSize=${pageSize}`)}>上一页</Button>
        <Button variant="outline" size="sm" disabled={page >= pageCount} onClick={() => router.push(`/manager/license-key-batches?page=${page + 1}&pageSize=${pageSize}`)}>下一页</Button>
      </div>

      {/* Create dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>创建授权码批次</DialogTitle><DialogDescription>创建后需审批，审批通过自动生成授权码</DialogDescription></DialogHeader>
          <div className="space-y-4 py-2">
            <div><label className="text-sm font-medium block mb-1">批次名称</label><Input value={createForm.name} onChange={e => setCreateForm(f => ({ ...f, name: e.target.value }))} placeholder="Apr Twitter 投放" /></div>
            <div><label className="text-sm font-medium block mb-1">渠道标签</label><Input value={createForm.sourceTag} onChange={e => setCreateForm(f => ({ ...f, sourceTag: e.target.value }))} placeholder="twitter / kol-xxx / winback" /></div>
            <div className="grid grid-cols-2 gap-4">
              <div><label className="text-sm font-medium block mb-1">数量 (1-10000)</label><Input type="number" min={1} max={10000} value={createForm.quantity} onChange={e => setCreateForm(f => ({ ...f, quantity: parseInt(e.target.value) || 100 }))} /></div>
              <div><label className="text-sm font-medium block mb-1">天数</label><Input type="number" min={1} value={createForm.planDays} onChange={e => setCreateForm(f => ({ ...f, planDays: parseInt(e.target.value) || 30 }))} /></div>
            </div>
            <div><label className="text-sm font-medium block mb-1">有效期（天）</label><Input type="number" min={1} value={createForm.expiresInDays} onChange={e => setCreateForm(f => ({ ...f, expiresInDays: parseInt(e.target.value) || 30 }))} /></div>
            <div><label className="text-sm font-medium block mb-1">使用条件</label><select className="w-full p-2 border border-border bg-background text-foreground rounded-md" value={createForm.recipientMatcher} onChange={e => setCreateForm(f => ({ ...f, recipientMatcher: e.target.value }))}><option value="all">所有用户</option><option value="never_paid">未付费用户</option></select></div>
            <div><label className="text-sm font-medium block mb-1">备注</label><Input value={createForm.note || ""} onChange={e => setCreateForm(f => ({ ...f, note: e.target.value }))} placeholder="可选" /></div>
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button onClick={handleCreate} disabled={isCreating || !createForm.name}>{isCreating ? "提交中..." : "提交审批"}</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Detail dialog */}
      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          {detail && (<>
            <DialogHeader><DialogTitle>{detail.name}</DialogTitle><DialogDescription>渠道: {detail.sourceTag || "-"} · {detail.quantity} 个 · {detail.planDays} 天</DialogDescription></DialogHeader>
            <div className="grid grid-cols-3 gap-3 py-2">
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">兑换率</div><div className="text-xl font-bold">{pct(detail.quantity > 0 ? detail.redeemedCount / detail.quantity : 0)}</div><div className="text-xs text-muted-foreground">{detail.redeemedCount}/{detail.quantity}</div></CardContent></Card>
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">转化率</div><div className="text-xl font-bold">{pct(detail.conversionRate)}</div><div className="text-xs text-muted-foreground">{detail.convertedUsers} 人付费</div></CardContent></Card>
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">收入</div><div className="text-xl font-bold">¥{(detail.revenue / 100).toFixed(2)}</div></CardContent></Card>
            </div>
            <div className="flex items-center gap-2 py-2">
              <select className="p-1 border border-border bg-background text-foreground rounded text-sm" value={detailKeyStatus} onChange={e => { setDetailKeyStatus(e.target.value); setDetailKeyPage(1); }}>
                <option value="all">全部</option><option value="used">已使用</option><option value="unused">未使用</option><option value="expired">已过期</option>
              </select>
              <span className="text-sm text-muted-foreground">共 {detailKeysTotal} 条</span>
            </div>
            <div className="rounded-md border max-h-64 overflow-y-auto">
              <Table>
                <TableHeader><TableRow><TableHead>授权码</TableHead><TableHead>状态</TableHead><TableHead>过期</TableHead><TableHead>使用者</TableHead></TableRow></TableHeader>
                <TableBody>
                  {detailKeys.map(k => (
                    <TableRow key={k.id}>
                      <TableCell><div className="flex items-center gap-1"><code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">{k.code}</code><Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => { navigator.clipboard.writeText(k.code); toast.success("已复制"); }}><Copy className="h-3 w-3" /></Button></div></TableCell>
                      <TableCell>{k.isUsed ? <Badge variant="secondary">已使用</Badge> : k.expiresAt < Date.now()/1000 ? <Badge variant="destructive">已过期</Badge> : <Badge>未使用</Badge>}</TableCell>
                      <TableCell className="text-xs">{formatDate(k.expiresAt)}</TableCell>
                      <TableCell className="text-xs">{k.usedByUserId || "-"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {detailKeysTotal > 50 && (
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" disabled={detailKeyPage <= 1} onClick={() => setDetailKeyPage(p => p - 1)}>上一页</Button>
                <Button variant="outline" size="sm" disabled={detailKeyPage * 50 >= detailKeysTotal} onClick={() => setDetailKeyPage(p => p + 1)}>下一页</Button>
              </div>
            )}
          </>)}
        </DialogContent>
      </Dialog>

      {/* Delete dialog */}
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>确认删除</DialogTitle><DialogDescription>将删除批次及其未使用的授权码。已使用的授权码保留。</DialogDescription></DialogHeader>
          <DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button><Button variant="destructive" onClick={handleDelete}>删除</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
