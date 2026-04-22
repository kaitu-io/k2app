"use client";

import { useState, useEffect, useCallback } from "react";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { api, AdminOrderListItem, isPendingApproval } from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";

export default function OrdersPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<AdminOrderListItem[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  // Refund dialog state
  const [refundDialogOpen, setRefundDialogOpen] = useState(false);
  const [selectedOrder, setSelectedOrder] = useState<AdminOrderListItem | null>(null);
  const [refundReason, setRefundReason] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

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
  const isRefunded = searchParams.get("isRefunded");

  // 本地筛选状态
  const [localLoginProvider, setLocalLoginProvider] = useState(loginProvider);
  const [localLoginIdentity, setLocalLoginIdentity] = useState(loginIdentity);
  const [localIsPaid, setLocalIsPaid] = useState(isPaid || "");
  const [localIsRefunded, setLocalIsRefunded] = useState(isRefunded || "");

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

  const fetchOrders = useCallback(async () => {
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

      if (isRefunded !== null && isRefunded !== "") {
        params.isRefunded = isRefunded === "true";
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
  }, [page, pageSize, loginProvider, loginIdentity, isPaid, isRefunded]);

  useEffect(() => {
    fetchOrders();
  }, [fetchOrders]);

  const handleRefundClick = (order: AdminOrderListItem) => {
    setSelectedOrder(order);
    setRefundReason("");
    setRefundDialogOpen(true);
  };

  const submitRefund = async () => {
    if (!selectedOrder) return;
    const reason = refundReason.trim();
    if (reason.length < 2) {
      toast.error("退款原因至少 2 个字符");
      return;
    }

    setIsSubmitting(true);
    try {
      await api.refundOrder(selectedOrder.uuid, reason);
      toast.success("退款成功");
      setRefundDialogOpen(false);
      fetchOrders();
    } catch (error: unknown) {
      if (isPendingApproval(error)) {
        toast.success("已提交审批，等待其他管理员确认");
        setRefundDialogOpen(false);
        return;
      }
      console.error("Failed to refund order:", error);
      const code = (error as { code?: number })?.code;
      toast.error(code ? getApiErrorMessageZh(code, "退款失败") : "退款失败");
    } finally {
      setIsSubmitting(false);
    }
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
        const { isPaid, isRefunded, refundedAt, refundReason } = row.original;
        if (isRefunded) {
          return (
            <div className="space-y-1">
              <Badge variant="destructive">{"已退款"}</Badge>
              {refundedAt ? (
                <div className="text-xs text-muted-foreground">
                  {formatDate(refundedAt)}
                </div>
              ) : null}
              {refundReason ? (
                <div className="text-xs text-muted-foreground max-w-[200px] truncate" title={refundReason}>
                  {refundReason}
                </div>
              ) : null}
            </div>
          );
        }
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
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const order = row.original;
        const canRefund = order.isPaid && !order.isRefunded;
        if (!canRefund) {
          return <span className="text-muted-foreground text-xs">{"-"}</span>;
        }
        return (
          <Button
            size="sm"
            variant="destructive"
            onClick={() => handleRefundClick(order)}
          >
            {"退款"}
          </Button>
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

    if (localLoginProvider && localLoginIdentity.trim()) {
      params.set("loginProvider", localLoginProvider);
      params.set("loginIdentity", localLoginIdentity.trim());
    }

    if (localIsPaid && localIsPaid !== "all") {
      params.set("isPaid", localIsPaid);
    }

    if (localIsRefunded && localIsRefunded !== "all") {
      params.set("isRefunded", localIsRefunded);
    }

    router.push(`/manager/orders?${params.toString()}`);
  };

  const handleReset = () => {
    setLocalLoginProvider("");
    setLocalLoginIdentity("");
    setLocalIsPaid("");
    setLocalIsRefunded("");
    router.push('/manager/orders');
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"订单管理"}</h1>
        <p className="text-muted-foreground">{"管理系统中的所有订单"}</p>
      </div>

      {/* 筛选区域 */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg flex-wrap">
        <div className="flex-1 min-w-[160px]">
          <label className="text-sm font-medium">{"登录类型"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localLoginProvider}
            onChange={(e) => setLocalLoginProvider(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="email">{"邮箱"}</option>
            <option value="google">{"Google"}</option>
            <option value="apple">{"Apple"}</option>
          </select>
        </div>
        <div className="flex-1 min-w-[200px]">
          <label className="text-sm font-medium">{"登录标识"}</label>
          <Input
            placeholder="输入邮箱或其他登录标识"
            value={localLoginIdentity}
            onChange={(e) => setLocalLoginIdentity(e.target.value)}
          />
        </div>
        <div className="flex-1 min-w-[140px]">
          <label className="text-sm font-medium">{"支付状态"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localIsPaid}
            onChange={(e) => setLocalIsPaid(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="true">{"已支付"}</option>
            <option value="false">{"待支付"}</option>
          </select>
        </div>
        <div className="flex-1 min-w-[140px]">
          <label className="text-sm font-medium">{"退款状态"}</label>
          <select
            className="w-full p-2 border border-border bg-muted text-foreground rounded-md"
            value={localIsRefunded}
            onChange={(e) => setLocalIsRefunded(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="true">{"已退款"}</option>
            <option value="false">{"未退款"}</option>
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

      {/* 退款对话框 */}
      <Dialog open={refundDialogOpen} onOpenChange={setRefundDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"订单退款"}</DialogTitle>
            <DialogDescription>
              {"退款将向用户钱包打款、撤销已发放的 Pro 天数、冲销分销返现。非超级管理员需另一位管理员审批后生效。"}
            </DialogDescription>
          </DialogHeader>
          {selectedOrder ? (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-2 text-sm">
                <div>
                  <div className="text-muted-foreground">{"订单"}</div>
                  <code className="text-xs bg-muted px-1 py-0.5 rounded break-all">
                    {selectedOrder.uuid}
                  </code>
                </div>
                <div>
                  <div className="text-muted-foreground">{"用户"}</div>
                  <div>{selectedOrder.user.email || selectedOrder.user.uuid}</div>
                </div>
                <div>
                  <div className="text-muted-foreground">{"退款金额"}</div>
                  <div className="font-medium">{formatAmount(selectedOrder.payAmount)}</div>
                </div>
                <div>
                  <div className="text-muted-foreground">{"商品"}</div>
                  <div>{selectedOrder.title}</div>
                </div>
              </div>
              <div>
                <label className="text-sm font-medium">
                  {"退款原因 "}<span className="text-destructive">{"*"}</span>
                </label>
                <Textarea
                  value={refundReason}
                  onChange={(e) => setRefundReason(e.target.value)}
                  placeholder="2-500 字，会写入审计记录"
                  rows={3}
                  maxLength={500}
                />
              </div>
            </div>
          ) : null}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRefundDialogOpen(false)}
              disabled={isSubmitting}
            >
              {"取消"}
            </Button>
            <Button
              variant="destructive"
              onClick={submitRefund}
              disabled={isSubmitting}
            >
              {isSubmitting ? "提交中..." : "确认退款"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
