"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
  getPaginationRowModel,
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
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Card, CardContent } from "@/components/ui/card";
import { api, AdminApproval } from "@/lib/api";
import { useAuth } from "@/contexts/AuthContext";
import { toast } from "sonner";
import { ChevronDown, Check, X, Ban } from "lucide-react";

const actionNames: Record<string, string> = {
  edm_create_task: "创建 EDM 邮件任务",
  campaign_create: "创建优惠活动",
  campaign_update: "修改优惠活动",
  campaign_delete: "删除优惠活动",
  campaign_issue_keys: "发放 License Key",
  user_hard_delete: "硬删除用户",
  plan_update: "修改订阅套餐",
  plan_delete: "删除订阅套餐",
  withdraw_approve: "审批提现",
  withdraw_complete: "完成提现",
};

const statusConfig: Record<string, { label: string; variant: string }> = {
  pending: { label: "待审批", variant: "bg-orange-500" },
  approved: { label: "已审批", variant: "bg-blue-500" },
  executed: { label: "已执行", variant: "bg-green-500" },
  failed: { label: "执行失败", variant: "bg-red-500" },
  rejected: { label: "已拒绝", variant: "bg-gray-500" },
  cancelled: { label: "已取消", variant: "bg-gray-400" },
};

const statusTabs = [
  { value: "", label: "全部" },
  { value: "pending", label: "待审批" },
  { value: "executed", label: "已执行" },
  { value: "rejected", label: "已拒绝" },
];

