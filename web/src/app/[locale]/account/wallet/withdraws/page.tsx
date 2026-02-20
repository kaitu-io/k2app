"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import { api } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  CircleDashed,
  CheckCircle,
  XCircle,
  Clock,
  ExternalLink,
  ArrowLeft,
  Download,
  Filter,
  RefreshCw,
  Calendar,
  Wallet,
  Info
} from "lucide-react";
import { useRouter } from "@/i18n/routing";
import { toast } from "sonner";
import { WithdrawDialog } from "@/components/wallet/WithdrawDialog";

interface Withdraw {
  id: number;
  amount: number;
  accountType: string;
  walletAddress: string;
  network: string;
  status: "pending" | "approved" | "processing" | "completed" | "rejected" | "cancelled";
  txHash?: string;
  txExplorerUrl?: string;
  rejectReason?: string;
  remark?: string;
  createdAt: string;
}

interface WithdrawAccount {
  id: number;
  accountType: string;      // tron, polygon, bsc, arbitrum, paypal
  accountId: string;        // 钱包地址或 PayPal 邮箱
  currency: string;         // usdt, usdc, usd
  label?: string;
  isDefault: boolean;
}

interface WalletData {
  availableBalance: number;
}

export default function WalletWithdrawsPage() {
  const t = useTranslations();
  const router = useRouter();

  const [withdraws, setWithdraws] = useState<Withdraw[]>([]);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(true);
  const [filterStatus, setFilterStatus] = useState<string>("all");
  const [pageSize, setPageSize] = useState(20);

  const [showWithdrawDialog, setShowWithdrawDialog] = useState(false);
  const [wallet, setWallet] = useState<WalletData | null>(null);
  const [accounts, setAccounts] = useState<WithdrawAccount[]>([]);

  useEffect(() => {
    loadWithdraws();
    loadWallet();
    loadAccounts();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    loadWithdraws();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, filterStatus, pageSize]);

  const loadWithdraws = async () => {
    try {
      setLoading(true);
      const params = {
        page,
        pageSize,
        status: filterStatus === "all" ? undefined : filterStatus,
      };

      const data = await api.getWithdrawRequests(params) as {
        items?: Withdraw[];
        pagination?: { total: number }
      };

      if (data) {
        setWithdraws(data.items || []);
        const total = data.pagination?.total || 0;
        setTotalPages(Math.ceil(total / pageSize));
      }
    } catch (error) {
      console.error("Failed to load withdraws:", error);
      toast.error(t("wallet.wallet.loadWithdrawsFailedRetry"));
    } finally {
      setLoading(false);
    }
  };

  const loadWallet = async () => {
    try {
      const data = await api.getWallet() as WalletData;
      if (data) {
        setWallet(data);
      }
    } catch (error) {
      console.error("Failed to load wallet:", error);
    }
  };

  const loadAccounts = async () => {
    try {
      const data = await api.getWithdrawAccounts() as WithdrawAccount[];
      if (data) {
        setAccounts(data);
      }
    } catch (error) {
      console.error("Failed to load accounts:", error);
    }
  };

  const handleWithdrawSuccess = () => {
    loadWithdraws();
    loadWallet();
  };

  const formatAmount = (cents: number) => {
    return `$${(cents / 100).toFixed(2)}`;
  };

  const formatTime = (timestamp: string) => {
    return new Date(timestamp).toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    });
  };

  const formatAddress = (address: string) => {
    if (address.length <= 16) return address;
    return `${address.slice(0, 8)}...${address.slice(-6)}`;
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "pending":
        return (
          <Badge variant="outline" className="border-yellow-500 text-yellow-600">
            <Clock className="h-3 w-3 mr-1" />
            {t("wallet.wallet.statusPending")}
          </Badge>
        );
      case "approved":
        return (
          <Badge variant="outline" className="border-blue-500 text-blue-600">
            <CheckCircle className="h-3 w-3 mr-1" />
            {t("wallet.wallet.statusApproved")}
          </Badge>
        );
      case "processing":
        return (
          <Badge variant="outline" className="border-orange-500 text-orange-600">
            <CircleDashed className="h-3 w-3 mr-1 animate-spin" />
            {t("wallet.wallet.statusProcessing")}
          </Badge>
        );
      case "completed":
        return (
          <Badge variant="outline" className="border-green-500 text-green-600">
            <CheckCircle className="h-3 w-3 mr-1" />
            {t("wallet.wallet.statusCompleted")}
          </Badge>
        );
      case "rejected":
        return (
          <Badge variant="outline" className="border-red-500 text-red-600">
            <XCircle className="h-3 w-3 mr-1" />
            {t("wallet.wallet.statusRejected")}
          </Badge>
        );
      case "cancelled":
        return (
          <Badge variant="outline" className="border-gray-500 text-gray-600">
            <XCircle className="h-3 w-3 mr-1" />
            {t("wallet.wallet.statusCancelled")}
          </Badge>
        );
      default:
        return <Badge variant="secondary">{status}</Badge>;
    }
  };

  const getNetworkName = (network: string) => {
    const networkMap: Record<string, string> = {
      'tron': 'TRON',
      'ethereum': 'ETH',
      'bsc': 'BSC',
      'polygon': 'MATIC',
      'arbitrum': 'ARB',
      'optimism': 'OP',
      'avalanche': 'AVAX',
      'solana': 'SOL',
    };
    return networkMap[network.toLowerCase()] || network.toUpperCase();
  };

  const exportWithdraws = () => {
    // TODO: Implement export functionality
    toast.info("Export functionality coming soon");
  };

  return (
    <div className="container max-w-7xl mx-auto py-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => router.push("/account/wallet")}
            >
              <ArrowLeft className="h-4 w-4 mr-2" />
              {t("common.common.back")}
            </Button>
            <h1 className="text-2xl font-bold">{t("wallet.wallet.withdrawsTab")}</h1>
          </div>

          <Button onClick={() => setShowWithdrawDialog(true)}>
            <Wallet className="h-4 w-4 mr-2" />
            {t("wallet.wallet.withdraw")}
          </Button>
        </div>

        {/* Filters and Actions */}
        <Card className="p-4">
          <div className="flex flex-wrap items-center gap-4">
            <div className="flex items-center gap-2">
              <Filter className="h-4 w-4 text-muted-foreground" />
              <Select value={filterStatus} onValueChange={setFilterStatus}>
                <SelectTrigger className="w-[150px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t("common.common.all")}</SelectItem>
                  <SelectItem value="pending">{t("wallet.wallet.statusPending")}</SelectItem>
                  <SelectItem value="approved">{t("wallet.wallet.statusApproved")}</SelectItem>
                  <SelectItem value="processing">{t("wallet.wallet.statusProcessing")}</SelectItem>
                  <SelectItem value="completed">{t("wallet.wallet.statusCompleted")}</SelectItem>
                  <SelectItem value="rejected">{t("wallet.wallet.statusRejected")}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">{t("common.common.pageSize")}</span>
              <Select value={pageSize.toString()} onValueChange={(v) => setPageSize(parseInt(v))}>
                <SelectTrigger className="w-[100px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="20">{"20"}</SelectItem>
                  <SelectItem value="50">{"50"}</SelectItem>
                  <SelectItem value="100">{"100"}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex-1" />

            <Button variant="outline" size="sm" onClick={exportWithdraws}>
              <Download className="h-4 w-4 mr-2" />
              {t("common.common.export")}
            </Button>

            <Button variant="outline" size="sm" onClick={loadWithdraws}>
              <RefreshCw className="h-4 w-4 mr-2" />
              {t("common.common.refresh")}
            </Button>
          </div>
        </Card>
      </div>

      {/* Table */}
      <Card>
        {loading ? (
          <div className="flex justify-center py-12">
            <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        ) : withdraws.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            {t("wallet.wallet.noWithdraws")}
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[180px]">{t("common.common.time")}</TableHead>
                    <TableHead className="text-right">{t("common.common.amount")}</TableHead>
                    <TableHead>{t("wallet.wallet.network")}</TableHead>
                    <TableHead>{t("wallet.wallet.walletAddress")}</TableHead>
                    <TableHead>{t("common.common.status")}</TableHead>
                    <TableHead>{t("wallet.wallet.transaction")}</TableHead>
                    <TableHead>{t("wallet.wallet.remark")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {withdraws.map((withdraw) => (
                    <TableRow key={withdraw.id}>
                      <TableCell className="font-mono text-sm">
                        <div className="flex items-center gap-2">
                          <Calendar className="h-3 w-3 text-muted-foreground" />
                          {formatTime(withdraw.createdAt)}
                        </div>
                      </TableCell>
                      <TableCell className="text-right font-bold text-blue-600">
                        {formatAmount(withdraw.amount)}
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {getNetworkName(withdraw.network)}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <code className="text-xs bg-muted px-1 py-0.5 rounded">
                          {formatAddress(withdraw.walletAddress)}
                        </code>
                      </TableCell>
                      <TableCell>{getStatusBadge(withdraw.status)}</TableCell>
                      <TableCell>
                        {withdraw.txHash ? (
                          withdraw.txExplorerUrl ? (
                            <a
                              href={withdraw.txExplorerUrl}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center gap-1 text-blue-600 hover:underline"
                            >
                              <span className="text-xs">{formatAddress(withdraw.txHash)}</span>
                              <ExternalLink className="h-3 w-3" />
                            </a>
                          ) : (
                            <code className="text-xs">{formatAddress(withdraw.txHash)}</code>
                          )
                        ) : (
                          <span className="text-muted-foreground">{"-"}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {withdraw.status === "rejected" && withdraw.rejectReason ? (
                          <div className="flex items-start gap-1">
                            <Info className="h-3 w-3 text-red-500 mt-0.5" />
                            <span className="text-xs text-red-600">{withdraw.rejectReason}</span>
                          </div>
                        ) : withdraw.remark ? (
                          <span className="text-sm text-muted-foreground max-w-[200px] truncate block">
                            {withdraw.remark}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">{"-"}</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between px-6 py-4 border-t">
                <div className="text-sm text-muted-foreground">
                  {t("common.common.page")} {page} {"/"} {totalPages}
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page === 1}
                    onClick={() => setPage(page - 1)}
                  >
                    {t("common.common.previous")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={page === totalPages}
                    onClick={() => setPage(page + 1)}
                  >
                    {t("common.common.next")}
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </Card>

      {/* Withdraw Dialog */}
      <WithdrawDialog
        open={showWithdrawDialog}
        onClose={() => setShowWithdrawDialog(false)}
        availableBalance={wallet?.availableBalance || 0}
        onSuccess={handleWithdrawSuccess}
        accounts={accounts}
        onOpenAccountManager={() => {
          setShowWithdrawDialog(false);
          router.push("/account/wallet/accounts");
        }}
      />
    </div>
  );
}