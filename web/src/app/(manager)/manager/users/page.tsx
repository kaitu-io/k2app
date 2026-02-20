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
  Row,
  RowSelectionState,
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
import { api } from "@/lib/api";
import { format } from "date-fns";
import Link from "next/link";
import { Checkbox } from "@/components/ui/checkbox";
import { DateInput } from "@/components/ui/date-input";
import { Mail, Users, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "sonner";

// 定义用户列表项的数据结构
interface UserListItem {
  uuid: string;
  expiredAt: number;
  isFirstOrderDone: boolean;
  loginIdentifies: { type: string; value: string }[];
  isRetailer?: boolean;
  retailerConfig?: {
    level: number;
    levelName: string;
    firstOrderPercent: number;
    renewalPercent: number;
    paidUserCount: number;
    progressPercent: number;
  };
  wallet?: {
    balance: number;
    availableBalance: number;
    frozenBalance: number;
    totalIncome: number;
    totalWithdrawn: number;
  };
}

// 等级颜色映射
const levelColors: Record<number, string> = {
  1: '#9E9E9E',  // L1 灰色
  2: '#2196F3',  // L2 蓝色
  3: '#9C27B0',  // L3 紫色
  4: '#FF9800',  // L4 金色
};

interface UserListResponse {
  items: UserListItem[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

export default function UsersPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<UserListItem[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [showFirstConfirm, setShowFirstConfirm] = useState(false);
  const [showSecondConfirm, setShowSecondConfirm] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // 从 URL query 获取状态
  const page = searchParams.get("page")
    ? parseInt(searchParams.get("page") as string, 10)
    : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 50;
  const email = searchParams.get("email") || "";
  const hasOrdered = searchParams.get("has_ordered") || "";
  const isRetailer = searchParams.get("is_retailer") || "";
  const expiredAtStart = searchParams.get("expired_at_start") || "";
  const expiredAtEnd = searchParams.get("expired_at_end") || "";

  // 用于UI组件的状态
  const [emailInput, setEmailInput] = useState(email);
  const [hasOrderedChecked, setHasOrderedChecked] = useState(
    hasOrdered === "true"
  );
  const [isRetailerChecked, setIsRetailerChecked] = useState(
    isRetailer === "true"
  );
  const [startDate, setStartDate] = useState(
    expiredAtStart
      ? format(new Date(parseInt(expiredAtStart) * 1000), "yyyy-MM-dd")
      : ""
  );
  const [endDate, setEndDate] = useState(
    expiredAtEnd
      ? format(new Date(parseInt(expiredAtEnd) * 1000), "yyyy-MM-dd")
      : ""
  );

  const columns: ColumnDef<UserListItem>[] = [
    {
      id: "select",
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label="Select all"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label="Select row"
        />
      ),
      enableSorting: false,
      enableHiding: false,
    },
    {
      accessorKey: "loginIdentifies",
      header: "Email",
      cell: ({ row }: { row: Row<UserListItem> }) => {
        const identifies = row.getValue("loginIdentifies") as {
          type: string;
          value: string;
        }[] || [];
        const emailIdentify = identifies.find((id) => id.type === "email");
        return emailIdentify ? emailIdentify.value : "N/A";
      },
    },
    {
      accessorKey: "expiredAt",
      header: "会员过期时间",
      cell: ({ row }: { row: Row<UserListItem> }) => {
        const expiredAt = row.getValue("expiredAt") as number;
        return expiredAt > 0
          ? format(new Date(expiredAt * 1000), "yyyy-MM-dd HH:mm:ss")
          : "N/A";
      },
    },
    {
      accessorKey: "isFirstOrderDone",
      header: "是否付费",
      cell: ({ row }: { row: Row<UserListItem> }) =>
        row.getValue("isFirstOrderDone") ? "是" : "否",
    },
    {
      accessorKey: "retailerConfig",
      header: "分销商",
      cell: ({ row }: { row: Row<UserListItem> }) => {
        const config = row.original.retailerConfig;
        if (!config) return <span className="text-muted-foreground">{"-"}</span>;

        const levelColor = levelColors[config.level] || levelColors[1];

        return (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <span
                className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-semibold text-white"
                style={{ backgroundColor: levelColor }}
              >
                {"L"}{config.level}
              </span>
              <span className="text-sm">{config.levelName}</span>
            </div>
            <div className="text-xs text-muted-foreground">
              {"首单 "}{config.firstOrderPercent}{"% · 续费 "}{config.renewalPercent}{"%"}
            </div>
            <div className="text-xs text-muted-foreground">
              {"付费用户: "}{config.paidUserCount}
            </div>
          </div>
        );
      },
    },
    {
      accessorKey: "wallet",
      header: "钱包",
      cell: ({ row }: { row: Row<UserListItem> }) => {
        const wallet = row.original.wallet;
        if (!wallet) return <span className="text-muted-foreground">{"-"}</span>;

        return (
          <div className="space-y-1">
            <div className="text-sm font-medium">
              {"$"}{(wallet.availableBalance / 100).toFixed(2)}
            </div>
            <div className="text-xs text-muted-foreground">
              {"冻结: $"}{(wallet.frozenBalance / 100).toFixed(2)}
            </div>
          </div>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }: { row: Row<UserListItem> }) => (
        <div className="flex items-center gap-2">
          <Link href={`/manager/users/detail?uuid=${row.original.uuid}`}>
            <Button type="button" variant="outline" size="sm">
              {"详情"}
            </Button>
          </Link>
          <Link href={`/manager/users/${row.original.uuid}/members`}>
            <Button type="button" variant="ghost" size="sm">
              <Users className="h-4 w-4 mr-1" />
              {"成员"}
            </Button>
          </Link>
        </div>
      ),
    },
  ];

  useEffect(() => {
    const fetchUsers = async () => {
      setIsLoading(true);
      const params = new URLSearchParams();
      params.append("page", page.toString());
      params.append("pageSize", pageSize.toString());
      if (email) params.append("email", email.trim());
      if (hasOrdered) params.append("has_ordered", hasOrdered);
      if (isRetailer) params.append("is_retailer", isRetailer);
      if (startDate)
        params.append(
          "expired_at_start",
          Math.floor(new Date(startDate).getTime() / 1000).toString()
        );
      if (endDate)
        params.append(
          "expired_at_end",
          Math.floor(new Date(endDate).getTime() / 1000).toString()
        );

      try {
        const response = await api.request<UserListResponse>(
          `/app/users?${params.toString()}`
        );

        setData(response.items || []);
        setPageCount(
          Math.ceil(response.pagination.total / response.pagination.pageSize)
        );
        setTotal(response.pagination.total);
      } catch (error) {
        // Errors are handled globally by AuthContext (for 401)
        // or shown as a toast by the api module.
        // We log it here for debugging purposes.
        console.error("Failed to fetch users:", error);
      } finally {
        setIsLoading(false);
      }
    };
    fetchUsers();
  }, [page, pageSize, email, hasOrdered, isRetailer, startDate, endDate]);

  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    pageCount,
    state: {
      pagination: {
        pageIndex: page,
        pageSize: pageSize,
      },
      rowSelection,
    },
    onRowSelectionChange: setRowSelection,
    getRowId: (row) => row.uuid,
    enableRowSelection: true,
    manualPagination: true,
  });

  const handleFilter = () => {
    const params = new URLSearchParams();
    params.set("page", "0");
    params.set("pageSize", pageSize.toString());
    if (emailInput) params.set("email", emailInput.trim());
    if (hasOrderedChecked) params.set("has_ordered", "true");
    if (isRetailerChecked) params.set("is_retailer", "true");
    if (startDate)
      params.set(
        "expired_at_start",
        Math.floor(new Date(startDate).getTime() / 1000).toString()
      );
    if (endDate)
      params.set(
        "expired_at_end",
        Math.floor(new Date(endDate).getTime() / 1000).toString()
      );

    router.push(`/manager/users?${params.toString()}`);
  };

  const handleSendMarketingEmail = () => {
    const selectedRowIds = Object.keys(rowSelection).filter(id => rowSelection[id]);
    if (selectedRowIds.length === 0) {
      return;
    }

    const userUuids = selectedRowIds.join(',');
    router.push(`/manager/edm/create-task?userUuids=${userUuids}`);
  };

  const handlePageChange = (newPageIndex: number) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("page", newPageIndex.toString());
    router.push(`/manager/users?${params.toString()}`);
  };

  const handlePageSizeChange = (newPageSize: number) => {
    const params = new URLSearchParams(searchParams.toString());
    params.set("pageSize", newPageSize.toString());
    params.set("page", "0");
    router.push(`/manager/users?${params.toString()}`);
  };

  const handleDeleteUsers = () => {
    const selectedRowIds = Object.keys(rowSelection).filter(id => rowSelection[id]);
    if (selectedRowIds.length === 0) {
      return;
    }
    setShowFirstConfirm(true);
  };

  const confirmFirstDelete = () => {
    setShowFirstConfirm(false);
    setShowSecondConfirm(true);
  };

  const confirmSecondDelete = async () => {
    const selectedRowIds = Object.keys(rowSelection).filter(id => rowSelection[id]);
    if (selectedRowIds.length === 0) {
      return;
    }

    // 防止重复点击
    if (isDeleting) return;

    setIsDeleting(true);
    try {
      await api.request('/app/users/hard-delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ userUuids: selectedRowIds }),
      });

      setShowSecondConfirm(false);
      setRowSelection({});

      // 显示成功提示
      toast.success(`成功删除 ${selectedRowIds.length} 个用户及其所有关联数据`);

      // 重新加载用户列表
      const params = new URLSearchParams();
      params.append("page", page.toString());
      params.append("pageSize", pageSize.toString());
      if (email) params.append("email", email.trim());
      if (hasOrdered) params.append("has_ordered", hasOrdered);
      if (isRetailer) params.append("is_retailer", isRetailer);
      if (startDate)
        params.append(
          "expired_at_start",
          Math.floor(new Date(startDate).getTime() / 1000).toString()
        );
      if (endDate)
        params.append(
          "expired_at_end",
          Math.floor(new Date(endDate).getTime() / 1000).toString()
        );

      const response = await api.request<UserListResponse>(
        `/app/users?${params.toString()}`
      );
      setData(response.items || []);
      setPageCount(
        Math.ceil(response.pagination.total / response.pagination.pageSize)
      );
      setTotal(response.pagination.total);
    } catch (error) {
      console.error("Failed to delete users:", error);
      toast.error("删除失败，请重试或联系管理员");
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <div className="container mx-auto py-10">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">{"用户管理"}</h1>
        {Object.keys(rowSelection).filter(id => rowSelection[id]).length > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {"已选择 "}{Object.keys(rowSelection).filter(id => rowSelection[id]).length}{" 个用户"}
            </span>
            <Button onClick={handleSendMarketingEmail} variant="default" size="sm">
              <Mail className="mr-2 h-4 w-4" />
              {"发送营销邮件"}
            </Button>
            <Button onClick={handleDeleteUsers} variant="destructive" size="sm">
              <Trash2 className="mr-2 h-4 w-4" />
              {"硬删除"}
            </Button>
          </div>
        )}
      </div>
      <div className="flex items-center gap-4 mb-4">
        <Input
          placeholder="按邮箱搜索..."
          value={emailInput}
          onChange={(e) => setEmailInput(e.target.value)}
          className="max-w-sm"
        />
        <div className="flex items-center space-x-2">
          <Checkbox
            id="has_ordered"
            checked={hasOrderedChecked}
            onCheckedChange={(checked) =>
              setHasOrderedChecked(checked as boolean)
            }
          />
          <label
            htmlFor="has_ordered"
            className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
          >
            {"只看付费用户"}
          </label>
        </div>
        <div className="flex items-center space-x-2">
          <Checkbox
            id="is_retailer"
            checked={isRetailerChecked}
            onCheckedChange={(checked) =>
              setIsRetailerChecked(checked as boolean)
            }
          />
          <label
            htmlFor="is_retailer"
            className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
          >
            {"只看分销用户"}
          </label>
        </div>
        <div className="flex items-center gap-2">
          <DateInput
            id="startDate"
            value={startDate}
            onChange={(date) => setStartDate(date)}
          />
          <span>{"-"}</span>
          <DateInput
            value={endDate}
            onChange={(date) => setEndDate(date)}
          />
        </div>
        <Button onClick={handleFilter}>{"筛选"}</Button>
      </div>

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
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center"
                >
                  <div className="flex items-center justify-center">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                  </div>
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
                <TableCell
                  colSpan={columns.length}
                  className="h-24 text-center"
                >
                  {"无结果."}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">{"总计: "}{total}</span>
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            handlePageChange(table.getState().pagination.pageIndex - 1)
          }
          disabled={!table.getCanPreviousPage()}
        >
          {"上一页"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            handlePageChange(table.getState().pagination.pageIndex + 1)
          }
          disabled={!table.getCanNextPage()}
        >
          {"下一页"}
        </Button>
        <span className="text-sm">
          {"第 "}{table.getState().pagination.pageIndex + 1}{" 页，共 "}{pageCount}{" 页"}
        </span>
        <select
          value={table.getState().pagination.pageSize}
          onChange={(e) => {
            handlePageSizeChange(Number(e.target.value));
          }}
          className="p-2 border rounded"
        >
          {[10, 20, 30, 40, 50].map((pageSize) => (
            <option key={pageSize} value={pageSize}>
              {"每页 "}{pageSize}
            </option>
          ))}
        </select>
      </div>

      {/* 第一次确认对话框 */}
      <Dialog open={showFirstConfirm} onOpenChange={setShowFirstConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"确认删除用户"}</DialogTitle>
            <DialogDescription>
              {"您即将硬删除 "}{Object.keys(rowSelection).filter(id => rowSelection[id]).length}{" 个用户及其所有关联数据（设备、订单、邀请码、钱包等）。此操作不可撤销！"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowFirstConfirm(false)}>
              {"取消"}
            </Button>
            <Button variant="destructive" onClick={confirmFirstDelete}>
              {"继续"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 第二次确认对话框 */}
      <Dialog open={showSecondConfirm} onOpenChange={setShowSecondConfirm}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"最后确认"}</DialogTitle>
            <DialogDescription className="space-y-2">
              <p className="font-semibold text-destructive">
                {"⚠️ 这是最后一次确认！"}
              </p>
              <p>
                {"您确定要永久删除这些用户吗？所有数据将被彻底清除，包括："}
              </p>
              <ul className="list-disc list-inside space-y-1">
                <li>{"用户账户信息"}</li>
                <li>{"所有设备记录"}</li>
                <li>{"所有订单记录"}</li>
                <li>{"所有邀请码"}</li>
                <li>{"钱包及交易记录"}</li>
                <li>{"邮件发送记录"}</li>
              </ul>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowSecondConfirm(false)}
              disabled={isDeleting}
            >
              {"取消"}
            </Button>
            <Button
              variant="destructive"
              onClick={confirmSecondDelete}
              disabled={isDeleting}
            >
              {isDeleting ? "删除中..." : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
