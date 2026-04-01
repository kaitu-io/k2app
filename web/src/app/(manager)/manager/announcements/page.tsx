"use client";

export const dynamic = "force-dynamic";

import { useState, useEffect, useCallback } from "react";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  type ColumnDef,
} from "@tanstack/react-table";
import { api, type AnnouncementResponse, type AnnouncementRequest } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Power, PowerOff } from "lucide-react";

function formatDate(ts: number): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function getStatusBadge(item: AnnouncementResponse) {
  if (item.isActive) {
    if (item.expiresAt > 0 && Date.now() / 1000 > item.expiresAt) {
      return <Badge variant="destructive">已过期</Badge>;
    }
    return <Badge className="bg-green-600">活跃</Badge>;
  }
  return <Badge variant="secondary">停用</Badge>;
}

const initialForm: AnnouncementRequest = {
  message: "",
  linkUrl: "",
  linkText: "",
  openMode: "external",
  authMode: "none",
  priority: 0,
  minVersion: "",
  maxVersion: "",
  expiresAt: 0,
};

export default function AnnouncementsPage() {
  const [announcements, setAnnouncements] = useState<AnnouncementResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20, total: 0 });

  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editingAnnouncement, setEditingAnnouncement] = useState<AnnouncementResponse | null>(null);
  const [form, setForm] = useState<AnnouncementRequest>({ ...initialForm });
  const [submitting, setSubmitting] = useState(false);

  const [activateTarget, setActivateTarget] = useState<AnnouncementResponse | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AnnouncementResponse | null>(null);

  const fetchAnnouncements = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.getAnnouncements({ page: pagination.page, pageSize: pagination.pageSize });
      setAnnouncements(res.items ?? []);
      if (res.pagination) {
        setPagination(prev => ({ ...prev, total: res.pagination!.total }));
      }
    } catch {
      toast.error("加载公告列表失败");
    } finally {
      setLoading(false);
    }
  }, [pagination.page, pagination.pageSize]);

  useEffect(() => {
    fetchAnnouncements();
  }, [fetchAnnouncements]);

  const resetForm = () => setForm({ ...initialForm });

  const handleCreate = async () => {
    if (!form.message.trim()) {
      toast.error("公告内容不能为空");
      return;
    }
    setSubmitting(true);
    try {
      await api.createAnnouncement(form);
      toast.success("公告创建成功");
      setCreateDialogOpen(false);
      resetForm();
      fetchAnnouncements();
    } catch {
      toast.error("创建失败");
    } finally {
      setSubmitting(false);
    }
  };

  const handleUpdate = async () => {
    if (!editingAnnouncement || !form.message.trim()) return;
    setSubmitting(true);
    try {
      await api.updateAnnouncement(editingAnnouncement.id, form);
      toast.success("公告更新成功");
      setEditDialogOpen(false);
      setEditingAnnouncement(null);
      resetForm();
      fetchAnnouncements();
    } catch {
      toast.error("更新失败");
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await api.deleteAnnouncement(deleteTarget.id);
      toast.success("公告已删除");
      setDeleteTarget(null);
      fetchAnnouncements();
    } catch {
      toast.error("删除失败");
    }
  };

  const handleActivate = async () => {
    if (!activateTarget) return;
    try {
      await api.activateAnnouncement(activateTarget.id);
      toast.success("公告已激活");
      setActivateTarget(null);
      fetchAnnouncements();
    } catch {
      toast.error("激活失败");
    }
  };

  const handleDeactivate = async (item: AnnouncementResponse) => {
    try {
      await api.deactivateAnnouncement(item.id);
      toast.success("公告已停用");
      fetchAnnouncements();
    } catch {
      toast.error("停用失败");
    }
  };

  const openEditDialog = (item: AnnouncementResponse) => {
    setEditingAnnouncement(item);
    setForm({
      message: item.message,
      linkUrl: item.linkUrl,
      linkText: item.linkText,
      openMode: item.openMode,
      authMode: item.authMode,
      priority: item.priority,
      minVersion: item.minVersion,
      maxVersion: item.maxVersion,
      expiresAt: item.expiresAt,
    });
    setEditDialogOpen(true);
  };

  const columns: ColumnDef<AnnouncementResponse>[] = [
    {
      accessorKey: "id",
      header: "ID",
      size: 60,
    },
    {
      accessorKey: "message",
      header: "公告内容",
      cell: ({ row }) => (
        <div className="max-w-[300px] truncate" title={row.original.message}>
          {row.original.message}
        </div>
      ),
    },
    {
      accessorKey: "openMode",
      header: "打开方式",
      size: 100,
      cell: ({ row }) => (
        <span>{row.original.openMode === "webview" ? "内部" : "外部"}</span>
      ),
    },
    {
      accessorKey: "authMode",
      header: "认证",
      size: 80,
      cell: ({ row }) => (
        <span>{row.original.authMode === "ott" ? "自动登录" : "无"}</span>
      ),
    },
    {
      accessorKey: "priority",
      header: "优先级",
      size: 80,
    },
    {
      id: "version",
      header: "版本范围",
      size: 120,
      cell: ({ row }) => {
        const { minVersion, maxVersion } = row.original;
        if (!minVersion && !maxVersion) return <span className="text-muted-foreground">全部</span>;
        return <span>{minVersion || "*"} ~ {maxVersion || "*"}</span>;
      },
    },
    {
      id: "status",
      header: "状态",
      size: 80,
      cell: ({ row }) => getStatusBadge(row.original),
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      size: 160,
      cell: ({ row }) => formatDate(row.original.createdAt),
    },
    {
      accessorKey: "expiresAt",
      header: "过期时间",
      size: 160,
      cell: ({ row }) => row.original.expiresAt ? formatDate(row.original.expiresAt) : "不过期",
    },
    {
      id: "actions",
      header: "操作",
      size: 200,
      cell: ({ row }) => {
        const item = row.original;
        return (
          <div className="flex gap-1">
            <Button variant="ghost" size="sm" onClick={() => openEditDialog(item)}>
              <Pencil className="h-4 w-4" />
            </Button>
            {item.isActive ? (
              <Button variant="ghost" size="sm" onClick={() => handleDeactivate(item)}>
                <PowerOff className="h-4 w-4" />
              </Button>
            ) : (
              <Button variant="ghost" size="sm" onClick={() => setActivateTarget(item)}>
                <Power className="h-4 w-4" />
              </Button>
            )}
            <Button variant="ghost" size="sm" onClick={() => setDeleteTarget(item)}>
              <Trash2 className="h-4 w-4 text-red-500" />
            </Button>
          </div>
        );
      },
    },
  ];

  const table = useReactTable({
    data: announcements,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  const renderForm = () => (
    <div className="grid gap-4 py-4">
      <div className="grid gap-2">
        <Label>公告内容 *</Label>
        <Textarea
          value={form.message}
          onChange={(e) => setForm({ ...form, message: e.target.value })}
          placeholder="输入公告文字内容（最多500字）"
          maxLength={500}
          rows={3}
        />
      </div>
      <div className="grid gap-2">
        <Label>链接地址</Label>
        <Input
          value={form.linkUrl ?? ""}
          onChange={(e) => setForm({ ...form, linkUrl: e.target.value })}
          placeholder="https://..."
        />
      </div>
      <div className="grid gap-2">
        <Label>链接文字</Label>
        <Input
          value={form.linkText ?? ""}
          onChange={(e) => setForm({ ...form, linkText: e.target.value })}
          placeholder="查看详情"
        />
      </div>
      <div className="grid gap-2">
        <Label>打开方式</Label>
        <Select
          value={form.openMode ?? "external"}
          onValueChange={(v) => setForm({ ...form, openMode: v })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="external">外部浏览器</SelectItem>
            <SelectItem value="webview">应用内打开</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-2">
        <Label>认证模式</Label>
        <Select
          value={form.authMode ?? "none"}
          onValueChange={(v) => setForm({ ...form, authMode: v })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">不需要登录</SelectItem>
            <SelectItem value="ott">自动登录</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-2">
        <Label>优先级</Label>
        <Input
          type="number"
          value={form.priority ?? 0}
          onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 0 })}
          placeholder="0"
        />
        <p className="text-xs text-muted-foreground">数字越大越优先显示</p>
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div className="grid gap-2">
          <Label>最低版本</Label>
          <Input
            value={form.minVersion ?? ""}
            onChange={(e) => setForm({ ...form, minVersion: e.target.value })}
            placeholder="0.4.2"
          />
        </div>
        <div className="grid gap-2">
          <Label>最高版本</Label>
          <Input
            value={form.maxVersion ?? ""}
            onChange={(e) => setForm({ ...form, maxVersion: e.target.value })}
            placeholder="0.4.3"
          />
        </div>
      </div>
      <div className="grid gap-2">
        <Label>过期时间</Label>
        <Input
          type="datetime-local"
          value={form.expiresAt ? new Date(form.expiresAt * 1000).toISOString().slice(0, 16) : ""}
          onChange={(e) => {
            const ts = e.target.value ? Math.floor(new Date(e.target.value).getTime() / 1000) : 0;
            setForm({ ...form, expiresAt: ts });
          }}
        />
        <p className="text-xs text-muted-foreground">留空表示不过期</p>
      </div>
    </div>
  );

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">公告管理</h1>
        <Button onClick={() => { resetForm(); setCreateDialogOpen(true); }}>
          <Plus className="h-4 w-4 mr-2" />
          创建公告
        </Button>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="text-center py-8">
                  加载中...
                </TableCell>
              </TableRow>
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="text-center py-8 text-muted-foreground">
                  暂无公告
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {pagination.total > pagination.pageSize && (
        <div className="flex items-center justify-between mt-4">
          <p className="text-sm text-muted-foreground">
            共 {pagination.total} 条
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page <= 1}
              onClick={() => setPagination(prev => ({ ...prev, page: prev.page - 1 }))}
            >
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page * pagination.pageSize >= pagination.total}
              onClick={() => setPagination(prev => ({ ...prev, page: prev.page + 1 }))}
            >
              下一页
            </Button>
          </div>
        </div>
      )}

      {/* Create Dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建公告</DialogTitle>
          </DialogHeader>
          {renderForm()}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>取消</Button>
            <Button onClick={handleCreate} disabled={submitting}>
              {submitting ? "创建中..." : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑公告</DialogTitle>
          </DialogHeader>
          {renderForm()}
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>取消</Button>
            <Button onClick={handleUpdate} disabled={submitting}>
              {submitting ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Activate Confirm */}
      <AlertDialog open={!!activateTarget} onOpenChange={(open) => !open && setActivateTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认激活</AlertDialogTitle>
            <AlertDialogDescription>
              确认激活此公告？激活后可与其他公告同时显示。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleActivate}>确认激活</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete Confirm */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除此公告吗？此操作不可恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-red-600 hover:bg-red-700">确认删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