export default function ApprovalsPage() {
  const { user } = useAuth();
  const [approvals, setApprovals] = useState<AdminApproval[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState("");
  const [pagination, setPagination] = useState({
    page: 0,
    pageSize: 20,
    total: 0,
  });
  const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set());

  // Reject dialog state
  const [rejectDialogOpen, setRejectDialogOpen] = useState(false);
  const [rejectingId, setRejectingId] = useState<number | null>(null);
  const [rejectReason, setRejectReason] = useState("");

  const toggleRow = (id: number) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const columns: ColumnDef<AdminApproval>[] = [
    {
      accessorKey: "action",
      header: "操作类型",
      cell: ({ row }) => {
        const action = row.getValue("action") as string;
        return (
          <span className="font-medium">
            {actionNames[action] || action}
          </span>
        );
      },
    },
    {
      accessorKey: "summary",
      header: "摘要",
      cell: ({ row }) => (
        <span className="text-sm text-muted-foreground max-w-[300px] truncate block">
          {row.getValue("summary")}
        </span>
      ),
    },
    {
      accessorKey: "requestorName",
      header: "发起人",
    },
    {
      accessorKey: "createdAt",
      header: "时间",
      cell: ({ row }) => {
        const ts = row.getValue("createdAt") as string;
        return (
          <span className="text-sm">
            {new Date(ts).toLocaleString("zh-CN")}
          </span>
        );
      },
    },
    {
      accessorKey: "status",
      header: "状态",
      cell: ({ row }) => {
        const status = row.getValue("status") as string;
        const config = statusConfig[status] || {
          label: status,
          variant: "bg-gray-500",
        };
        return (
          <Badge className={`${config.variant} text-white`}>
            {config.label}
          </Badge>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const approval = row.original;
        if (approval.status !== "pending") return null;

        const isRequestor = user?.id === approval.requestorId;

        if (isRequestor) {
          return (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleCancel(approval.id)}
            >
              <Ban className="h-4 w-4 mr-1" />
              取消
            </Button>
          );
        }

        if (user?.isAdmin) {
          return (
            <div className="flex items-center space-x-1">
              <Button
                variant="ghost"
                size="sm"
                className="text-green-600 hover:text-green-700"
                onClick={() => handleApprove(approval.id)}
              >
                <Check className="h-4 w-4 mr-1" />
                通过
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-red-600 hover:text-red-700"
                onClick={() => openRejectDialog(approval.id)}
              >
                <X className="h-4 w-4 mr-1" />
                拒绝
              </Button>
            </div>
          );
        }

        return null;
      },
    },
    {
      id: "expand",
      header: "",
      cell: ({ row }) => (
        <CollapsibleTrigger asChild onClick={() => toggleRow(row.original.id)}>
          <Button variant="ghost" size="sm">
            <ChevronDown
              className={`h-4 w-4 transition-transform ${
                expandedRows.has(row.original.id) ? "rotate-180" : ""
              }`}
            />
          </Button>
        </CollapsibleTrigger>
      ),
    },
  ];

  const table = useReactTable({
    data: approvals,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount: Math.ceil(pagination.total / pagination.pageSize),
    state: {
      pagination: {
        pageIndex: pagination.page,
        pageSize: pagination.pageSize,
      },
    },
    onPaginationChange: (updater) => {
      const newPagination =
        typeof updater === "function"
          ? updater({
              pageIndex: pagination.page,
              pageSize: pagination.pageSize,
            })
          : updater;
      setPagination((prev) => ({
        ...prev,
        page: newPagination.pageIndex,
        pageSize: newPagination.pageSize,
      }));
    },
  });

  const fetchApprovals = useCallback(async () => {
    try {
      setLoading(true);
      const response = await api.getApprovals({
        status: statusFilter || undefined,
        page: pagination.page,
        pageSize: pagination.pageSize,
      });
      setApprovals(response.items || []);
      setPagination((prev) => ({
        ...prev,
        total: response.pagination?.total ?? 0,
      }));
    } catch (error) {
      toast.error("获取审批列表失败");
      console.error("Error fetching approvals:", error);
    } finally {
      setLoading(false);
    }
  }, [pagination.page, pagination.pageSize, statusFilter]);

  useEffect(() => {
    fetchApprovals();
  }, [fetchApprovals]);

  const handleApprove = async (id: number) => {
    try {
      await api.approveApproval(id);
      toast.success("审批已通过");
      fetchApprovals();
    } catch (error) {
      toast.error("审批操作失败");
      console.error("Error approving:", error);
    }
  };

  const openRejectDialog = (id: number) => {
    setRejectingId(id);
    setRejectReason("");
    setRejectDialogOpen(true);
  };

  const handleReject = async () => {
    if (!rejectingId || !rejectReason.trim()) return;
    try {
      await api.rejectApproval(rejectingId, rejectReason.trim());
      toast.success("审批已拒绝");
      setRejectDialogOpen(false);
      setRejectingId(null);
      setRejectReason("");
      fetchApprovals();
    } catch (error) {
      toast.error("拒绝操作失败");
      console.error("Error rejecting:", error);
    }
  };

  const handleCancel = async (id: number) => {
    try {
      await api.cancelApproval(id);
      toast.success("审批已取消");
      fetchApprovals();
    } catch (error) {
      toast.error("取消操作失败");
      console.error("Error cancelling:", error);
    }
  };

  const formatParams = (params: string): string => {
    try {
      return JSON.stringify(JSON.parse(params), null, 2);
    } catch {
      return params;
    }
  };

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold">审批管理</h1>
        <p className="text-muted-foreground">
          查看和处理需要审批的操作请求
        </p>
      </div>

      {/* Status filter tabs */}
      <div className="flex space-x-1 border-b">
        {statusTabs.map((tab) => (
          <button
            key={tab.value}
            onClick={() => {
              setStatusFilter(tab.value);
              setPagination((prev) => ({ ...prev, page: 0 }));
            }}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              statusFilter === tab.value
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Table */}
      <Card>
        <CardContent className="p-0">
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((headerGroup) => (
                  <TableRow key={headerGroup.id}>
                    {headerGroup.headers.map((header) => (
                      <TableHead key={header.id}>
                        {header.isPlaceholder
                          ? null
                          : flexRender(
                              header.column.columnDef.header,
                              header.getContext()
                            )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell
                      colSpan={columns.length}
                      className="h-24 text-center"
                    >
                      加载中...
                    </TableCell>
                  </TableRow>
                ) : table.getRowModel().rows?.length ? (
                  table.getRowModel().rows.map((row) => (
                    <Collapsible key={row.id} asChild>
                      <>
                        <TableRow
                          data-state={row.getIsSelected() && "selected"}
                        >
                          {row.getVisibleCells().map((cell) => (
                            <TableCell key={cell.id}>
                              {flexRender(
                                cell.column.columnDef.cell,
                                cell.getContext()
                              )}
                            </TableCell>
                          ))}
                        </TableRow>
                        <CollapsibleContent asChild>
                          <TableRow className="bg-muted/50">
                            <TableCell colSpan={columns.length}>
                              <div className="py-2 space-y-2">
                                <div className="text-sm font-medium">
                                  请求参数
                                </div>
                                <pre className="text-xs bg-muted p-3 rounded-md overflow-auto max-h-[300px] whitespace-pre-wrap">
                                  {formatParams(row.original.params)}
                                </pre>
                                {row.original.rejectReason && (
                                  <div className="text-sm">
                                    <span className="font-medium text-red-600">
                                      拒绝原因：
                                    </span>
                                    {row.original.rejectReason}
                                  </div>
                                )}
                                {row.original.execError && (
                                  <div className="text-sm">
                                    <span className="font-medium text-red-600">
                                      执行错误：
                                    </span>
                                    {row.original.execError}
                                  </div>
                                )}
                                {row.original.approverName && (
                                  <div className="text-sm text-muted-foreground">
                                    审批人：{row.original.approverName}
                                    {row.original.approvedAt &&
                                      ` (${new Date(row.original.approvedAt).toLocaleString("zh-CN")})`}
                                  </div>
                                )}
                              </div>
                            </TableCell>
                          </TableRow>
                        </CollapsibleContent>
                      </>
                    </Collapsible>
                  ))
                ) : (
                  <TableRow>
                    <TableCell
                      colSpan={columns.length}
                      className="h-24 text-center"
                    >
                      暂无审批记录
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
          <div className="flex items-center justify-between space-x-2 py-4 px-4">
            <div className="text-sm text-muted-foreground">
              {`共 ${pagination.total} 条记录`}
            </div>
            <div className="flex items-center space-x-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => table.previousPage()}
                disabled={!table.getCanPreviousPage()}
              >
                上一页
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => table.nextPage()}
                disabled={!table.getCanNextPage()}
              >
                下一页
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Reject Dialog */}
      <Dialog open={rejectDialogOpen} onOpenChange={setRejectDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>拒绝审批</DialogTitle>
            <DialogDescription>
              请填写拒绝原因，该原因将记录在审批记录中。
            </DialogDescription>
          </DialogHeader>
          <div className="py-4">
            <Textarea
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              placeholder="请输入拒绝原因..."
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRejectDialogOpen(false)}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleReject}
              disabled={!rejectReason.trim()}
            >
              确认拒绝
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
