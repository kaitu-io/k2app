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
  RefreshCw,
  CircleDashed,
  ArrowLeft,
  Download,
  Filter,
  Calendar,
  Lock
} from "lucide-react";
import { useRouter } from "@/i18n/routing";
import { toast } from "sonner";

interface WalletChange {
  id: number;
  type: "income" | "withdraw" | "refund";
  amount: number;
  balanceBefore: number;
  balanceAfter: number;
  frozenUntil?: string;
  remark?: string;
  createdAt: string;
}

export default function WalletChangesPage() {
  const t = useTranslations();
  const router = useRouter();

  const [changes, setChanges] = useState<WalletChange[]>([]);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(true);
  const [filterType, setFilterType] = useState<string>("all");
  const [pageSize, setPageSize] = useState(20);

  useEffect(() => {
    loadChanges();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, filterType, pageSize]);

  const loadChanges = async () => {
    try {
      setLoading(true);
      const params = {
        page,
        pageSize,
        type: filterType === "all" ? undefined : filterType,
      };

      const data = await api.getWalletChanges(params) as {
        items?: WalletChange[];
        pagination?: { total: number }
      };

      if (data) {
        setChanges(data.items || []);
        const total = data.pagination?.total || 0;
        setTotalPages(Math.ceil(total / pageSize));
      }
    } catch (error) {
      console.error("Failed to load changes:", error);
      toast.error(t("wallet.wallet.loadChangesFailedRetry"));
    } finally {
      setLoading(false);
    }
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

  const getChangeType = (type: string) => {
    switch (type) {
      case "income":
        return (
          <Badge className="bg-green-500 hover:bg-green-600 text-white">
            {t("wallet.wallet.typeIncome")}
          </Badge>
        );
      case "withdraw":
        return (
          <Badge className="bg-blue-500 hover:bg-blue-600 text-white">
            {t("wallet.wallet.typeWithdraw")}
          </Badge>
        );
      case "refund":
        return (
          <Badge className="bg-orange-500 hover:bg-orange-600 text-white">
            {t("wallet.wallet.typeRefund")}
          </Badge>
        );
      default:
        return <Badge variant="secondary">{type}</Badge>;
    }
  };

  const exportChanges = () => {
    // TODO: Implement export functionality
    toast.info("Export functionality coming soon");
  };

  return (
    <div className="container max-w-7xl mx-auto py-6 space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => router.push("/account/wallet")}
          >
            <ArrowLeft className="h-4 w-4 mr-2" />
            {t("common.common.back")}
          </Button>
          <h1 className="text-2xl font-bold">{t("wallet.wallet.changesTab")}</h1>
        </div>

        {/* Filters and Actions */}
        <Card className="p-4">
          <div className="flex flex-wrap items-center gap-4">
            <div className="flex items-center gap-2">
              <Filter className="h-4 w-4 text-muted-foreground" />
              <Select value={filterType} onValueChange={setFilterType}>
                <SelectTrigger className="w-[150px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t("common.common.all")}</SelectItem>
                  <SelectItem value="income">{t("wallet.wallet.typeIncome")}</SelectItem>
                  <SelectItem value="withdraw">{t("wallet.wallet.typeWithdraw")}</SelectItem>
                  <SelectItem value="refund">{t("wallet.wallet.typeRefund")}</SelectItem>
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

            <Button variant="outline" size="sm" onClick={exportChanges}>
              <Download className="h-4 w-4 mr-2" />
              {t("common.common.export")}
            </Button>

            <Button variant="outline" size="sm" onClick={loadChanges}>
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
        ) : changes.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            {t("wallet.wallet.noChanges")}
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[180px]">{t("common.common.time")}</TableHead>
                  <TableHead className="w-[100px]">{t("common.common.type")}</TableHead>
                  <TableHead className="text-right">{t("common.common.amount")}</TableHead>
                  <TableHead className="text-right">{t("wallet.wallet.balanceBefore")}</TableHead>
                  <TableHead className="text-right">{t("wallet.wallet.balanceAfter")}</TableHead>
                  <TableHead>{t("common.common.status")}</TableHead>
                  <TableHead>{t("wallet.wallet.remark")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {changes.map((change) => (
                  <TableRow key={change.id}>
                    <TableCell className="font-mono text-sm">
                      <div className="flex items-center gap-2">
                        <Calendar className="h-3 w-3 text-muted-foreground" />
                        {formatTime(change.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>{getChangeType(change.type)}</TableCell>
                    <TableCell className="text-right font-medium">
                      <span className={
                        change.type === "income" ? "text-green-600" :
                        change.type === "withdraw" ? "text-blue-600" :
                        "text-orange-600"
                      }>
                        {change.type === "income" ? "+" : "-"}{formatAmount(change.amount)}
                      </span>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {formatAmount(change.balanceBefore)}
                    </TableCell>
                    <TableCell className="text-right font-medium">
                      {formatAmount(change.balanceAfter)}
                    </TableCell>
                    <TableCell>
                      {change.frozenUntil && new Date(change.frozenUntil) > new Date() ? (
                        <div className="flex items-center gap-1">
                          <Lock className="h-3 w-3 text-orange-500" />
                          <span className="text-xs text-orange-600">
                            {t("wallet.wallet.frozen")}
                          </span>
                        </div>
                      ) : (
                        <Badge variant="outline" className="text-green-600">
                          {t("common.common.available")}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-muted-foreground max-w-[200px] truncate block">
                        {change.remark || "-"}
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>

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
    </div>
  );
}