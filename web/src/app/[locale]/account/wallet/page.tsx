"use client";

import { useState, useEffect, useCallback } from "react";
import { useTranslations } from "next-intl";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Wallet,
  Lock,
  TrendingUp,
  DollarSign,
  History,
  FileText,
  Settings,
  ArrowRight,
  CircleDashed,
  RefreshCw
} from "lucide-react";
import { useRouter } from "@/i18n/routing";
import { toast } from "sonner";
import { WithdrawDialog } from "@/components/wallet/WithdrawDialog";

interface WalletData {
  id: number;
  balance: number;
  totalIncome: number;
  totalWithdrawn: number;
  availableBalance: number;
  frozenBalance: number;
}

interface WithdrawAccount {
  id: number;
  accountType: string;      // tron, polygon, bsc, arbitrum, paypal
  accountId: string;        // 钱包地址或 PayPal 邮箱
  currency: string;         // usdt, usdc, usd
  label?: string;
  isDefault: boolean;
}

export default function WalletPage() {
  const t = useTranslations();
  const router = useRouter();

  const [wallet, setWallet] = useState<WalletData | null>(null);
  const [loadingWallet, setLoadingWallet] = useState(true);
  const [accounts, setAccounts] = useState<WithdrawAccount[]>([]);
  const [showWithdrawDialog, setShowWithdrawDialog] = useState(false);

  const loadWallet = useCallback(async () => {
    try {
      setLoadingWallet(true);
      const data = await api.getWallet();
      if (data) {
        setWallet(data as WalletData);
      }
    } catch (error) {
      console.error("Failed to load wallet:", error);
      toast.error(t("wallet.wallet.loadWalletFailedRetry"));
    } finally {
      setLoadingWallet(false);
    }
  }, [t]);

  const loadWithdrawAccounts = useCallback(async () => {
    try {
      const data = await api.getWithdrawAccounts();
      if (data) {
        setAccounts(data as WithdrawAccount[]);
      }
    } catch (error) {
      console.error("Failed to load withdraw accounts:", error);
    }
  }, []);

  useEffect(() => {
    loadWallet();
    loadWithdrawAccounts();
  }, [loadWallet, loadWithdrawAccounts]);

  const handleWithdraw = () => {
    if (accounts.length === 0) {
      toast.warning(t("wallet.wallet.noAccountsWarning"));
      router.push("/account/wallet/accounts");
      return;
    }
    setShowWithdrawDialog(true);
  };

  const handleWithdrawSuccess = () => {
    loadWallet();
    setShowWithdrawDialog(false);
  };

  const formatAmount = (cents: number) => {
    return `$${(cents / 100).toFixed(2)}`;
  };

  if (loadingWallet) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!wallet) {
    return (
      <Card className="p-6">
        <div className="text-center space-y-4">
          <p className="text-destructive">{t("wallet.wallet.loadWalletFailed")}</p>
          <Button onClick={loadWallet}>
            <RefreshCw className="h-4 w-4 mr-2" />
            {t("common.common.retry")}
          </Button>
        </div>
      </Card>
    );
  }

  return (
    <div className="container max-w-6xl mx-auto py-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">{t("wallet.wallet.title")}</h1>
        <Button onClick={loadWallet} variant="outline" size="sm">
          <RefreshCw className="h-4 w-4 mr-2" />
          {t("common.common.refresh")}
        </Button>
      </div>

      {/* Balance Overview */}
      <Card className="p-8 bg-gradient-to-br from-primary/5 via-transparent to-green-50 dark:from-primary/10 dark:via-transparent dark:to-green-950">
        <div className="space-y-8">
          {/* Main Balance */}
          <div>
            <p className="text-sm text-muted-foreground mb-2">
              {t("wallet.wallet.availableBalance")}
            </p>
            <div className="flex items-baseline gap-4">
              <h2 className="text-5xl font-bold text-primary">
                {formatAmount(wallet.availableBalance)}
              </h2>
              <Button size="lg" onClick={handleWithdraw}>
                <Wallet className="h-5 w-5 mr-2" />
                {t("wallet.wallet.withdraw")}
              </Button>
            </div>
          </div>

          {/* Stats Grid */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Lock className="h-4 w-4" />
                {t("wallet.wallet.frozenBalance")}
              </div>
              <p className="text-2xl font-semibold text-orange-600 dark:text-orange-400">
                {formatAmount(wallet.frozenBalance)}
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <TrendingUp className="h-4 w-4" />
                {t("wallet.wallet.totalIncome")}
              </div>
              <p className="text-2xl font-semibold text-green-600 dark:text-green-400">
                {formatAmount(wallet.totalIncome)}
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <DollarSign className="h-4 w-4" />
                {t("wallet.wallet.totalWithdrawn")}
              </div>
              <p className="text-2xl font-semibold text-blue-600 dark:text-blue-400">
                {formatAmount(wallet.totalWithdrawn)}
              </p>
            </div>
          </div>
        </div>
      </Card>

      {/* Quick Actions */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Changes History Card */}
        <Card className="p-6 hover:shadow-lg transition-shadow cursor-pointer group"
              onClick={() => router.push("/account/wallet/changes")}>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="p-3 bg-green-100 dark:bg-green-900/30 rounded-lg">
                <History className="h-6 w-6 text-green-600 dark:text-green-400" />
              </div>
              <ArrowRight className="h-5 w-5 text-muted-foreground group-hover:translate-x-1 transition-transform" />
            </div>
            <div>
              <h3 className="font-semibold text-lg">{t("wallet.wallet.changesTab")}</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {t("wallet.wallet.changesDescription")}
              </p>
            </div>
          </div>
        </Card>

        {/* Withdraw Records Card */}
        <Card className="p-6 hover:shadow-lg transition-shadow cursor-pointer group"
              onClick={() => router.push("/account/wallet/withdraws")}>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="p-3 bg-blue-100 dark:bg-blue-900/30 rounded-lg">
                <FileText className="h-6 w-6 text-blue-600 dark:text-blue-400" />
              </div>
              <ArrowRight className="h-5 w-5 text-muted-foreground group-hover:translate-x-1 transition-transform" />
            </div>
            <div>
              <h3 className="font-semibold text-lg">{t("wallet.wallet.withdrawsTab")}</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {t("wallet.wallet.withdrawsDescription")}
              </p>
            </div>
          </div>
        </Card>

        {/* Account Management Card */}
        <Card className="p-6 hover:shadow-lg transition-shadow cursor-pointer group"
              onClick={() => router.push("/account/wallet/accounts")}>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="p-3 bg-purple-100 dark:bg-purple-900/30 rounded-lg">
                <Settings className="h-6 w-6 text-purple-600 dark:text-purple-400" />
              </div>
              <ArrowRight className="h-5 w-5 text-muted-foreground group-hover:translate-x-1 transition-transform" />
            </div>
            <div>
              <h3 className="font-semibold text-lg">{t("wallet.wallet.manageAccounts")}</h3>
              <p className="text-sm text-muted-foreground mt-1">
                {t("wallet.wallet.accountsDescription")}
              </p>
            </div>
          </div>
        </Card>
      </div>

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