"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { useSearchParams, useRouter } from "next/navigation";
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
import { api, RetailerTodoItem } from "@/lib/api";
import { format } from "date-fns";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Check, ExternalLink, AlertTriangle, Clock } from "lucide-react";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

// 等级颜色映射
const levelColors: Record<number, string> = {
  1: '#9E9E9E',
  2: '#2196F3',
  3: '#9C27B0',
  4: '#FF9800',
};

export default function RetailerTodosPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<RetailerTodoItem[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  // 从 URL query 获取状态
  const page = searchParams.get("page")
    ? parseInt(searchParams.get("page") as string, 10)
    : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 20;

  // 标记完成
  const handleMarkComplete = async (item: RetailerTodoItem) => {
    try {
      await api.updateRetailerNote(item.retailerUuid, item.noteId, {
        isCompleted: true,
      });
      // 从列表中移除
      setData(data.filter((d) => d.noteId !== item.noteId));
      setTotal(total - 1);
      toast.success("已标记为完成");
    } catch (error) {
      console.error("Failed to mark complete:", error);
      toast.error("操作失败");
    }
  };

  const columns: ColumnDef<RetailerTodoItem>[] = [
    {
      accessorKey: "retailerEmail",
      header: "分销商",
      cell: ({ row }) => (
        <div>
          <div className="font-medium">{row.original.retailerEmail}</div>
          <Badge
            style={{ backgroundColor: levelColors[row.original.level] || '#9E9E9E' }}
            className="text-white text-xs mt-1"
          >
            L{row.original.level} {row.original.levelName}
          </Badge>
        </div>
      ),
    },
    {
      accessorKey: "noteContent",
      header: "沟通内容",
      cell: ({ row }) => (
        <div className="max-w-md">
          <p className="text-sm line-clamp-2">{row.original.noteContent}</p>
        </div>
      ),
    },
    {
      accessorKey: "followUpAt",
      header: "跟进时间",
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-muted-foreground" />
          <span>{format(new Date(row.original.followUpAt * 1000), "yyyy-MM-dd HH:mm")}</span>
        </div>
      ),
    },
    {
      accessorKey: "daysOverdue",
      header: "逾期状态",
      cell: ({ row }) => {
        const days = row.original.daysOverdue;
        if (days <= 0) {
          return (
            <Badge variant="secondary">
              今日到期
            </Badge>
          );
        }
        return (
          <Badge variant="destructive" className="flex items-center gap-1">
            <AlertTriangle className="h-3 w-3" />
            逾期 {days} 天
          </Badge>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => (
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleMarkComplete(row.original)}
          >
            <Check className="h-4 w-4 mr-1" />
            完成
          </Button>
          <Link href={`/manager/retailers/${row.original.retailerUuid}`}>
            <Button variant="ghost" size="sm">
              <ExternalLink className="h-4 w-4 mr-1" />
              详情
            </Button>
          </Link>
        </div>
      ),
    },
  ];

  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount,
    state: {
      pagination: {
        pageIndex: page,
        pageSize,
      },
    },
  });

  // 更新URL参数
  const updateSearchParams = (updates: Record<string, string | undefined>) => {
    const params = new URLSearchParams(searchParams.toString());
    Object.entries(updates).forEach(([key, value]) => {
      if (value === undefined || value === "") {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    router.push(`/manager/retailers/todos?${params.toString()}`);
  };

  // 分页处理
  const handlePageChange = (newPage: number) => {
    updateSearchParams({ page: newPage.toString() });
  };

  const handlePageSizeChange = (newPageSize: string) => {
    updateSearchParams({ pageSize: newPageSize, page: "0" });
  };

  // 加载数据
  useEffect(() => {
    const fetchData = async () => {
      setIsLoading(true);
      try {
        const result = await api.getRetailerTodos({
          page,
          pageSize,
        });

        setData(result.items || []);
        if (result.pagination) {
          setTotal(result.pagination.total);
          setPageCount(Math.ceil(result.pagination.total / pageSize));
        }
      } catch (error) {
        console.error("Failed to fetch todos:", error);
      } finally {
        setIsLoading(false);
      }
    };

    fetchData();
  }, [page, pageSize]);

  return (
    <div className="container mx-auto py-10">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold">分销待办</h1>
          <p className="text-muted-foreground mt-1">
            需要跟进的分销商沟通事项
          </p>
        </div>
        <div className="flex items-center gap-2">
          {total > 0 && (
            <Badge variant="destructive" className="text-lg px-3 py-1">
              {total} 个待处理
            </Badge>
          )}
        </div>
      </div>

      {/* 数据表格 */}
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
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  加载中...
                </TableCell>
              </TableRow>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  className={row.original.daysOverdue > 0 ? "bg-red-50" : ""}
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
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                    <Check className="h-8 w-8 text-green-500" />
                    <span>太棒了！没有待处理的事项</span>
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {/* 分页 */}
      {total > 0 && (
        <div className="flex items-center justify-end space-x-2 py-4">
          <div className="flex items-center space-x-2">
            <span className="text-sm text-muted-foreground">每页显示</span>
            <Select
              value={pageSize.toString()}
              onValueChange={handlePageSizeChange}
            >
              <SelectTrigger className="w-[70px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[10, 20, 30, 50].map((size) => (
                  <SelectItem key={size} value={size.toString()}>
                    {size}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center space-x-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(page - 1)}
              disabled={page === 0}
            >
              上一页
            </Button>
            <span className="text-sm text-muted-foreground">
              第 {page + 1} 页，共 {pageCount} 页
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(page + 1)}
              disabled={page >= pageCount - 1}
            >
              下一页
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
