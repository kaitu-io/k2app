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
import { Input } from "@/components/ui/input";
import { api, AdminRetailerListItem, getContactTypeName } from "@/lib/api";
import { format } from "date-fns";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { MessageSquare, Clock } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

// 等级颜色映射
const levelColors: Record<number, string> = {
  1: '#9E9E9E',  // L1 灰色
  2: '#2196F3',  // L2 蓝色
  3: '#9C27B0',  // L3 紫色
  4: '#FF9800',  // L4 金色
};

// 格式化金额（美分转美元）
function formatAmount(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

export default function RetailersPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<AdminRetailerListItem[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  // 从 URL query 获取状态
  const page = searchParams.get("page")
    ? parseInt(searchParams.get("page") as string, 10)
    : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 50;
  const email = searchParams.get("email") || "";
  const level = searchParams.get("level") || "";

  // 用于UI组件的状态
  const [emailInput, setEmailInput] = useState(email);
  const [levelFilter, setLevelFilter] = useState(level);

  const columns: ColumnDef<AdminRetailerListItem>[] = [
    {
      accessorKey: "email",
      header: "Email",
      cell: ({ row }) => (
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <span className="font-medium">{row.original.email}</span>
            {(row.original.pendingFollowUpCnt ?? 0) > 0 && (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger>
                    <Badge variant="destructive" className="text-xs px-1.5 py-0">
                      <Clock className="h-3 w-3 mr-0.5" />
                      {row.original.pendingFollowUpCnt}
                    </Badge>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{row.original.pendingFollowUpCnt} 个待跟进事项</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
          </div>
          {row.original.notes && (
            <span className="text-xs text-muted-foreground line-clamp-1">
              {row.original.notes}
            </span>
          )}
        </div>
      ),
    },
    {
      accessorKey: "level",
      header: "等级",
      cell: ({ row }) => (
        <Badge
          style={{ backgroundColor: levelColors[row.original.level] || '#9E9E9E' }}
          className="text-white"
        >
          L{row.original.level} {row.original.levelName}
        </Badge>
      ),
    },
    {
      accessorKey: "contacts",
      header: "联系方式",
      cell: ({ row }) => {
        const contacts = row.original.contacts || [];
        if (contacts.length === 0) {
          return <span className="text-muted-foreground">-</span>;
        }
        return (
          <div className="flex flex-col gap-1">
            {contacts.slice(0, 2).map((contact, idx) => (
              <span key={idx} className="text-sm">
                {getContactTypeName(contact.type)}: {contact.value}
              </span>
            ))}
            {contacts.length > 2 && (
              <span className="text-xs text-muted-foreground">
                +{contacts.length - 2} 更多
              </span>
            )}
          </div>
        );
      },
    },
    {
      accessorKey: "paidUserCount",
      header: "付费用户",
      cell: ({ row }) => (
        <span>{row.original.paidUserCount}</span>
      ),
    },
    {
      accessorKey: "totalIncome",
      header: "累计收入",
      cell: ({ row }) => {
        const totalIncome = row.original.totalIncome;
        if (totalIncome === undefined || totalIncome === 0) {
          return <span className="text-muted-foreground">-</span>;
        }
        return <span className="font-medium">{formatAmount(totalIncome)}</span>;
      },
    },
    {
      accessorKey: "wallet",
      header: "钱包余额",
      cell: ({ row }) => {
        const wallet = row.original.wallet;
        if (!wallet) {
          return <span className="text-muted-foreground">-</span>;
        }
        return (
          <div className="flex flex-col">
            <span className="font-medium">{formatAmount(wallet.availableBalance)}</span>
            {wallet.frozenBalance > 0 && (
              <span className="text-xs text-muted-foreground">
                冻结: {formatAmount(wallet.frozenBalance)}
              </span>
            )}
          </div>
        );
      },
    },
    {
      accessorKey: "lastCommunicatedAt",
      header: "最近沟通",
      cell: ({ row }) => {
        const timestamp = row.original.lastCommunicatedAt;
        if (!timestamp) {
          return <span className="text-muted-foreground">-</span>;
        }
        return (
          <span className="text-sm">
            {format(new Date(timestamp * 1000), "yyyy-MM-dd")}
          </span>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "注册时间",
      cell: ({ row }) => {
        const timestamp = row.original.createdAt;
        if (!timestamp) {
          return <span className="text-muted-foreground">-</span>;
        }
        return (
          <span className="text-sm">
            {format(new Date(timestamp * 1000), "yyyy-MM-dd")}
          </span>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => (
        <div className="flex gap-2">
          <Link href={`/manager/retailers/${row.original.uuid}`}>
            <Button variant="outline" size="sm">
              <MessageSquare className="h-4 w-4 mr-1" />
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
    router.push(`/manager/retailers?${params.toString()}`);
  };

  // 搜索处理
  const handleSearch = () => {
    updateSearchParams({
      email: emailInput || undefined,
      level: levelFilter || undefined,
      page: "0",
    });
  };

  // 清除筛选
  const handleClearFilters = () => {
    setEmailInput("");
    setLevelFilter("");
    router.push("/manager/retailers");
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
        const result = await api.getRetailers({
          page,
          pageSize,
          email: email || undefined,
          level: level ? parseInt(level, 10) : undefined,
        });

        setData(result.items || []);
        if (result.pagination) {
          setTotal(result.pagination.total);
          setPageCount(Math.ceil(result.pagination.total / pageSize));
        }
      } catch (error) {
        console.error("Failed to fetch retailers:", error);
      } finally {
        setIsLoading(false);
      }
    };

    fetchData();
  }, [page, pageSize, email, level]);

  return (
    <div className="container mx-auto py-10">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">分销商管理</h1>
        <div className="text-sm text-muted-foreground">
          共 {total} 位分销商
        </div>
      </div>

      {/* 筛选工具栏 */}
      <div className="flex items-center gap-4 mb-4 flex-wrap">
        <Input
          placeholder="搜索邮箱..."
          value={emailInput}
          onChange={(e) => setEmailInput(e.target.value)}
          className="max-w-xs"
          onKeyDown={(e) => e.key === "Enter" && handleSearch()}
        />

        <Select value={levelFilter} onValueChange={setLevelFilter}>
          <SelectTrigger className="w-[140px]">
            <SelectValue placeholder="选择等级" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部等级</SelectItem>
            <SelectItem value="1">L1 推荐者</SelectItem>
            <SelectItem value="2">L2 分销商</SelectItem>
            <SelectItem value="3">L3 优质分销商</SelectItem>
            <SelectItem value="4">L4 合伙人</SelectItem>
          </SelectContent>
        </Select>

        <Button onClick={handleSearch}>搜索</Button>
        <Button variant="outline" onClick={handleClearFilters}>
          清除筛选
        </Button>
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
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-24 text-center">
                  暂无数据
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {/* 分页 */}
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
              {[10, 20, 30, 40, 50].map((size) => (
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
    </div>
  );
}
