"use client";

import { useState, useEffect, useCallback } from "react";
import { useTranslations } from "next-intl";
import { api } from "@/lib/api";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  ArrowLeft,
  Plus,
  Star,
  Copy,
  ExternalLink,
  Trash2,
  Loader2,
  RefreshCw,
  Shield,
  Wallet,
  Info,
  CreditCard,
  Bitcoin,
  Banknote
} from "lucide-react";
import { useRouter } from "@/i18n/routing";
import { toast } from "sonner";

// 提现账户类型（匹配后端）
interface WithdrawAccount {
  id: number;
  accountType: string;      // tron, polygon, bsc, arbitrum, paypal, alipay
  accountId: string;        // 钱包地址、PayPal 邮箱或支付宝脱敏账号（服务端已脱敏）
  currency: string;         // usdt, usdc, usd, cny
  label?: string;
  isDefault: boolean;
}

// 渠道类型配置
type ChannelType = 'alipay' | 'crypto' | 'paypal';

// 支付宝账号：大陆手机号或邮箱（与服务端 validateAlipayPayee 一致）
const alipayAccountPattern = /^(1\d{10}|[^\s@]+@[^\s@]+\.[^\s@]+)$/;

interface CryptoNetwork {
  value: string;
  label: string;
  icon: string;
  addressPattern: RegExp;
  addressExample: string;
}

const cryptoNetworks: CryptoNetwork[] = [
  {
    value: "tron",
    label: "TRON (TRC20)",
    icon: "🔵",
    addressPattern: /^T[a-zA-Z0-9]{33}$/,
    addressExample: "T..."
  },
  {
    value: "polygon",
    label: "Polygon",
    icon: "🟣",
    addressPattern: /^0x[a-fA-F0-9]{40}$/,
    addressExample: "0x..."
  },
  {
    value: "bsc",
    label: "BSC (BEP20)",
    icon: "🟡",
    addressPattern: /^0x[a-fA-F0-9]{40}$/,
    addressExample: "0x..."
  },
  {
    value: "arbitrum",
    label: "Arbitrum",
    icon: "🔷",
    addressPattern: /^0x[a-fA-F0-9]{40}$/,
    addressExample: "0x..."
  },
];

const currencies = [
  { value: "usdt", label: "USDT", description: "Tether USD - 最常用的稳定币" },
  { value: "usdc", label: "USDC", description: "USD Coin - Circle 发行的稳定币" },
];

