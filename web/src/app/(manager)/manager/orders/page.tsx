"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useSearchParams } from "next/navigation";
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
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { api, AdminOrderListItem } from "@/lib/api";

export default function OrdersPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<AdminOrderListItem[]>([]);
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
  const loginProvider = searchParams.get("loginProvider") || "";
  const loginIdentity = searchParams.get("loginIdentity") || "";
  const isPaid = searchParams.get("isPaid");

  // 本地筛选状态
  const [localLoginProvider, setLocalLoginProvider] = useState(loginProvider);
  const [localLoginIdentity, setLocalLoginIdentity] = useState(loginIdentity);
  const [localIsPaid, setLocalIsPaid] = useState(isPaid || "");

  const formatAmount = (amount: number) => {
    return `$${(amount / 100).toFixed(2)}`;
  };

  const formatDate = (timestamp: number) => {
    if (!timestamp) return "-";
    return new Date(timestamp * 1000).toLocaleString("zh-CN");
  };

  const goToUserDetail = (userUuid: string) => {
    router.push(`/manager/users/detail?uuid=${userUuid}`);
  };

  const columns: ColumnDef<AdminOrderListItem>[] = [
    {
      accessorKey: "uuid",
      header: "订单ID",
      cell: ({ row }) => (
        <code className="text-xs bg-muted px-1 py-0.5 rounded">
          {row.getValue("uuid")}
        </code>
      ),
    },
    {
      accessorKey: "title",
      header: "标题",
    },
    {
      accessorKey: "user",
      header: "用户邮箱",
      cell: ({ row }) => {
        const user = row.original.user;
        return (
          <Button
            variant="link"
            className="p-0 h-auto font-normal"
            onClick={() => goToUserDetail(user.uuid)}
          >
            {user.email || "未设置"}
          </Button>
        );
      },
    },
    {
      accessorKey: "originAmount",
      header: "原价",
      cell: ({ row }) => formatAmount(row.getValue("originAmount")),
    },
    {
      accessorKey: "campaignReduceAmount",
      header: "优惠",
      cell: ({ row }) => {
        const amount = row.getValue("campaignReduceAmount") as number;
        return amount > 0 ? `-${formatAmount(amount)}` : "-";
      },
    },
    {
      accessorKey: "payAmount",
      header: "实付",
      cell: ({ row }) => (
        <span className="font-medium">
          {formatAmount(row.getValue("payAmount"))}
        </span>
      ),
    },
    {
      accessorKey: "isPaid",
      header: "状态",
      cell: ({ row }) => {
        const isPaid = row.getValue("isPaid") as boolean;
        return (
          <Badge variant={isPaid ? "default" : "secondary"}>
            {isPaid ? "已支付" : "待支付"}
          </Badge>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      cell: ({ row }) => formatDate(row.getValue("createdAt")),
    },
    {
      accessorKey: "paidAt",
      header: "支付时间",
      cell: ({ row }) => formatDate(row.getValue("paidAt")),
    },
    {
      accessorKey: "cashback",
      header: "分销分成",
      cell: ({ row }) => {
        const cashback = row.original.cashback;
        if (!cashback) return <span className="text-muted-foreground">{"-"}</span>;

        return (
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Button
                variant="link"
                className="p-0 h-auto font-normal text-sm"
                onClick={() => goToUserDetail(cashback.retailerUuid)}
              >
                {cashback.retailerEmail}
              </Button>
            </div>
            <div className="text-xs">
              <span className="font-medium">{formatAmount(cashback.amount)}</span>
              {" · "}
              <Badge variant={cashback.status === "completed" ? "default" : "secondary"} className="text-xs">
                {cashback.status === "pending" ? "冻结中" : "已解冻"}
              </Badge>
            </div>
          </div>
        );
      },
    },
  ];

  useEffect(() => {
    const fetchOrders = async () => {
      setIsLoading(true);
      try {
        const params: Record<string, string | number | boolean> = {
          page,
          pageSize,
        };

        if (loginProvider && loginIdentity) {
          params.loginProvider = loginProvider;
          params.loginIdentity = loginIdentity.trim();
        }

        if (isPaid !== null && isPaid !== "") {
          params.isPaid = isPaid === "true";
        }

        const response = await api.getOrders(params);

        setData(response.items || []);
        setPageCount(Math.ceil(response.pagination.total / response.pagination.pageSize));
        setTotal(response.pagination.total);
      } catch (error) {
        console.error("Failed to fetch orders:", error);
      } finally {
        setIsLoading(false);
      }
    };
    fetchOrders();
  }, [page, pageSize, loginProvider, loginIdentity, isPaid]);

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

    if (localLoginProvider && localLoginIdentity.trim()) {
      params.set("loginProvider", localLoginProvider);
      params.set("loginIdentity", localLoginIdentity.trim());
    }

    if (localIsPaid && localIsPaid !== "all") {
      params.set("isPaid", localIsPaid);
    }

    router.push(`/manager/orders?${params.toString()}`);
  };

  const handleReset = () => {
    setLocalLoginProvider("");
    setLocalLoginIdentity("");
    setLocalIsPaid("");
    router.push('/manager/orders');
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"订单管理"}</h1>
        <p className="text-muted-foreground">{"管理系统中的所有订单"}</p>
      </div>

      {/* 筛选区域 */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1">
          <label className="text-sm font-medium">{"登录类型"}</label>
          <select
            className="w-full p-2 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-white rounded-md"
            value={localLoginProvider}
            onChange={(e) => setLocalLoginProvider(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="email">{"邮箱"}</option>
            <option value="google">{"Google"}</option>
            <option value="apple">{"Apple"}</option>
          </select>
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"登录标识"}</label>
          <Input
            placeholder="输入邮箱或其他登录标识"
            value={localLoginIdentity}
            onChange={(e) => setLocalLoginIdentity(e.target.value)}
          />
        </div>
        <div className="flex-1">
          <label className="text-sm font-medium">{"支付状态"}</label>
          <select
            className="w-full p-2 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-white rounded-md"
            value={localIsPaid}
            onChange={(e) => setLocalIsPaid(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="true">{"已支付"}</option>
            <option value="false">{"待支付"}</option>
          </select>
        </div>
        <div className="flex gap-2">
          <Button onClick={handleFilter}>{"筛选"}</Button>
          <Button variant="outline" onClick={handleReset}>
            {"重置"}
          </Button>
        </div>
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
          onClick={() => {
            const params = new URLSearchParams(searchParams.toString());
            params.set('page', (page - 1).toString());
            router.push(`/manager/orders?${params.toString()}`);
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
            params.set('page', (page + 1).toString());
            router.push(`/manager/orders?${params.toString()}`);
          }}
          disabled={page >= pageCount - 1}
        >
          {"下一页"}
        </Button>
      </div>
    </div>
  );
}