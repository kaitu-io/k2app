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
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { api, LicenseKeyAdmin } from "@/lib/api";
import { toast } from "sonner";
import { Trash2, Copy } from "lucide-react";

function formatDate(ts: number) {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function getStatus(key: LicenseKeyAdmin): { label: string; variant: "default" | "secondary" | "destructive" } {
  if (key.isUsed) return { label: "已使用", variant: "secondary" };
  if (key.expiresAt < Date.now() / 1000) return { label: "已过期", variant: "destructive" };
  return { label: "未使用", variant: "default" };
}

export default function LicenseKeysPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [data, setData] = useState<LicenseKeyAdmin[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const page = parseInt(searchParams.get("page") || "1", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "50", 10);
  const batchIdParam = searchParams.get("batchId") || "";
  const isUsedParam = searchParams.get("isUsed") || "";

  const [localBatchId, setLocalBatchId] = useState(batchIdParam);
  const [localIsUsed, setLocalIsUsed] = useState(isUsedParam);

  const columns: ColumnDef<LicenseKeyAdmin>[] = [
    {
      accessorKey: "code", header: "授权码",
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          <code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">{row.original.code}</code>
          <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => { navigator.clipboard.writeText(row.original.code); toast.success("已复制"); }}><Copy className="h-3 w-3" /></Button>
        </div>
      ),
    },
    { accessorKey: "batchId", header: "批次ID", cell: ({ row }) => <code className="text-xs bg-muted px-1 py-0.5 rounded">{row.original.batchId}</code> },
    { accessorKey: "planDays", header: "天数", cell: ({ row }) => `${row.original.planDays} 天` },
    { header: "状态", cell: ({ row }) => { const s = getStatus(row.original); return <Badge variant={s.variant}>{s.label}</Badge>; } },
    { accessorKey: "expiresAt", header: "过期时间", cell: ({ row }) => formatDate(row.original.expiresAt) },
    { accessorKey: "createdAt", header: "创建时间", cell: ({ row }) => formatDate(row.original.createdAt) },
    {
      id: "actions", header: "操作",
      cell: ({ row }) => (<Button variant="ghost" size="sm" onClick={() => { setDeletingId(row.original.id); setDeleteOpen(true); }}><Trash2 className="h-4 w-4" /></Button>),
    },
  ];

  const table = useReactTable({ data, columns, pageCount, getCoreRowModel: getCoreRowModel(), manualPagination: true });

  const fetchData = useCallback(async () => {
    setIsLoading(true);
    try {
      const params: { page: number; pageSize: number; batchId?: number; isUsed?: boolean } = { page, pageSize };
      if (batchIdParam) params.batchId = parseInt(batchIdParam, 10);
      if (isUsedParam !== "") params.isUsed = isUsedParam === "true";
      const res = await api.listAdminLicenseKeys(params);
      setData(res.items || []);
      setTotal(res.total);
      setPageCount(Math.ceil(res.total / pageSize));
    } catch { toast.error("获取授权码列表失败"); }
    finally { setIsLoading(false); }
  }, [page, pageSize, batchIdParam, isUsedParam]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleFilter = () => {
    const p = new URLSearchParams();
    p.set("page", "1"); p.set("pageSize", pageSize.toString());
    if (localBatchId.trim()) p.set("batchId", localBatchId.trim());
    if (localIsUsed && localIsUsed !== "all") p.set("isUsed", localIsUsed);
    router.push(`/manager/license-keys?${p.toString()}`);
  };

  const handleDelete = async () => {
    if (!deletingId) return;
    try { await api.deleteAdminLicenseKey(deletingId); toast.success("已删除"); setDeleteOpen(false); setDeletingId(null); fetchData(); }
    catch { toast.error("删除失败"); }
  };

  return (
    <div className="space-y-6">
      <div><h1 className="text-3xl font-bold">授权码列表</h1><p className="text-muted-foreground">查看所有授权码。批次管理请到「授权码批次」页面。</p></div>

      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1"><label className="text-sm font-medium">批次ID</label><Input placeholder="输入批次ID" value={localBatchId} onChange={e => setLocalBatchId(e.target.value)} /></div>
        <div className="flex-1"><label className="text-sm font-medium">状态</label><select className="w-full p-2 border border-border bg-muted text-foreground rounded-md" value={localIsUsed} onChange={e => setLocalIsUsed(e.target.value)}><option value="">全部</option><option value="true">已使用</option><option value="false">未使用</option></select></div>
        <div className="flex gap-2"><Button onClick={handleFilter}>筛选</Button><Button variant="outline" onClick={() => { setLocalBatchId(""); setLocalIsUsed(""); router.push("/manager/license-keys"); }}>重置</Button></div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>{table.getHeaderGroups().map(hg => (<TableRow key={hg.id}>{hg.headers.map(h => (<TableHead key={h.id}>{h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext())}</TableHead>))}</TableRow>))}</TableHeader>
          <TableBody>
            {isLoading ? (<TableRow><TableCell colSpan={columns.length} className="h-24 text-center"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" /></TableCell></TableRow>)
            : table.getRowModel().rows?.length ? table.getRowModel().rows.map(row => (<TableRow key={row.id}>{row.getVisibleCells().map(cell => (<TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>))}</TableRow>))
            : (<TableRow><TableCell colSpan={columns.length} className="h-24 text-center">无数据</TableCell></TableRow>)}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">总计: {total}</span>
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => { const p = new URLSearchParams(searchParams.toString()); p.set("page", String(page - 1)); router.push(`/manager/license-keys?${p}`); }}>上一页</Button>
        <Button variant="outline" size="sm" disabled={page >= pageCount} onClick={() => { const p = new URLSearchParams(searchParams.toString()); p.set("page", String(page + 1)); router.push(`/manager/license-keys?${p}`); }}>下一页</Button>
      </div>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent><DialogHeader><DialogTitle>确认删除</DialogTitle><DialogDescription>确定要删除这个授权码吗？</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button><Button variant="destructive" onClick={handleDelete}>删除</Button></DialogFooter></DialogContent>
      </Dialog>
    </div>
  );
}