export default function WalletAccountsPage() {
  const t = useTranslations();
  const router = useRouter();

  const [accounts, setAccounts] = useState<WithdrawAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  // 表单状态
  const [channelType, setChannelType] = useState<ChannelType>("alipay");
  const [network, setNetwork] = useState("tron");
  const [currency, setCurrency] = useState("cny");
  const [accountId, setAccountId] = useState("");
  const [realName, setRealName] = useState("");
  const [label, setLabel] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);

  const loadAccounts = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.getWithdrawAccounts() as WithdrawAccount[];
      if (data) {
        setAccounts(data);
      }
    } catch (error) {
      console.error("Failed to load accounts:", error);
      toast.error(t("wallet.wallet.loadAccountsFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  // 验证地址/邮箱格式
  const validateAccountId = (value: string, type: ChannelType, networkValue: string): string | null => {
    if (type === "alipay") {
      // 列表里的支付宝账号是脱敏值，无法在前端查重，重复由服务端拒绝
      if (!value.trim()) return t("wallet.wallet.alipayAccountRequired");
      return alipayAccountPattern.test(value.trim()) ? null : t("wallet.wallet.alipayAccountInvalid");
    }

    if (!value) {
      return type === "paypal" ? t("wallet.wallet.paypalEmailRequired") : t("wallet.wallet.addressRequired");
    }

    if (type === "paypal") {
      // PayPal 邮箱验证
      const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
      if (!emailRegex.test(value)) {
        return t("wallet.wallet.paypalEmailInvalid");
      }
    } else {
      // 加密货币地址验证
      const network = cryptoNetworks.find(n => n.value === networkValue);
      if (network && !network.addressPattern.test(value)) {
        return t("wallet.wallet.addressInvalid", { network: network.label });
      }
    }

    // 检查是否已存在
    const accountType = type === "paypal" ? "paypal" : networkValue;
    if (accounts.some(acc =>
      acc.accountId.toLowerCase() === value.toLowerCase() &&
      acc.accountType === accountType
    )) {
      return type === "paypal" ? t("wallet.wallet.paypalExists") : t("wallet.wallet.addressExists");
    }

    return null;
  };

  const handleAccountIdChange = (value: string) => {
    setAccountId(value);
    const error = validateAccountId(value, channelType, network);
    setValidationError(error);
  };

  const handleChannelTypeChange = (type: ChannelType) => {
    setChannelType(type);
    setAccountId("");
    setRealName("");
    setValidationError(null);
    if (type === "paypal") {
      setCurrency("usd");
    } else if (type === "alipay") {
      setCurrency("cny");
    } else {
      setCurrency("usdt");
    }
  };

  const handleNetworkChange = (value: string) => {
    setNetwork(value);
    if (accountId) {
      const error = validateAccountId(accountId, channelType, value);
      setValidationError(error);
    }
  };

  const resetForm = () => {
    setChannelType("alipay");
    setNetwork("tron");
    setCurrency("cny");
    setAccountId("");
    setRealName("");
    setLabel("");
    setValidationError(null);
  };

  const handleSubmit = async () => {
    const error = validateAccountId(accountId, channelType, network);
    if (error) {
      setValidationError(error);
      return;
    }
    if (channelType === "alipay" && !realName.trim()) {
      setValidationError(t("wallet.wallet.alipayRealNameRequired"));
      return;
    }

    setIsSubmitting(true);
    try {
      const accountType = channelType === "crypto" ? network : channelType;
      const currencyValue = channelType === "paypal" ? "usd" : channelType === "alipay" ? "cny" : currency;

      await api.createWithdrawAccount({
        accountType,
        accountId: accountId.trim(),
        currency: currencyValue,
        label: label.trim() || undefined,
        realName: channelType === "alipay" ? realName.trim() : undefined,
      });

      toast.success(t("wallet.wallet.accountAddSuccess"));
      resetForm();
      setShowAddDialog(false);
      loadAccounts();
    } catch (error) {
      console.error("Failed to add account:", error);
      toast.error(t("wallet.wallet.accountAddFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm(t("wallet.wallet.confirmDeleteAccount"))) {
      return;
    }

    setDeletingId(id);
    try {
      await api.deleteWithdrawAccount(id);
      toast.success(t("wallet.wallet.accountDeleteSuccess"));
      loadAccounts();
    } catch (error) {
      console.error("Failed to delete account:", error);
      toast.error(t("wallet.wallet.accountDeleteFailed"));
    } finally {
      setDeletingId(null);
    }
  };

  const handleSetDefault = async (id: number) => {
    try {
      await api.setDefaultWithdrawAccount(id);
      toast.success(t("wallet.wallet.accountSetDefaultSuccess"));
      loadAccounts();
    } catch (error) {
      console.error("Failed to set default account:", error);
      toast.error(t("wallet.wallet.accountSetDefaultFailed"));
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success(t("common.common.copiedToClipboard"));
  };

  const getExplorerUrl = (accountId: string, accountType: string): string | null => {
    const explorers: Record<string, string> = {
      'tron': `https://tronscan.org/#/address/${accountId}`,
      'polygon': `https://polygonscan.com/address/${accountId}`,
      'bsc': `https://bscscan.com/address/${accountId}`,
      'arbitrum': `https://arbiscan.io/address/${accountId}`,
    };
    return explorers[accountType] || null;
  };

  const formatAccountId = (accountId: string, accountType: string) => {
    if (accountType === "paypal" || accountType === "alipay") {
      return accountId; // 邮箱 / 支付宝脱敏账号不截断
    }
    if (accountId.length <= 16) return accountId;
    return `${accountId.slice(0, 8)}...${accountId.slice(-6)}`;
  };

  const getAccountTypeDisplay = (account: WithdrawAccount) => {
    if (account.accountType === "paypal") {
      return { icon: "💳", label: "PayPal", sublabel: "USD" };
    }
    if (account.accountType === "alipay") {
      return { icon: "💴", label: t("wallet.wallet.alipay"), sublabel: "CNY" };
    }
    const network = cryptoNetworks.find(n => n.value === account.accountType);
    const currencyLabel = account.currency?.toUpperCase() || "USDT";
    return {
      icon: network?.icon || "🔗",
      label: network?.label || account.accountType,
      sublabel: currencyLabel
    };
  };

  return (
    <div className="container max-w-6xl mx-auto py-6 space-y-6">
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
            <h1 className="text-2xl font-bold">{t("wallet.wallet.manageAccounts")}</h1>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={loadAccounts}>
              <RefreshCw className="h-4 w-4 mr-2" />
              {t("common.common.refresh")}
            </Button>
            <Button onClick={() => setShowAddDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
              {t("wallet.wallet.addWithdrawAccount")}
            </Button>
          </div>
        </div>

        {/* Info Card */}
        <Card className="p-4 bg-blue-50 dark:bg-blue-950/20 border-blue-200 dark:border-blue-800">
          <div className="flex items-start gap-3">
            <Shield className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
            <div className="text-sm space-y-1">
              <p className="font-medium text-blue-900 dark:text-blue-100">
                {t("wallet.wallet.accountSecurityNotice")}
              </p>
              <p className="text-blue-800 dark:text-blue-200">
                {t("wallet.wallet.accountSecurityDescription")}
              </p>
            </div>
          </div>
        </Card>
      </div>

      {/* Accounts Table */}
      {loading ? (
        <Card className="p-12">
          <div className="flex justify-center">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        </Card>
      ) : accounts.length === 0 ? (
        <Card className="p-12">
          <div className="text-center space-y-4">
            <Wallet className="h-12 w-12 mx-auto text-muted-foreground" />
            <h3 className="text-lg font-medium">{t("wallet.wallet.noWithdrawAccounts")}</h3>
            <p className="text-sm text-muted-foreground max-w-md mx-auto">
              {t("wallet.wallet.noAccountsDescription")}
            </p>
            <Button onClick={() => setShowAddDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
              {t("wallet.wallet.addFirstAccount")}
            </Button>
          </div>
        </Card>
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("wallet.wallet.accountLabel")}</TableHead>
                <TableHead>{t("wallet.wallet.channelType")}</TableHead>
                <TableHead>{t("wallet.wallet.accountIdentifier")}</TableHead>
                <TableHead>{t("common.common.status")}</TableHead>
                <TableHead className="text-right">{t("common.common.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accounts.map(account => {
                const display = getAccountTypeDisplay(account);
                const explorerUrl = getExplorerUrl(account.accountId, account.accountType);

                return (
                  <TableRow key={account.id}>
                    <TableCell>
                      <div className="font-medium">
                        {account.label || t("wallet.wallet.unnamedAccount")}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="text-lg">{display.icon}</span>
                        <div>
                          <span className="block">{display.label}</span>
                          <span className="text-xs text-muted-foreground">{display.sublabel}</span>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <code className="text-sm bg-muted px-2 py-1 rounded font-mono">
                          {formatAccountId(account.accountId, account.accountType)}
                        </code>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0"
                          onClick={() => copyToClipboard(account.accountId)}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                        {explorerUrl && (
                          <a
                            href={explorerUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-7 w-7 p-0"
                            >
                              <ExternalLink className="h-3 w-3" />
                            </Button>
                          </a>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {account.isDefault ? (
                        <Badge className="bg-green-500 hover:bg-green-600 text-white">
                          <Star className="h-3 w-3 mr-1 fill-current" />
                          {t("wallet.wallet.defaultAccount")}
                        </Badge>
                      ) : (
                        <Badge variant="secondary">
                          {t("wallet.wallet.standardAccount")}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!account.isDefault && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleSetDefault(account.id)}
                          >
                            <Star className="h-3 w-3 mr-1" />
                            {t("wallet.wallet.setAsDefault")}
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(account.id)}
                          disabled={deletingId === account.id || account.isDefault}
                        >
                          {deletingId === account.id ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4 text-destructive" />
                          )}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Card>
      )}

      {/* Add Account Dialog */}
      <Dialog open={showAddDialog} onOpenChange={(open) => {
        setShowAddDialog(open);
        if (!open) resetForm();
      }}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("wallet.wallet.addWithdrawAccount")}</DialogTitle>
            <DialogDescription>
              {t("wallet.wallet.addAccountDescription")}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-6 py-4">
            {/* Channel Type Selection */}
            <div className="space-y-3">
              <Label>{t("wallet.wallet.selectChannelType")}</Label>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Card
                  className={`p-4 cursor-pointer transition-all ${
                    channelType === "alipay"
                      ? "border-primary bg-primary/5"
                      : "hover:border-muted-foreground/50"
                  }`}
                  onClick={() => handleChannelTypeChange("alipay")}
                >
                  <div className="flex items-center gap-3">
                    <Banknote className="h-6 w-6 text-sky-600" />
                    <div>
                      <div className="font-medium">{t("wallet.wallet.alipay")}</div>
                      <div className="text-xs text-muted-foreground">
                        {t("wallet.wallet.alipayDescription")}
                      </div>
                    </div>
                  </div>
                </Card>
                <Card
                  className={`p-4 cursor-pointer transition-all ${
                    channelType === "crypto"
                      ? "border-primary bg-primary/5"
                      : "hover:border-muted-foreground/50"
                  }`}
                  onClick={() => handleChannelTypeChange("crypto")}
                >
                  <div className="flex items-center gap-3">
                    <Bitcoin className="h-6 w-6 text-orange-500" />
                    <div>
                      <div className="font-medium">{t("wallet.wallet.cryptoWallet")}</div>
                      <div className="text-xs text-muted-foreground">
                        {t("wallet.wallet.cryptoWalletDescription")}
                      </div>
                    </div>
                  </div>
                </Card>
                <Card
                  className={`p-4 cursor-pointer transition-all ${
                    channelType === "paypal"
                      ? "border-primary bg-primary/5"
                      : "hover:border-muted-foreground/50"
                  }`}
                  onClick={() => handleChannelTypeChange("paypal")}
                >
                  <div className="flex items-center gap-3">
                    <CreditCard className="h-6 w-6 text-blue-600" />
                    <div>
                      <div className="font-medium">{"PayPal"}</div>
                      <div className="text-xs text-muted-foreground">
                        {t("wallet.wallet.paypalDescription")}
                      </div>
                    </div>
                  </div>
                </Card>
              </div>
            </div>

            {/* Crypto Options */}
            {channelType === "crypto" && (
              <>
                {/* Network Selection */}
                <div className="space-y-2">
                  <Label htmlFor="network">{t("wallet.wallet.network")}</Label>
                  <Select
                    value={network}
                    onValueChange={handleNetworkChange}
                    disabled={isSubmitting}
                  >
                    <SelectTrigger id="network">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {cryptoNetworks.map(net => (
                        <SelectItem key={net.value} value={net.value}>
                          <span className="flex items-center gap-2">
                            <span>{net.icon}</span>
                            <span>{net.label}</span>
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {/* Currency Selection with Guide */}
                <div className="space-y-2">
                  <Label htmlFor="currency">{t("wallet.wallet.currency")}</Label>
                  <Select
                    value={currency}
                    onValueChange={setCurrency}
                    disabled={isSubmitting}
                  >
                    <SelectTrigger id="currency">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {currencies.map(cur => (
                        <SelectItem key={cur.value} value={cur.value}>
                          <span className="flex items-center gap-2">
                            <span className="font-medium">{cur.label}</span>
                            <span className="text-muted-foreground text-xs">{`- ${cur.description}`}</span>
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="flex items-start gap-2 p-2 bg-amber-50 dark:bg-amber-950/20 rounded text-xs text-amber-800 dark:text-amber-200">
                    <Info className="h-4 w-4 mt-0.5 flex-shrink-0" />
                    <span>{t("wallet.wallet.currencyGuide")}</span>
                  </div>
                </div>

                {/* Wallet Address */}
                <div className="space-y-2">
                  <Label htmlFor="address">{t("wallet.wallet.walletAddress")}</Label>
                  <Input
                    id="address"
                    type="text"
                    placeholder={cryptoNetworks.find(n => n.value === network)?.addressExample}
                    value={accountId}
                    onChange={(e) => handleAccountIdChange(e.target.value)}
                    className={validationError ? 'border-red-500' : ''}
                    disabled={isSubmitting}
                  />
                  {validationError && (
                    <p className="text-sm text-red-500">{validationError}</p>
                  )}
                </div>
              </>
            )}

            {/* Alipay Options */}
            {channelType === "alipay" && (
              <>
                <div className="space-y-2">
                  <Label htmlFor="alipay-account">{t("wallet.wallet.alipayAccount")}</Label>
                  <Input
                    id="alipay-account"
                    type="text"
                    autoComplete="off"
                    placeholder={t("wallet.wallet.alipayAccountPlaceholder")}
                    value={accountId}
                    onChange={(e) => handleAccountIdChange(e.target.value)}
                    className={validationError ? 'border-red-500' : ''}
                    disabled={isSubmitting}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="alipay-name">{t("wallet.wallet.alipayRealName")}</Label>
                  <Input
                    id="alipay-name"
                    type="text"
                    autoComplete="off"
                    placeholder={t("wallet.wallet.alipayRealNamePlaceholder")}
                    value={realName}
                    onChange={(e) => {
                      setRealName(e.target.value);
                      if (e.target.value.trim()) {
                        setValidationError(validateAccountId(accountId, channelType, network));
                      }
                    }}
                    disabled={isSubmitting}
                  />
                  {validationError && (
                    <p className="text-sm text-red-500">{validationError}</p>
                  )}
                </div>
                <div className="flex items-start gap-2 p-2 bg-blue-50 dark:bg-blue-950/20 rounded text-xs text-blue-800 dark:text-blue-200">
                  <Info className="h-4 w-4 mt-0.5 flex-shrink-0" />
                  <span>{t("wallet.wallet.alipayGuide")}</span>
                </div>
              </>
            )}

            {/* PayPal Options */}
            {channelType === "paypal" && (
              <div className="space-y-2">
                <Label htmlFor="paypal-email">{t("wallet.wallet.paypalEmail")}</Label>
                <Input
                  id="paypal-email"
                  type="email"
                  placeholder="your@email.com"
                  value={accountId}
                  onChange={(e) => handleAccountIdChange(e.target.value)}
                  className={validationError ? 'border-red-500' : ''}
                  disabled={isSubmitting}
                />
                {validationError && (
                  <p className="text-sm text-red-500">{validationError}</p>
                )}
                <div className="flex items-start gap-2 p-2 bg-blue-50 dark:bg-blue-950/20 rounded text-xs text-blue-800 dark:text-blue-200">
                  <Info className="h-4 w-4 mt-0.5 flex-shrink-0" />
                  <span>{t("wallet.wallet.paypalGuide")}</span>
                </div>
              </div>
            )}

            {/* Label (Optional) */}
            <div className="space-y-2">
              <Label htmlFor="label">
                {t("wallet.wallet.accountLabel")}
                <span className="text-muted-foreground ml-1">{"("}{t("common.common.optional")}{")"}</span>
              </Label>
              <Input
                id="label"
                type="text"
                placeholder={t("wallet.wallet.accountLabelPlaceholder")}
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                disabled={isSubmitting}
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setShowAddDialog(false);
                resetForm();
              }}
              disabled={isSubmitting}
            >
              {t("common.common.cancel")}
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={isSubmitting || !!validationError || !accountId || (channelType === "alipay" && !realName.trim())}
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t("common.common.adding")}
                </>
              ) : (
                t("common.common.add")
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
