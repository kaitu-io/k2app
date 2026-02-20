"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
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
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { Mail, Search, RefreshCw, CheckCircle2, XCircle, Clock, SkipForward } from "lucide-react";
import type { EmailSendLogResponse, EmailSendLogStats } from "@/lib/api";
import Link from "next/link";
import { useSearchParams } from "next/navigation";

export default function EmailSendLogsPage() {
  const locale = "zh-CN";
  const searchParams = useSearchParams();

  const [logs, setLogs] = useState<EmailSendLogResponse[]>([]);
  const [stats, setStats] = useState<EmailSendLogStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(0);
  const [pageSize] = useState(100);
  const [total, setTotal] = useState(0);

  // Filters
  const [batchId, setBatchId] = useState<string>(searchParams.get("batchId") || "");
  const [status, setStatus] = useState<string>(searchParams.get("status") || "");
  const [email, setEmail] = useState<string>(searchParams.get("email") || "");

  const formatDate = (timestamp: number | null) => {
    if (!timestamp) return "-";
    return new Date(timestamp * 1000).toLocaleString(locale);
  };

  const getStatusBadge = (statusValue: string) => {
    switch (statusValue) {
      case "sent":
        return (
          <Badge variant="default" className="bg-green-500">
            <CheckCircle2 className="mr-1 h-3 w-3" />
            {"已发送"}
          </Badge>
        );
      case "failed":
        return (
          <Badge variant="destructive">
            <XCircle className="mr-1 h-3 w-3" />
            {"失败"}
          </Badge>
        );
      case "pending":
        return (
          <Badge variant="outline">
            <Clock className="mr-1 h-3 w-3" />
            {"等待中"}
          </Badge>
        );
      case "skipped":
        return (
          <Badge variant="secondary">
            <SkipForward className="mr-1 h-3 w-3" />
            {"已跳过"}
          </Badge>
        );
      default:
        return <Badge variant="outline">{statusValue}</Badge>;
    }
  };

  const columns: ColumnDef<EmailSendLogResponse>[] = [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => <span className="font-mono text-xs">{row.getValue("id")}</span>,
    },
    {
      accessorKey: "email",
      header: "邮箱",
      cell: ({ row }) => (
        <div>
          <div className="font-medium">{row.getValue("email")}</div>
          <div className="text-xs text-muted-foreground">
            {row.original.userUuid?.substring(0, 8)}...
          </div>
        </div>
      ),
    },
    {
      accessorKey: "templateName",
      header: "模板",
      cell: ({ row }) => (
        <div>
          <div className="font-medium">{row.getValue("templateName") || "-"}</div>
          <div className="text-xs text-muted-foreground">ID: {row.original.templateId}</div>
        </div>
      ),
    },
    {
      accessorKey: "language",
      header: "语言",
      cell: ({ row }) => <Badge variant="outline">{row.getValue("language")}</Badge>,
    },
    {
      accessorKey: "status",
      header: "状态",
      cell: ({ row }) => getStatusBadge(row.getValue("status")),
    },
    {
      accessorKey: "sentAt",
      header: "发送时间",
      cell: ({ row }) => formatDate(row.getValue("sentAt")),
    },
    {
      accessorKey: "errorMsg",
      header: "错误信息",
      cell: ({ row }) => {
        const error = row.getValue("errorMsg") as string | null;
        if (!error) return "-";
        return (
          <span className="text-xs text-red-500 max-w-[200px] truncate block" title={error}>
            {error}
          </span>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      cell: ({ row }) => formatDate(row.getValue("createdAt")),
    },
  ];

  const table = useReactTable({
    data: logs,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  });

  const fetchLogs = useCallback(async () => {
    try {
      setLoading(true);
      const params: {
        page: number;
        pageSize: number;
        batchId?: string;
        status?: "pending" | "sent" | "failed" | "skipped";
        email?: string;
      } = {
        page,
        pageSize,
      };

      if (batchId) params.batchId = batchId;
      if (status && status !== "all") params.status = status as "pending" | "sent" | "failed" | "skipped";
      if (email) params.email = email;

      const data = await api.getEmailSendLogs(params);
      setLogs(data.items);
      setStats(data.stats);
      setTotal(data.pagination.total);
    } catch (error) {
      toast.error("获取日志失败");
      console.error("Error fetching logs:", error);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, batchId, status, email]);

  const handleSearch = () => {
    setPage(0);
    fetchLogs();
  };

  const handleReset = () => {
    setBatchId("");
    setStatus("");
    setEmail("");
    setPage(0);
  };

  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  const totalPages = Math.ceil(total / pageSize);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <Link href="/manager/edm" className="text-3xl font-bold tracking-tight hover:underline">
            {"邮件发送日志"}
          </Link>
          <p className="text-muted-foreground">{"查看所有邮件发送记录"}</p>
        </div>
        <Button onClick={fetchLogs} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          {"刷新"}
        </Button>
      </div>

      {/* Stats Cards */}
      {stats && (
        <div className="grid gap-4 md:grid-cols-5">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{"总计"}</CardTitle>
              <Mail className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.totalCount.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{"已发送"}</CardTitle>
              <CheckCircle2 className="h-4 w-4 text-green-500" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-green-600">{stats.sentCount.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{"失败"}</CardTitle>
              <XCircle className="h-4 w-4 text-red-500" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-red-600">{stats.failedCount.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{"等待中"}</CardTitle>
              <Clock className="h-4 w-4 text-yellow-500" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-yellow-600">{stats.pendingCount.toLocaleString()}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{"已跳过"}</CardTitle>
              <SkipForward className="h-4 w-4 text-gray-500" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-gray-600">{stats.skippedCount.toLocaleString()}</div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center">
            <Search className="mr-2 h-5 w-5" />
            {"筛选条件"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <div className="space-y-2">
              <Label>{"批次ID"}</Label>
              <Input
                placeholder={"输入批次ID"}
                value={batchId}
                onChange={(e) => setBatchId(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleSearch()}
              />
            </div>
            <div className="space-y-2">
              <Label>{"状态"}</Label>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger>
                  <SelectValue placeholder={"全部状态"} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{"全部状态"}</SelectItem>
                  <SelectItem value="sent">{"已发送"}</SelectItem>
                  <SelectItem value="failed">{"失败"}</SelectItem>
                  <SelectItem value="pending">{"等待中"}</SelectItem>
                  <SelectItem value="skipped">{"已跳过"}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>{"邮箱"}</Label>
              <Input
                placeholder={"输入邮箱地址"}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleSearch()}
              />
            </div>
            <div className="flex items-end space-x-2">
              <Button onClick={handleSearch} className="flex-1">
                <Search className="mr-2 h-4 w-4" />
                {"搜索"}
              </Button>
              <Button variant="outline" onClick={handleReset}>
                {"重置"}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Data Table */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center">
            <Mail className="mr-2 h-5 w-5" />
            {"发送记录"}
          </CardTitle>
          <CardDescription>
            {`显示 ${page * pageSize + 1} - ${Math.min((page + 1) * pageSize, total)} 条，共 ${total} 条`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center justify-center h-32">
              <RefreshCw className="h-6 w-6 animate-spin" />
            </div>
          ) : (
            <>
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
                  {table.getRowModel().rows?.length ? (
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
                        {"暂无记录"}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>

              {/* Pagination */}
              <div className="flex items-center justify-between py-4">
                <div className="text-sm text-muted-foreground">
                  {`第 ${page + 1} 页，共 ${totalPages || 1} 页`}
                </div>
                <div className="flex space-x-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => Math.max(0, p - 1))}
                    disabled={page === 0}
                  >
                    {"上一页"}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => p + 1)}
                    disabled={page >= totalPages - 1}
                  >
                    {"下一页"}
                  </Button>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
