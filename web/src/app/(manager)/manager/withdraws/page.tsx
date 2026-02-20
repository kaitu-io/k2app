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
import { api, AdminWithdrawListItem } from "@/lib/api";
import { toast } from "sonner";
import { ExternalLink } from "lucide-react";

export default function WithdrawsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [data, setData] = useState<AdminWithdrawListItem[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  // Dialog states
  const [approveDialogOpen, setApproveDialogOpen] = useState(false);
  const [rejectDialogOpen, setRejectDialogOpen] = useState(false);
  const [completeDialogOpen, setCompleteDialogOpen] = useState(false);
  const [selectedWithdrawId, setSelectedWithdrawId] = useState<number | null>(null);
  const [remark, setRemark] = useState("");
  const [txHash, setTxHash] = useState("");

  // 从 URL query 获取状态
  const page = searchParams.get("page")
    ? parseInt(searchParams.get("page") as string, 10)
    : 0;
  const pageSize = searchParams.get("pageSize")
    ? parseInt(searchParams.get("pageSize") as string, 10)
    : 10;
  const status = searchParams.get("status") || "";

  // 本地筛选状态
  const [localStatus, setLocalStatus] = useState(status);

  const formatAmount = (amount: number) => {
    return `$${(amount / 100).toFixed(2)}`;
  };

  const formatDate = (timestamp: number) => {
    if (!timestamp) return "-";
    return new Date(timestamp * 1000).toLocaleString("zh-CN");
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "pending":
        return <Badge variant="secondary">{"待处理"}</Badge>;
      case "rejected":
        return <Badge variant="destructive">{"已拒绝"}</Badge>;
      case "completed":
        return <Badge variant="default">{"已完成"}</Badge>;
      default:
        return <Badge variant="outline">{status}</Badge>;
    }
  };

  const goToUserDetail = (userUuid: string) => {
    router.push(`/manager/users/detail?uuid=${userUuid}`);
  };

  const handleApprove = (id: number) => {
    setSelectedWithdrawId(id);
    setRemark("");
    setApproveDialogOpen(true);
  };

  const handleReject = (id: number) => {
    setSelectedWithdrawId(id);
    setRemark("");
    setRejectDialogOpen(true);
  };

  const handleComplete = (id: number) => {
    setSelectedWithdrawId(id);
    setTxHash("");
    setRemark("");
    setCompleteDialogOpen(true);
  };

  const submitApprove = async () => {
    if (!selectedWithdrawId) return;

    try {
      await api.approveWithdraw(selectedWithdrawId, {
        action: "approve",
        remark: remark.trim(),
      });
      toast.success("审批成功");
      setApproveDialogOpen(false);
      fetchWithdraws();
    } catch (error) {
      console.error("Failed to approve withdraw:", error);
      toast.error("审批失败");
    }
  };

  const submitReject = async () => {
    if (!selectedWithdrawId) return;

    try {
      await api.approveWithdraw(selectedWithdrawId, {
        action: "reject",
        remark: remark.trim(),
      });
      toast.success("已拒绝提现请求");
      setRejectDialogOpen(false);
      fetchWithdraws();
    } catch (error) {
      console.error("Failed to reject withdraw:", error);
      toast.error("拒绝失败");
    }
  };

  const submitComplete = async () => {
    if (!selectedWithdrawId || !txHash.trim()) {
      toast.error("请输入交易哈希");
      return;
    }

    try {
      await api.completeWithdraw(selectedWithdrawId, {
        txHash: txHash.trim(),
        remark: remark.trim(),
      });
      toast.success("已标记为完成");
      setCompleteDialogOpen(false);
      fetchWithdraws();
    } catch (error) {
      console.error("Failed to complete withdraw:", error);
      toast.error("操作失败");
    }
  };

  const columns: ColumnDef<AdminWithdrawListItem>[] = [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => (
        <code className="text-xs bg-muted px-1 py-0.5 rounded">
          {row.getValue("id")}
        </code>
      ),
    },
    {
      accessorKey: "user",
      header: "用户",
      cell: ({ row }) => {
        const user = row.original.user;
        return (
          <Button
            variant="link"
            className="p-0 h-auto font-normal"
            onClick={() => goToUserDetail(user.uuid)}
          >
            {user.email || user.uuid}
          </Button>
        );
      },
    },
    {
      accessorKey: "amount",
      header: "金额明细",
      cell: ({ row }) => {
        const { amount, feeAmount, netAmount } = row.original;
        return (
          <div className="space-y-1">
            <div className="font-medium">{formatAmount(amount)}</div>
            <div className="text-xs text-muted-foreground">
              {"手续费: "}{formatAmount(feeAmount)} {"→ 实际: "}{formatAmount(netAmount)}
            </div>
          </div>
        );
      },
    },
    {
      accessorKey: "account",
      header: "提现账户",
      cell: ({ row }) => {
        const account = row.original.account;
        const isPaypal = account.accountType === "paypal";
        return (
          <div className="space-y-1">
            <div className="text-sm flex items-center gap-1">
              <Badge variant="outline">
                {isPaypal ? "PayPal" : account.accountType.toUpperCase()}
              </Badge>
              {account.currency && (
                <Badge variant="secondary" className="text-xs">
                  {account.currency.toUpperCase()}
                </Badge>
              )}
            </div>
            <code className="text-xs text-muted-foreground break-all">
              {account.accountId}
            </code>
          </div>
        );
      },
    },
    {
      accessorKey: "status",
      header: "状态",
      cell: ({ row }) => getStatusBadge(row.getValue("status")),
    },
    {
      accessorKey: "transaction",
      header: "交易",
      cell: ({ row }) => {
        const transaction = row.original.transaction;
        if (!transaction?.txHash) return <span className="text-muted-foreground">{"-"}</span>;

        return (
          <div className="space-y-1">
            <code className="text-xs bg-muted px-1 py-0.5 rounded break-all">
              {transaction.txHash.slice(0, 10)}{"..."}{transaction.txHash.slice(-8)}
            </code>
            {transaction.explorerUrl && (
              <a
                href={transaction.explorerUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 text-xs text-blue-600 hover:underline"
              >
                {"查看交易"} <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
        );
      },
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      cell: ({ row }) => formatDate(row.getValue("createdAt")),
    },
    {
      accessorKey: "processedAt",
      header: "处理时间",
      cell: ({ row }) => {
        const processedAt = row.original.processedAt;
        return processedAt ? formatDate(processedAt) : "-";
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const status = row.original.status;
        const id = row.original.id;

        if (status === "pending") {
          return (
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => handleApprove(id)}
              >
                {"审批"}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => handleComplete(id)}
              >
                {"打款完成"}
              </Button>
              <Button
                size="sm"
                variant="destructive"
                onClick={() => handleReject(id)}
              >
                {"拒绝"}
              </Button>
            </div>
          );
        }

        return <span className="text-muted-foreground">{"-"}</span>;
      },
    },
  ];

  const fetchWithdraws = useCallback(async () => {
    setIsLoading(true);
    try {
      const params: Record<string, string | number> = {
        page,
        pageSize,
      };

      if (status) {
        params.status = status;
      }

      const response = await api.listWithdrawRequests(params);

      setData(response.items || []);
      if (response.pagination) {
        setPageCount(Math.ceil(response.pagination.total / response.pagination.pageSize));
        setTotal(response.pagination.total);
      }
    } catch (error) {
      console.error("Failed to fetch withdraws:", error);
    } finally {
      setIsLoading(false);
    }
  }, [page, pageSize, status]);

  useEffect(() => {
    fetchWithdraws();
  }, [fetchWithdraws]);

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

    if (localStatus && localStatus !== "all") {
      params.set("status", localStatus);
    }

    router.push(`/manager/withdraws?${params.toString()}`);
  };

  const handleReset = () => {
    setLocalStatus("");
    router.push('/manager/withdraws');
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"提现管理"}</h1>
        <p className="text-muted-foreground">{"管理系统中的所有提现请求"}</p>
      </div>

      {/* 筛选区域 */}
      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1">
          <label className="text-sm font-medium">{"状态"}</label>
          <select
            className="w-full p-2 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-white rounded-md"
            value={localStatus}
            onChange={(e) => setLocalStatus(e.target.value)}
          >
            <option value="">{"全部"}</option>
            <option value="pending">{"待处理"}</option>
            <option value="completed">{"已完成"}</option>
            <option value="rejected">{"已拒绝"}</option>
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
            router.push(`/manager/withdraws?${params.toString()}`);
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
            router.push(`/manager/withdraws?${params.toString()}`);
          }}
          disabled={page >= pageCount - 1}
        >
          {"下一页"}
        </Button>
      </div>

      {/* 审批对话框 */}
      <Dialog open={approveDialogOpen} onOpenChange={setApproveDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"审批提现请求"}</DialogTitle>
            <DialogDescription>
              {"审批通过后，请线下打款并标记为完成"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">{"备注（可选）"}</label>
              <Textarea
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
                placeholder="输入备注信息..."
                rows={3}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setApproveDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button onClick={submitApprove}>
              {"确认审批"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 拒绝对话框 */}
      <Dialog open={rejectDialogOpen} onOpenChange={setRejectDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"拒绝提现请求"}</DialogTitle>
            <DialogDescription>
              {"拒绝后将无法恢复，请谨慎操作"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">{"拒绝原因（可选）"}</label>
              <Textarea
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
                placeholder="输入拒绝原因..."
                rows={3}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRejectDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button variant="destructive" onClick={submitReject}>
              {"确认拒绝"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 完成打款对话框 */}
      <Dialog open={completeDialogOpen} onOpenChange={setCompleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"标记为已完成"}</DialogTitle>
            <DialogDescription>
              {"请在线下完成打款后，填写交易凭证（区块链交易哈希或 PayPal 交易 ID）"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">{"交易凭证 *"}</label>
              <Input
                value={txHash}
                onChange={(e) => setTxHash(e.target.value)}
                placeholder="输入交易哈希或 PayPal 交易 ID..."
              />
            </div>
            <div>
              <label className="text-sm font-medium">{"备注（可选）"}</label>
              <Textarea
                value={remark}
                onChange={(e) => setRemark(e.target.value)}
                placeholder="输入备注信息..."
                rows={3}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCompleteDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button onClick={submitComplete}>
              {"确认完成"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
