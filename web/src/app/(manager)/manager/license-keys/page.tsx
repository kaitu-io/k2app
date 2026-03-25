"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, LicenseKeyAdmin, LicenseKeyStatsRow, CreateLicenseKeysRequest } from "@/lib/api";
import { toast } from "sonner";
import { Trash2, Key, Copy, Plus } from "lucide-react";

function getStatus(key: LicenseKeyAdmin): { label: string; variant: "default" | "secondary" | "destructive" } {
  if (key.isUsed) {
    return { label: "已使用", variant: "secondary" };
  }
  if (key.expiresAt < Date.now() / 1000) {
    return { label: "已过期", variant: "destructive" };
  }
  return { label: "未使用", variant: "default" };
}

function formatDate(timestamp: number): string {
  if (!timestamp) return "-";
  return new Date(timestamp * 1000).toLocaleString("zh-CN");
}

function getSourceBadge(source: string) {
  if (source === "campaign") {
    return <Badge variant="secondary">活动</Badge>;
  }
  return <Badge variant="default">手动</Badge>;
}

export default function LicenseKeysPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<LicenseKeyAdmin[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [stats, setStats] = useState<LicenseKeyStatsRow[]>([]);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  // Create dialog state
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [createForm, setCreateForm] = useState<CreateLicenseKeysRequest>({
    count: 10,
    planDays: 30,
    expiresInDays: 30,
    recipientMatcher: "all",
    note: "",
  });
  const [isCreating, setIsCreating] = useState(false);

  // Result dialog state
  const [resultDialogOpen, setResultDialogOpen] = useState(false);
  const [createdCodes, setCreatedCodes] = useState<string[]>([]);

  // URL query state
  const page = parseInt(searchParams.get("page") || "0", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "50", 10);
  const campaignIdParam = searchParams.get("campaignId") || "";
  const isUsedParam = searchParams.get("isUsed") || "";
  const sourceParam = searchParams.get("source") || "";

  // Local filter state
  const [localCampaignId, setLocalCampaignId] = useState(campaignIdParam);
  const [localIsUsed, setLocalIsUsed] = useState(isUsedParam);
  const [localSource, setLocalSource] = useState(sourceParam);

  const columns: ColumnDef<LicenseKeyAdmin>[] = [
    {
      accessorKey: "code",
      header: "授权码",
      cell: ({ row }) => {
        const code = row.getValue("code") as string;
        return (
          <div className="flex items-center gap-1">
            <code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">
              {code}
            </code>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              onClick={() => {
                navigator.clipboard.writeText(code);
                toast.success("已复制授权码");
              }}
            >
              <Copy className="h-3 w-3" />
            </Button>
          </div>
        );
      },
    },
    {
      accessorKey: "planDays",
      header: "天数",
      cell: ({ row }) => (
        <span className="font-medium">{row.getValue("planDays")} 天</span>
      ),
    },
    {
      accessorKey: "source",
      header: "来源",
      cell: ({ row }) => getSourceBadge(row.getValue("source") as string),
    },
    {
      accessorKey: "note",
      header: "备注",
      cell: ({ row }) => {
        const note = row.original.note;
        if (!note) return <span className="text-muted-foreground">-</span>;
        return (
          <span className="text-sm max-w-[120px] truncate block" title={note}>
            {note}
          </span>
        );
      },
    },
    {
      header: "状态",
      cell: ({ row }) => {
        const { label, variant } = getStatus(row.original);
        return <Badge variant={variant}>{label}</Badge>;
      },
    },
    {
      accessorKey: "expiresAt",
      header: "过期时间",
      cell: ({ row }) => formatDate(row.getValue("expiresAt")),
    },
    {
      accessorKey: "campaignId",
      header: "关联活动ID",
      cell: ({ row }) => {
        const id = row.original.campaignId;
        return id ? (
          <code className="text-xs bg-muted px-1 py-0.5 rounded">{id}</code>
        ) : (
          <span className="text-muted-foreground">-</span>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      cell: ({ row }) => formatDate(row.getValue("createdAt")),
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setDeletingId(row.original.id);
            setDeleteDialogOpen(true);
          }}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      ),
    },
  ];

  const table = useReactTable({
    data,
    columns,
    pageCount,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  });

  const fetchData = useCallback(async () => {
    setIsLoading(true);
    try {
      const params: { page: number; pageSize: number; campaignId?: number; isUsed?: boolean; source?: string } = {
        page,
        pageSize,
      };
      if (campaignIdParam) {
        params.campaignId = parseInt(campaignIdParam, 10);
      }
      if (isUsedParam !== "") {
        params.isUsed = isUsedParam === "true";
      }
      if (sourceParam !== "") {
        params.source = sourceParam;
      }
      const response = await api.listAdminLicenseKeys(params);
      setData(response.items || []);
      setTotal(response.total);
      setPageCount(Math.ceil(response.total / pageSize));
    } catch (error) {
      toast.error("获取授权码列表失败");
      console.error("Failed to fetch license keys:", error);
    } finally {
      setIsLoading(false);
    }
  }, [page, pageSize, campaignIdParam, isUsedParam, sourceParam]);

  const fetchStats = useCallback(async () => {
    try {
      const result = await api.getLicenseKeyStats();
      setStats(result || []);
    } catch (error) {
      console.error("Failed to fetch stats:", error);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  const handleFilter = () => {
    const params = new URLSearchParams();
    params.set("page", "0");
    params.set("pageSize", pageSize.toString());
    if (localCampaignId.trim()) {
      params.set("campaignId", localCampaignId.trim());
    }
    if (localIsUsed && localIsUsed !== "all") {
      params.set("isUsed", localIsUsed);
    }
    if (localSource && localSource !== "all") {
      params.set("source", localSource);
    }
    router.push(`/manager/license-keys?${params.toString()}`);
  };

  const handleReset = () => {
    setLocalCampaignId("");
    setLocalIsUsed("");
    setLocalSource("");
    router.push("/manager/license-keys");
  };

  const handleDelete = async () => {
    if (deletingId === null) return;
    try {
      await api.deleteAdminLicenseKey(deletingId);
      toast.success("授权码已删除");
      setDeleteDialogOpen(false);
      setDeletingId(null);
      fetchData();
      fetchStats();
    } catch (error) {
      toast.error("删除授权码失败");
      console.error("Failed to delete license key:", error);
    }
  };

  const handleCreate = async () => {
    setIsCreating(true);
    try {
      const req: CreateLicenseKeysRequest = {
        ...createForm,
        note: createForm.note || undefined,
      };
      const response = await api.createAdminLicenseKeys(req);
      const codes = response.keys.map((k) => k.code);
      setCreatedCodes(codes);
      setCreateDialogOpen(false);
      setResultDialogOpen(true);
      fetchData();
      fetchStats();
      toast.success(`已创建 ${codes.length} 个授权码`);
    } catch (error) {
      toast.error("创建授权码失败");
      console.error("Failed to create license keys:", error);
    } finally {
      setIsCreating(false);
    }
  };

  const handleBulkCopy = () => {
    navigator.clipboard.writeText(createdCodes.join("\n"));
    toast.success("已批量复制所有授权码");
  };

  // Aggregate stats
  const totalKeys = stats.reduce((sum, r) => sum + r.total, 0);
  const totalUsed = stats.reduce((sum, r) => sum + r.used, 0);
  const totalExpired = stats.reduce((sum, r) => sum + r.expired, 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">{"授权码管理"}</h1>
          <p className="text-muted-foreground">{"管理系统中的所有授权码"}</p>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          {"创建授权码"}
        </Button>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{"总计"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalKeys}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{"已使用"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalUsed}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{"已过期"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalExpired}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">{"关联活动数"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.length}</div>
          </CardContent>
        </Card>
      </div>

      {/* Per-campaign breakdown */}
      {stats.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">{"按活动统计"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {stats.map((row, i) => (
                <div key={i} className="flex items-center gap-2 text-sm">
                  <Key className="h-3 w-3 text-muted-foreground" />
                  <span className="text-muted-foreground">
                    {row.campaignId ? `活动 #${row.campaignId}` : "无活动"}
                  </span>
                  <span className="font-medium">{row.total}</span>
                  <span className="text-muted-foreground text-xs">
                    ({row.used} 已用 / {row.expired} 过期)
                  </span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Filters */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1">
          <label className="text-sm font-medium">{"活动ID"}</label>
          <Input
            placeholder="输入活动ID"
            value={localCampaignId}
            onChange={(e) => setLocalCampaignId(e.target.value)}
          />
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"使用状态"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localIsUsed}
            onChange={(e) => setLocalIsUsed(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="true">{"已使用"}</option>
            <option value="false">{"未使用"}</option>
          </select>
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"来源"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localSource}
            onChange={(e) => setLocalSource(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="manual">{"手动"}</option>
            <option value="campaign">{"活动"}</option>
          </select>
        </div>
        <div className="flex gap-2">
          <Button onClick={handleFilter}>{"筛选"}</Button>
          <Button variant="outline" onClick={handleReset}>{"重置"}</Button>
        </div>
      </div>

      {/* Table */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  <div className="flex items-center justify-center">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                  </div>
                </TableCell>
              </TableRow>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} data-state={row.getIsSelected() && "selected"}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  {"无结果."}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">{"总计: "}{total}</span>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            const params = new URLSearchParams(searchParams.toString());
            params.set("page", (page - 1).toString());
            router.push(`/manager/license-keys?${params.toString()}`);
          }}
          disabled={page === 0}
        >
          {"上一页"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            const params = new URLSearchParams(searchParams.toString());
            params.set("page", (page + 1).toString());
            router.push(`/manager/license-keys?${params.toString()}`);
          }}
          disabled={page >= pageCount - 1}
        >
          {"下一页"}
        </Button>
      </div>

      {/* Delete confirmation dialog */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"确认删除"}</DialogTitle>
            <DialogDescription>{"确定要删除这个授权码吗？此操作不可撤销。"}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button variant="destructive" onClick={handleDelete}>
              {"删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"创建授权码"}</DialogTitle>
            <DialogDescription>{"批量创建新授权码"}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium block mb-1">{"数量 (1-100)"}</label>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={createForm.count}
                  onChange={(e) =>
                    setCreateForm((f) => ({ ...f, count: parseInt(e.target.value, 10) || 1 }))
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium block mb-1">{"天数"}</label>
                <Input
                  type="number"
                  min={1}
                  value={createForm.planDays}
                  onChange={(e) =>
                    setCreateForm((f) => ({ ...f, planDays: parseInt(e.target.value, 10) || 30 }))
                  }
                />
              </div>
            </div>
            <div>
              <label className="text-sm font-medium block mb-1">{"有效期（天）"}</label>
              <Input
                type="number"
                min={1}
                value={createForm.expiresInDays}
                onChange={(e) =>
                  setCreateForm((f) => ({ ...f, expiresInDays: parseInt(e.target.value, 10) || 30 }))
                }
              />
            </div>
            <div>
              <label className="text-sm font-medium block mb-1">{"使用条件"}</label>
              <select
                className="w-full p-2 border border-border bg-background text-foreground rounded-md"
                value={createForm.recipientMatcher}
                onChange={(e) =>
                  setCreateForm((f) => ({ ...f, recipientMatcher: e.target.value }))
                }
              >
                <option value="all">{"所有用户"}</option>
                <option value="never_paid">{"未付费用户"}</option>
              </select>
            </div>
            <div>
              <label className="text-sm font-medium block mb-1">{"备注"}</label>
              <Input
                placeholder="可选"
                value={createForm.note || ""}
                onChange={(e) => setCreateForm((f) => ({ ...f, note: e.target.value }))}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button onClick={handleCreate} disabled={isCreating}>
              {isCreating ? "创建中..." : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Result dialog */}
      <Dialog open={resultDialogOpen} onOpenChange={setResultDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{"创建成功"}</DialogTitle>
            <DialogDescription>
              {"已生成 "}{createdCodes.length}{" 个授权码"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <div className="max-h-64 overflow-y-auto rounded-md border p-2 space-y-1">
              {createdCodes.map((code) => (
                <div key={code} className="flex items-center justify-between gap-2 py-0.5">
                  <code className="text-xs font-mono bg-muted px-1.5 py-0.5 rounded flex-1">
                    {code}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0 shrink-0"
                    onClick={() => {
                      navigator.clipboard.writeText(code);
                      toast.success("已复制");
                    }}
                  >
                    <Copy className="h-3 w-3" />
                  </Button>
                </div>
              ))}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={handleBulkCopy}>
              <Copy className="h-4 w-4 mr-2" />
              {"批量复制"}
            </Button>
            <Button onClick={() => setResultDialogOpen(false)}>{"关闭"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
