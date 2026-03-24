"use client";

import { useState, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { api, FeedbackTicket } from "@/lib/api";
import { useAuth } from "@/contexts/AuthContext";
import { toast } from "sonner";
import { CheckCircle, XCircle, Eye, FileText } from "lucide-react";

const statusConfig: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
  open: { label: "待处理", variant: "destructive" },
  resolved: { label: "已解决", variant: "default" },
  closed: { label: "已关闭", variant: "secondary" },
};

export default function TicketsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { user } = useAuth();

  const [data, setData] = useState<FeedbackTicket[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  // Detail dialog
  const [detailTicket, setDetailTicket] = useState<FeedbackTicket | null>(null);

  // URL query state
  const page = parseInt(searchParams.get("page") || "0", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "50", 10);
  const filterStatus = searchParams.get("status") || "";
  const filterEmail = searchParams.get("email") || "";
  const filterUdid = searchParams.get("udid") || "";

  // Local filter state
  const [localStatus, setLocalStatus] = useState(filterStatus);
  const [localEmail, setLocalEmail] = useState(filterEmail);
  const [localUdid, setLocalUdid] = useState(filterUdid);

  const formatDate = (timestamp: number) => {
    if (!timestamp) return "-";
    return new Date(timestamp * 1000).toLocaleString("zh-CN");
  };

  const truncate = (str: string, len: number) => {
    if (!str) return "-";
    return str.length > len ? str.slice(0, len) + "..." : str;
  };

  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    const fetchTickets = async () => {
      setIsLoading(true);
      try {
        const response = await api.getFeedbackTickets({
          page,
          pageSize,
          status: filterStatus || undefined,
          email: filterEmail || undefined,
          udid: filterUdid || undefined,
        });
        setData(response.items || []);
        if (response.pagination) {
          setPageCount(Math.ceil(response.pagination.total / response.pagination.pageSize));
          setTotal(response.pagination.total);
        }
      } catch (error) {
        console.error("Failed to fetch tickets:", error);
        toast.error("加载工单失败");
      } finally {
        setIsLoading(false);
      }
    };
    fetchTickets();
  }, [page, pageSize, filterStatus, filterEmail, filterUdid, refreshKey]);

  const handleResolve = async (ticket: FeedbackTicket) => {
    try {
      await api.resolveFeedbackTicket(ticket.id, user?.email || "admin");
      toast.success("工单已标记为已解决");
      setRefreshKey((k) => k + 1);
      setDetailTicket(null);
    } catch (error) {
      console.error("Failed to resolve ticket:", error);
      toast.error("操作失败");
    }
  };

  const handleClose = async (ticket: FeedbackTicket) => {
    try {
      await api.closeFeedbackTicket(ticket.id);
      toast.success("工单已关闭");
      setRefreshKey((k) => k + 1);
      setDetailTicket(null);
    } catch (error) {
      console.error("Failed to close ticket:", error);
      toast.error("操作失败");
    }
  };

  const columns: ColumnDef<FeedbackTicket>[] = [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => (
        <code className="text-xs bg-muted px-1 py-0.5 rounded">
          #{row.getValue("id")}
        </code>
      ),
    },
    {
      accessorKey: "email",
      header: "邮箱",
      cell: ({ row }) => row.getValue("email") || <span className="text-muted-foreground">{"匿名"}</span>,
    },
    {
      accessorKey: "content",
      header: "内容",
      cell: ({ row }) => (
        <span className="text-sm" title={row.getValue("content")}>
          {truncate(row.getValue("content"), 60)}
        </span>
      ),
    },
    {
      accessorKey: "status",
      header: "状态",
      cell: ({ row }) => {
        const status = row.getValue("status") as string;
        const config = statusConfig[status] || { label: status, variant: "outline" as const };
        return <Badge variant={config.variant}>{config.label}</Badge>;
      },
    },
    {
      accessorKey: "logCount",
      header: "日志",
      cell: ({ row }) => {
        const count = row.getValue("logCount") as number;
        return count > 0 ? (
          <Badge variant="outline" className="gap-1">
            <FileText className="h-3 w-3" />
            {count}
          </Badge>
        ) : (
          <span className="text-muted-foreground">{"0"}</span>
        );
      },
    },
    {
      accessorKey: "udid",
      header: "UDID",
      cell: ({ row }) => (
        <code className="text-xs bg-muted px-1 py-0.5 rounded" title={row.getValue("udid")}>
          {truncate(row.getValue("udid"), 12)}
        </code>
      ),
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      cell: ({ row }) => formatDate(row.getValue("createdAt")),
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const ticket = row.original;
        return (
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setDetailTicket(ticket)}
              title="查看详情"
            >
              <Eye className="h-4 w-4" />
            </Button>
            {ticket.status === "open" && (
              <>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleResolve(ticket)}
                  title="标记已解决"
                  className="text-green-500 hover:text-green-600"
                >
                  <CheckCircle className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleClose(ticket)}
                  title="关闭"
                  className="text-red-500 hover:text-red-600"
                >
                  <XCircle className="h-4 w-4" />
                </Button>
              </>
            )}
            {ticket.status === "resolved" && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleClose(ticket)}
                title="关闭"
                className="text-red-500 hover:text-red-600"
              >
                <XCircle className="h-4 w-4" />
              </Button>
            )}
          </div>
        );
      },
    },
  ];

  const table = useReactTable({
    data,
    columns,
    pageCount,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  });

  const handleFilter = () => {
    const params = new URLSearchParams();
    params.set("page", "0");
    params.set("pageSize", pageSize.toString());
    if (localStatus) params.set("status", localStatus);
    if (localEmail.trim()) params.set("email", localEmail.trim());
    if (localUdid.trim()) params.set("udid", localUdid.trim());
    router.push(`/manager/tickets?${params.toString()}`);
  };

  const handleReset = () => {
    setLocalStatus("");
    setLocalEmail("");
    setLocalUdid("");
    router.push("/manager/tickets");
  };

  const parseMeta = (meta?: string): Record<string, string> => {
    if (!meta) return {};
    try {
      return JSON.parse(meta);
    } catch {
      return {};
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"工单管理"}</h1>
        <p className="text-muted-foreground">{"查看和处理用户反馈工单"}</p>
      </div>

      {/* Filter area */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1">
          <label className="text-sm font-medium">{"状态"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localStatus}
            onChange={(e) => setLocalStatus(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="open">{"待处理"}</option>
            <option value="resolved">{"已解决"}</option>
            <option value="closed">{"已关闭"}</option>
          </select>
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"邮箱"}</label>
          <Input
            placeholder="搜索邮箱"
            value={localEmail}
            onChange={(e) => setLocalEmail(e.target.value)}
          />
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"UDID"}</label>
          <Input
            placeholder="搜索设备 UDID"
            value={localUdid}
            onChange={(e) => setLocalUdid(e.target.value)}
          />
        </div>
        <div className="flex gap-2">
          <Button onClick={handleFilter}>{"筛选"}</Button>
          <Button variant="outline" onClick={handleReset}>
            {"重置"}
          </Button>
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
                <TableRow key={row.id}>
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
                  {"无工单数据"}
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
            router.push(`/manager/tickets?${params.toString()}`);
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
            router.push(`/manager/tickets?${params.toString()}`);
          }}
          disabled={page >= pageCount - 1}
        >
          {"下一页"}
        </Button>
      </div>

      {/* Detail Dialog */}
      <Dialog open={!!detailTicket} onOpenChange={(open) => { if (!open) setDetailTicket(null); }}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{"工单详情"} #{detailTicket?.id}</DialogTitle>
            <DialogDescription>
              {"创建于 "}{detailTicket ? formatDate(detailTicket.createdAt) : ""}
            </DialogDescription>
          </DialogHeader>
          {detailTicket && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-sm font-medium text-muted-foreground">{"状态"}</label>
                  <div className="mt-1">
                    <Badge variant={statusConfig[detailTicket.status]?.variant || "outline"}>
                      {statusConfig[detailTicket.status]?.label || detailTicket.status}
                    </Badge>
                  </div>
                </div>
                <div>
                  <label className="text-sm font-medium text-muted-foreground">{"邮箱"}</label>
                  <p className="mt-1">{detailTicket.email || "匿名用户"}</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-muted-foreground">{"UDID"}</label>
                  <code className="mt-1 block text-xs bg-muted px-2 py-1 rounded break-all">
                    {detailTicket.udid}
                  </code>
                </div>
                <div>
                  <label className="text-sm font-medium text-muted-foreground">{"Feedback ID"}</label>
                  <code className="mt-1 block text-xs bg-muted px-2 py-1 rounded break-all">
                    {detailTicket.feedbackId}
                  </code>
                </div>
                {detailTicket.userId && (
                  <div>
                    <label className="text-sm font-medium text-muted-foreground">{"用户 ID"}</label>
                    <p className="mt-1">{detailTicket.userId}</p>
                  </div>
                )}
                <div>
                  <label className="text-sm font-medium text-muted-foreground">{"关联日志"}</label>
                  <p className="mt-1">{detailTicket.logCount} {"条"}</p>
                </div>
                {detailTicket.resolvedBy && (
                  <div>
                    <label className="text-sm font-medium text-muted-foreground">{"处理人"}</label>
                    <p className="mt-1">{detailTicket.resolvedBy}</p>
                  </div>
                )}
                {detailTicket.resolvedAt && (
                  <div>
                    <label className="text-sm font-medium text-muted-foreground">{"处理时间"}</label>
                    <p className="mt-1">{formatDate(detailTicket.resolvedAt)}</p>
                  </div>
                )}
              </div>

              {/* Meta info */}
              {detailTicket.meta && (() => {
                const meta = parseMeta(detailTicket.meta);
                const entries = Object.entries(meta);
                if (entries.length === 0) return null;
                return (
                  <div>
                    <label className="text-sm font-medium text-muted-foreground">{"设备信息"}</label>
                    <div className="mt-1 grid grid-cols-2 gap-2 bg-muted p-3 rounded-lg text-sm">
                      {entries.map(([key, value]) => (
                        <div key={key}>
                          <span className="text-muted-foreground">{key}: </span>
                          <span>{value}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })()}

              <div>
                <label className="text-sm font-medium text-muted-foreground">{"反馈内容"}</label>
                <div className="mt-1 bg-muted p-4 rounded-lg whitespace-pre-wrap text-sm">
                  {detailTicket.content}
                </div>
              </div>
            </div>
          )}
          <DialogFooter>
            {detailTicket?.status === "open" && (
              <>
                <Button
                  variant="outline"
                  onClick={() => detailTicket && handleClose(detailTicket)}
                >
                  {"关闭工单"}
                </Button>
                <Button onClick={() => detailTicket && handleResolve(detailTicket)}>
                  {"标记已解决"}
                </Button>
              </>
            )}
            {detailTicket?.status === "resolved" && (
              <Button
                variant="outline"
                onClick={() => detailTicket && handleClose(detailTicket)}
              >
                {"关闭工单"}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
