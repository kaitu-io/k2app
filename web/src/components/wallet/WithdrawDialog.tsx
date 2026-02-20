"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2, AlertCircle, Plus } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";

interface WithdrawAccount {
  id: number;
  accountType: string;      // tron, polygon, bsc, arbitrum, paypal
  accountId: string;        // 钱包地址或 PayPal 邮箱
  currency: string;         // usdt, usdc, usd
  label?: string;
  isDefault: boolean;
}

interface WithdrawDialogProps {
  open: boolean;
  onClose: () => void;
  availableBalance: number;
  onSuccess: () => void;
  accounts: WithdrawAccount[];
  onOpenAccountManager: () => void;
}

export function WithdrawDialog({
  open,
  onClose,
  availableBalance,
  onSuccess,
  accounts,
  onOpenAccountManager
}: WithdrawDialogProps) {
  const t = useTranslations();

  const [amount, setAmount] = useState("");
  const [selectedAccountId, setSelectedAccountId] = useState<string>("");
  const [userRemark, setUserRemark] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);

  // Set default account when accounts load
  useEffect(() => {
    if (accounts.length > 0 && !selectedAccountId) {
      const defaultAccount = accounts.find(a => a.isDefault) || accounts[0];
      setSelectedAccountId(defaultAccount.id.toString());
    }
  }, [accounts, selectedAccountId]);

  // Reset form when dialog opens
  useEffect(() => {
    if (open) {
      setAmount("");
      setUserRemark("");
      setValidationError(null);
      if (accounts.length > 0) {
        const defaultAccount = accounts.find(a => a.isDefault) || accounts[0];
        setSelectedAccountId(defaultAccount.id.toString());
      }
    }
  }, [open, accounts]);

  const validateAmount = (value: string): string | null => {
    if (!value) {
      return t("wallet.wallet.withdrawAmountRequired");
    }

    const numValue = parseFloat(value);
    if (isNaN(numValue) || numValue <= 0) {
      return t("wallet.wallet.withdrawAmountInvalid");
    }

    const amountInCents = Math.round(numValue * 100);

    // Minimum withdraw amount: $10
    if (amountInCents < 1000) {
      return t("wallet.wallet.withdrawMinAmount", { amount: "$10.00" });
    }

    // Maximum withdraw amount: available balance
    if (amountInCents > availableBalance) {
      return t("wallet.wallet.withdrawInsufficientBalance");
    }

    return null;
  };

  const handleAmountChange = (value: string) => {
    // Allow empty string
    if (value === "") {
      setAmount("");
      setValidationError(null);
      return;
    }

    // Only allow valid number format
    const regex = /^\d*\.?\d{0,2}$/;
    if (regex.test(value)) {
      setAmount(value);
      const error = validateAmount(value);
      setValidationError(error);
    }
  };

  const handleSubmit = async () => {
    // Validate amount
    const error = validateAmount(amount);
    if (error) {
      setValidationError(error);
      return;
    }

    if (!selectedAccountId) {
      toast.error(t("wallet.wallet.withdrawAccountRequired"));
      return;
    }

    setIsSubmitting(true);
    try {
      const amountInCents = Math.round(parseFloat(amount) * 100);

      await api.createWithdrawRequest({
        amount: amountInCents,
        withdrawAccountId: parseInt(selectedAccountId),
        userRemark: userRemark.trim() || undefined
      });

      toast.success(t("wallet.wallet.withdrawSubmitSuccess"));
      onSuccess();
      onClose();
    } catch (error) {
      console.error("Failed to create withdraw request:", error);
      toast.error(t("wallet.wallet.withdrawSubmitFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  const formatAmount = (cents: number) => {
    return `$${(cents / 100).toFixed(2)}`;
  };

  // 获取渠道显示名称
  const getChannelDisplayName = (accountType: string, currency: string) => {
    if (accountType === "paypal") {
      return "PayPal (USD)";
    }
    const networkNames: Record<string, string> = {
      'tron': 'TRON (TRC20)',
      'polygon': 'Polygon',
      'bsc': 'BSC (BEP20)',
      'arbitrum': 'Arbitrum',
    };
    const networkName = networkNames[accountType.toLowerCase()] || accountType.toUpperCase();
    return `${networkName} - ${currency.toUpperCase()}`;
  };

  // 格式化账户标识（钱包地址截断，邮箱保持原样）
  const formatAccountId = (accountId: string, accountType: string) => {
    if (accountType === "paypal") {
      return accountId; // PayPal 邮箱不截断
    }
    if (accountId.length <= 16) return accountId;
    return `${accountId.slice(0, 8)}...${accountId.slice(-6)}`;
  };

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("wallet.wallet.withdraw")}</DialogTitle>
          <DialogDescription>
            {t("wallet.wallet.withdrawDescription")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {/* Available Balance */}
          <div className="bg-gray-50 dark:bg-gray-900 p-3 rounded-lg">
            <div className="text-sm text-gray-600 dark:text-gray-400">
              {t("wallet.wallet.availableBalance")}
            </div>
            <div className="text-xl font-bold text-gray-900 dark:text-gray-100">
              {formatAmount(availableBalance)}
            </div>
          </div>

          {/* Withdraw Account Selection */}
          <div className="space-y-2">
            <Label htmlFor="account">{t("wallet.wallet.withdrawAccount")}</Label>
            {accounts.length > 0 ? (
              <Select
                value={selectedAccountId}
                onValueChange={setSelectedAccountId}
                disabled={isSubmitting}
              >
                <SelectTrigger id="account">
                  <SelectValue placeholder={t("wallet.wallet.selectWithdrawAccount")} />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map(account => (
                    <SelectItem key={account.id} value={account.id.toString()}>
                      <div className="flex flex-col">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">
                            {account.label || formatAccountId(account.accountId, account.accountType)}
                          </span>
                          {account.isDefault && (
                            <span className="text-xs bg-blue-100 dark:bg-blue-900 text-blue-600 dark:text-blue-300 px-1.5 py-0.5 rounded">
                              {t("wallet.wallet.defaultAccount")}
                            </span>
                          )}
                        </div>
                        <div className="text-xs text-gray-500">
                          {getChannelDisplayName(account.accountType, account.currency)} {"•"} {formatAccountId(account.accountId, account.accountType)}
                        </div>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <div className="text-center py-4">
                <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
                  {t("wallet.wallet.noWithdrawAccounts")}
                </p>
                <Button
                  variant="outline"
                  onClick={() => {
                    onClose();
                    onOpenAccountManager();
                  }}
                  disabled={isSubmitting}
                >
                  <Plus className="h-4 w-4 mr-2" />
                  {t("wallet.wallet.addWithdrawAccount")}
                </Button>
              </div>
            )}
          </div>

          {accounts.length > 0 && (
            <>
              {/* Amount Input */}
              <div className="space-y-2">
                <Label htmlFor="amount">{t("wallet.wallet.withdrawAmount")}</Label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">{"$"}</span>
                  <Input
                    id="amount"
                    type="text"
                    placeholder="0.00"
                    value={amount}
                    onChange={(e) => handleAmountChange(e.target.value)}
                    className={`pl-8 ${validationError ? 'border-red-500' : ''}`}
                    disabled={isSubmitting}
                  />
                </div>
                {validationError && (
                  <p className="text-sm text-red-500">{validationError}</p>
                )}
                <p className="text-xs text-gray-500">
                  {t("wallet.wallet.withdrawMinAmount", { amount: "$10.00" })}
                </p>
              </div>

              {/* User Remark */}
              <div className="space-y-2">
                <Label htmlFor="remark">{t("wallet.wallet.withdrawRemark")} {"("}{t("common.common.optional")}{")"}</Label>
                <Textarea
                  id="remark"
                  placeholder={t("wallet.wallet.withdrawRemarkPlaceholder")}
                  value={userRemark}
                  onChange={(e) => setUserRemark(e.target.value)}
                  rows={3}
                  disabled={isSubmitting}
                />
              </div>

              {/* Fee Notice */}
              <div className="flex items-start gap-2 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                <AlertCircle className="h-4 w-4 mt-0.5 text-blue-600 dark:text-blue-400" />
                <p className="text-sm text-blue-900 dark:text-blue-100">
                  {t("wallet.wallet.withdrawFeeNotice")}
                </p>
              </div>
            </>
          )}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
            disabled={isSubmitting}
          >
            {t("common.common.cancel")}
          </Button>
          {accounts.length > 0 && (
            <Button
              onClick={handleSubmit}
              disabled={isSubmitting || !!validationError || !amount || !selectedAccountId}
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t("common.common.submitting")}
                </>
              ) : (
                t("wallet.wallet.confirmWithdraw")
              )}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
