"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, Suspense, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { api, ContactInfo, ContactType, getContactUrl, getContactTypeName, AdminDeviceData, IssueDeviceTokenResponse } from "@/lib/api";
import { format } from "date-fns";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Pencil } from "lucide-react";
import { EditEmailDialog } from "./components/EditEmailDialog";
import { MoreActionsMenu } from "./components/MoreActionsMenu";

// 分销商等级颜色映射
const levelColors: Record<number, string> = {
  1: '#9E9E9E',  // L1 灰色
  2: '#2196F3',  // L2 蓝色
  3: '#9C27B0',  // L3 紫色
  4: '#FF9800',  // L4 金色
};


// 完全匹配后端 AdminUserDetailData 结构
interface DataLoginIdentify {
  type: string;
  value: string;
}

interface DataDevice {
  udid: string;
  remark: string;
  tokenLastUsedAt: number; // 注意：后端是 TokenLastUsedAt，不是 token_last_used_at
}

interface DataOrder {
  uuid: string;
  title: string;
  payAmount: number; // 注意：后端是 PayAmount，不是 pay_amount
  isPaid: boolean; // 注意：后端是 IsPaid，不是 is_paid
  createdAt: number; // 注意：后端是 CreatedAt，不是 created_at
  payAt: number; // 注意：后端是 PayAt，不是 pay_at
}

interface DataProHistory {
  type: string;
  days: number;
  reason: string;
  createdAt: number; // 注意：后端是 CreatedAt，不是 created_at
  order?: DataOrder | null;
}

interface DataMyInviteCode {
  code: string;
  link: string;
  registerCount: number; // 注册人数（仅统计，无奖励）
  purchaseCount: number; // 购买人数
  purchaseReward: number; // 购买奖励（天数）
}

interface DataRetailerConfig {
  level: number;
  levelName: string;
  firstOrderPercent: number;
  renewalPercent: number;
  paidUserCount: number;
  nextLevel?: number;
  nextLevelName?: string;
  nextLevelRequirement?: number;
  needContentProof: boolean;
  progressPercent: number;
  contentProof?: string;
  contentVerifiedAt?: number;
  contacts?: ContactInfo[];
}

interface DataWallet {
  balance: number;
  availableBalance: number;
  frozenBalance: number;
  totalIncome: number;
  totalWithdrawn: number;
}

interface DataWalletChange {
  id: number;
  type: string;
  amount: number;
  balanceAfter: number;
  description: string;
  frozenUntil?: number;
  createdAt: number;
}

interface UserDetailData {
  uuid: string;
  expiredAt: number;
  isFirstOrderDone: boolean;
  deviceCount: number;
  isRetailer: boolean;
  loginIdentifies: DataLoginIdentify[];
  devices: DataDevice[];
  orders: DataOrder[];
  proHistories: DataProHistory[];
  inviteCodes: DataMyInviteCode[];
  retailerConfig?: DataRetailerConfig;
  wallet?: DataWallet;
  walletChanges?: DataWalletChange[];
}

// 安全的日期格式化函数
const safeFormatDate = (
  timestamp: number,
  formatStr: string = "yyyy-MM-dd HH:mm"
): string => {
  if (!timestamp || timestamp <= 0) {
    return "-";
  }
  try {
    const date = new Date(timestamp * 1000);
    if (isNaN(date.getTime())) {
      return "-";
    }
    return format(date, formatStr);
  } catch (error) {
    console.warn("Failed to format date:", timestamp, error);
    return "-";
  }
};


function UserDetailContent() {
  const searchParams = useSearchParams();
  const uuid = searchParams.get("uuid");

  const [userDetail, setUserDetail] = useState<UserDetailData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isUpdatingRetailer, setIsUpdatingRetailer] = useState(false);


  // Contact editing state
  const [isEditingContacts, setIsEditingContacts] = useState(false);
  const [editContacts, setEditContacts] = useState<ContactInfo[]>([]);
  const [isSavingContacts, setIsSavingContacts] = useState(false);

  // Membership editing state
  const [isAddingMembership, setIsAddingMembership] = useState(false);
  const [membershipMonths, setMembershipMonths] = useState("1");
  const [membershipReason, setMembershipReason] = useState("");
  const [isSavingMembership, setIsSavingMembership] = useState(false);

  // Device management state
  const [devices, setDevices] = useState<AdminDeviceData[]>([]);
  const [isLoadingDevices, setIsLoadingDevices] = useState(false);
  const [tokenDialog, setTokenDialog] = useState<{
    show: boolean;
    udid?: string;
    tokenResponse?: IssueDeviceTokenResponse;
  }>({ show: false });

  // Email editing state
  const [isEditingEmail, setIsEditingEmail] = useState(false);

  // Fetch devices
  useEffect(() => {
    const fetchDevices = async () => {
      if (!uuid) return;
      setIsLoadingDevices(true);
      try {
        const result = await api.getUserDevices(uuid);
        setDevices(result.items || []);
      } catch (error) {
        console.error('Failed to fetch devices:', error);
        toast.error('加载设备列表失败');
      } finally {
        setIsLoadingDevices(false);
      }
    };
    if (uuid) {
      fetchDevices();
    }
  }, [uuid]);

  // 提取 fetchUserDetail 为可复用的函数
  const fetchUserDetail = useCallback(async () => {
    if (!uuid) {
      setError("未提供用户UUID");
      setIsLoading(false);
      return;
    }
    setIsLoading(true);
    setError(null);

    try {
      const data = await api.request<UserDetailData>(`/app/users/${uuid}`);

      // 确保所有字段都有默认值
      setUserDetail({
        ...data,
        isRetailer: data.isRetailer || false,
        loginIdentifies: data.loginIdentifies || [],
        devices: data.devices || [],
        orders: data.orders || [],
        proHistories: data.proHistories || [],
        inviteCodes: data.inviteCodes || [],
      });
    } catch (error) {
      console.error("Failed to fetch user details", error);
      setError("加载用户详情失败");
    } finally {
      setIsLoading(false);
    }
  }, [uuid]);

  useEffect(() => {
    fetchUserDetail();
  }, [fetchUserDetail]);

  // 更新分销商状态
  const handleRetailerStatusChange = async (newStatus: boolean) => {
    if (!uuid || !userDetail) return;

    setIsUpdatingRetailer(true);
    try {
      await api.request(`/app/users/${uuid}/retailer-status`, {
        method: 'PUT',
        body: JSON.stringify({ isRetailer: newStatus }),
      });

      // 更新本地状态
      setUserDetail(prev => prev ? { ...prev, isRetailer: newStatus } : null);

      toast.success(`用户分销商状态已${newStatus ? '开启' : '关闭'}`);
    } catch (error) {
      console.error('Failed to update retailer status:', error);
      toast.error('更新分销商状态失败');
    } finally {
      setIsUpdatingRetailer(false);
    }
  };

  // 开始编辑联系方式
  const handleStartEditContacts = () => {
    setEditContacts(userDetail?.retailerConfig?.contacts || []);
    setIsEditingContacts(true);
  };

  // 添加联系方式
  const handleAddContact = () => {
    setEditContacts([...editContacts, { type: 'telegram', value: '' }]);
  };

  // 删除联系方式
  const handleRemoveContact = (index: number) => {
    setEditContacts(editContacts.filter((_, i) => i !== index));
  };

  // 更新联系方式
  const handleUpdateContact = (index: number, field: keyof ContactInfo, value: string) => {
    const newContacts = [...editContacts];
    newContacts[index] = { ...newContacts[index], [field]: value };
    setEditContacts(newContacts);
  };

  // 保存联系方式
  const handleSaveContacts = async () => {
    if (!uuid) return;

    // 过滤掉空值
    const validContacts = editContacts.filter(c => c.value.trim() !== '');

    setIsSavingContacts(true);
    try {
      await api.updateRetailerContacts(uuid, { contacts: validContacts });

      // 重新获取用户数据
      const data = await api.request<UserDetailData>(`/app/users/${uuid}`);
      setUserDetail({
        ...data,
        isRetailer: data.isRetailer || false,
        loginIdentifies: data.loginIdentifies || [],
        devices: data.devices || [],
        orders: data.orders || [],
        proHistories: data.proHistories || [],
        inviteCodes: data.inviteCodes || [],
      });

      setIsEditingContacts(false);
      toast.success('联系方式已更新');
    } catch (error) {
      console.error('Failed to update contacts:', error);
      toast.error('更新联系方式失败');
    } finally {
      setIsSavingContacts(false);
    }
  };

  // 打开添加会员时长对话框
  const handleOpenAddMembership = () => {
    setMembershipMonths("1");
    setMembershipReason("");
    setIsAddingMembership(true);
  };

  // 保存会员时长
  const handleSaveMembership = async () => {
    if (!uuid) return;

    const months = parseInt(membershipMonths, 10);
    if (isNaN(months) || months < 1 || months > 120) {
      toast.error('请选择有效的月数');
      return;
    }

    setIsSavingMembership(true);
    try {
      await api.addUserMembership(uuid, {
        months,
        reason: membershipReason || undefined,
      });

      // 重新获取用户数据
      const data = await api.request<UserDetailData>(`/app/users/${uuid}`);
      setUserDetail({
        ...data,
        isRetailer: data.isRetailer || false,
        loginIdentifies: data.loginIdentifies || [],
        devices: data.devices || [],
        orders: data.orders || [],
        proHistories: data.proHistories || [],
        inviteCodes: data.inviteCodes || [],
      });

      setIsAddingMembership(false);
      toast.success(`成功添加 ${months} 个月会员时长`);
    } catch (error) {
      console.error('Failed to add membership:', error);
      toast.error('添加会员时长失败');
    } finally {
      setIsSavingMembership(false);
    }
  };

  // 签发设备 Token
  const handleIssueTestToken = async (userUUID: string, udid: string) => {
    if (!userUUID || !udid) return;
    try {
      const resp = await api.issueTestToken(userUUID, udid);
      setTokenDialog({
        show: true,
        udid: udid,
        tokenResponse: resp,
      });
      // Refresh device list to show updated token issue time
      const result = await api.getUserDevices(userUUID);
      setDevices(result.items || []);
      toast.success('Token 签发成功');
    } catch (error) {
      console.error('Failed to issue token:', error);
      toast.error('签发 Token 失败');
    }
  };

  // 复制到剪贴板
  const handleCopyToClipboard = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`${label} 已复制到剪贴板`);
    } catch (error) {
      console.error('Failed to copy:', error);
      toast.error('复制失败');
    }
  };

  // 加载状态
  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
      </div>
    );
  }

  // 错误状态
  if (error) {
    return (
      <div className="container mx-auto p-8">
        <div className="text-center">
          <h1 className="text-2xl font-bold text-red-600 mb-4">{"错误"}</h1>
          <p className="text-gray-600">{error}</p>
        </div>
      </div>
    );
  }

  // 数据为空状态
  if (!userDetail) {
    return (
      <div className="container mx-auto p-8">
        <div className="text-center">
          <h1 className="text-2xl font-bold mb-4">{"用户不存在"}</h1>
          <p className="text-gray-600">{"未找到该用户的详细信息"}</p>
        </div>
      </div>
    );
  }

  const email =
    userDetail.loginIdentifies?.find((id) => id.type === "email")?.value ||
    "N/A";

  return (
    <div className="container mx-auto p-8 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">{"用户详情"}</h1>
        <MoreActionsMenu userUUID={userDetail.uuid} userEmail={email} />
      </div>

      {/* 基本信息 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-4">
            <span>{email}</span>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              onClick={() => setIsEditingEmail(true)}
            >
              <Pencil className="h-3 w-3" />
              <span className="sr-only">{"修改邮箱"}</span>
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div>
              <p className="text-sm text-muted-foreground">{"用户UUID"}</p>
              <p className="font-mono text-sm">{userDetail.uuid}</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{"会员到期"}</p>
              <div className="flex items-center gap-2">
                <p>
                  {userDetail.expiredAt > 0
                    ? safeFormatDate(userDetail.expiredAt)
                    : "未开通"}
                </p>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleOpenAddMembership}
                >
                  {"添加时长"}
                </Button>
              </div>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{"是否付费"}</p>
              <p>{userDetail.isFirstOrderDone ? "是" : "否"}</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{"设备数量"}</p>
              <p>{userDetail.deviceCount || 0}</p>
            </div>
          </div>

          {/* 分销商状态管理 */}
          <div className="border-t pt-4">
            <div className="flex items-center justify-between">
              <div className="space-y-1">
                <p className="text-sm font-medium">{"分销商权限"}</p>
                <p className="text-sm text-muted-foreground">
                  {userDetail.isRetailer
                    ? "该用户拥有分销商权限，可以访问分销商管理页面"
                    : "该用户没有分销商权限，无法访问分销商管理页面"}
                </p>
              </div>
              <div className="flex items-center space-x-3">
                <Badge variant={userDetail.isRetailer ? "default" : "secondary"}>
                  {userDetail.isRetailer ? "分销商" : "普通用户"}
                </Badge>
                <Switch
                  checked={userDetail.isRetailer}
                  onCheckedChange={handleRetailerStatusChange}
                  disabled={isUpdatingRetailer}
                />
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 分销商配置 */}
      {userDetail.isRetailer && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>{"分销商配置"}</CardTitle>
              <Link href={`/manager/retailers/${uuid}`}>
                <Button variant="outline" size="sm">
                  {"查看分销商详情"}
                </Button>
              </Link>
            </div>
          </CardHeader>
          <CardContent>
            {userDetail.retailerConfig ? (
              <div className="space-y-4">
                {/* 当前等级显示 */}
                <div className="flex items-center gap-3">
                  <span
                    className="inline-flex items-center px-2.5 py-1 rounded text-sm font-semibold text-white"
                    style={{ backgroundColor: levelColors[userDetail.retailerConfig.level] || levelColors[1] }}
                  >
                    {"L"}{userDetail.retailerConfig.level}
                  </span>
                  <span className="text-lg font-medium">{userDetail.retailerConfig.levelName}</span>
                </div>

                {/* 分成比例 */}
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                  <div>
                    <p className="text-sm text-muted-foreground">{"首单分成"}</p>
                    <p className="text-lg font-medium">{userDetail.retailerConfig.firstOrderPercent}{"%"}</p>
                  </div>
                  <div>
                    <p className="text-sm text-muted-foreground">{"续费分成"}</p>
                    <p className="text-lg font-medium">{userDetail.retailerConfig.renewalPercent}{"%"}</p>
                  </div>
                  <div>
                    <p className="text-sm text-muted-foreground">{"累计付费用户"}</p>
                    <p className="text-lg font-medium">{userDetail.retailerConfig.paidUserCount}</p>
                  </div>
                  {userDetail.retailerConfig.nextLevel && (
                    <div>
                      <p className="text-sm text-muted-foreground">{"升级进度"}</p>
                      <p className="text-lg font-medium">{userDetail.retailerConfig.progressPercent}{"%"}</p>
                    </div>
                  )}
                </div>

                {/* 升级信息 */}
                {userDetail.retailerConfig.nextLevel && (
                  <div className="border-t pt-4">
                    <p className="text-sm text-muted-foreground mb-2">{"下一等级"}</p>
                    <div className="flex items-center gap-2">
                      <span
                        className="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold text-white"
                        style={{ backgroundColor: levelColors[userDetail.retailerConfig.nextLevel] || levelColors[1] }}
                      >
                        {"L"}{userDetail.retailerConfig.nextLevel}
                      </span>
                      <span>{userDetail.retailerConfig.nextLevelName}</span>
                      {userDetail.retailerConfig.nextLevelRequirement && (
                        <span className="text-sm text-muted-foreground">
                          {"(需要 "}{userDetail.retailerConfig.nextLevelRequirement}{" 位付费用户)"}
                        </span>
                      )}
                    </div>
                    {userDetail.retailerConfig.needContentProof && (
                      <p className="text-sm text-orange-600 mt-2">
                        {"⚠️ 升级到下一等级需要提交内容证明（社交媒体推广证据）"}
                      </p>
                    )}

                    {/* 升级进度条 */}
                    <div className="mt-3">
                      <div className="w-full bg-gray-200 rounded-full h-2.5">
                        <div
                          className="h-2.5 rounded-full transition-all duration-300"
                          style={{
                            width: `${userDetail.retailerConfig.progressPercent}%`,
                            backgroundColor: levelColors[userDetail.retailerConfig.nextLevel] || levelColors[1]
                          }}
                        ></div>
                      </div>
                      <p className="text-xs text-muted-foreground mt-1">
                        {userDetail.retailerConfig.paidUserCount}{" / "}{userDetail.retailerConfig.nextLevelRequirement}{" 用户"}
                      </p>
                    </div>
                  </div>
                )}

                {/* 内容证明 */}
                {userDetail.retailerConfig.contentProof && (
                  <div className="border-t pt-4">
                    <p className="text-sm text-muted-foreground mb-2">{"内容证明"}</p>
                    <p className="text-sm bg-gray-50 p-2 rounded">{userDetail.retailerConfig.contentProof}</p>
                    {userDetail.retailerConfig.contentVerifiedAt && (
                      <p className="text-xs text-green-600 mt-1">
                        {"✓ 已审核于 "}{safeFormatDate(userDetail.retailerConfig.contentVerifiedAt)}
                      </p>
                    )}
                  </div>
                )}

                {/* 联系方式 */}
                <div className="border-t pt-4">
                  <div className="flex items-center justify-between mb-3">
                    <p className="text-sm font-medium">{"联系方式"}</p>
                    {!isEditingContacts && (
                      <Button onClick={handleStartEditContacts} variant="outline" size="sm">
                        {"编辑联系方式"}
                      </Button>
                    )}
                  </div>

                  {isEditingContacts ? (
                    <div className="space-y-3">
                      {editContacts.map((contact, index) => (
                        <div key={index} className="flex gap-2 items-start">
                          <select
                            className="w-32 p-2 border rounded-md text-sm"
                            value={contact.type}
                            onChange={(e) => handleUpdateContact(index, 'type', e.target.value)}
                          >
                            <option value="telegram">{"Telegram"}</option>
                            <option value="email">{"Email"}</option>
                            <option value="signal">{"Signal"}</option>
                            <option value="whatsapp">{"WhatsApp"}</option>
                            <option value="wechat">{"微信"}</option>
                            <option value="line">{"Line"}</option>
                            <option value="other">{"其他"}</option>
                          </select>
                          <Input
                            className="flex-1"
                            placeholder={contact.type === 'wechat' ? '微信二维码链接' : contact.type === 'email' ? '邮箱地址' : '用户名或链接'}
                            value={contact.value}
                            onChange={(e) => handleUpdateContact(index, 'value', e.target.value)}
                          />
                          {contact.type === 'other' && (
                            <Input
                              className="w-24"
                              placeholder="标签"
                              value={contact.label || ''}
                              onChange={(e) => handleUpdateContact(index, 'label', e.target.value)}
                            />
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleRemoveContact(index)}
                            className="text-red-500 hover:text-red-700"
                          >
                            {"删除"}
                          </Button>
                        </div>
                      ))}
                      <div className="flex gap-2 pt-2">
                        <Button onClick={handleAddContact} variant="outline" size="sm">
                          {"+ 添加联系方式"}
                        </Button>
                        <Button onClick={handleSaveContacts} size="sm" disabled={isSavingContacts}>
                          {isSavingContacts ? "保存中..." : "保存"}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setIsEditingContacts(false)}>
                          {"取消"}
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <div className="space-y-2">
                      {userDetail.retailerConfig.contacts && userDetail.retailerConfig.contacts.length > 0 ? (
                        userDetail.retailerConfig.contacts.map((contact, index) => {
                          const url = getContactUrl(contact);
                          const typeName = contact.type === 'other' && contact.label
                            ? contact.label
                            : getContactTypeName(contact.type as ContactType);

                          return (
                            <div key={index} className="flex items-center gap-2">
                              <Badge variant="outline" className="min-w-[80px] justify-center">
                                {typeName}
                              </Badge>
                              {url ? (
                                <a
                                  href={url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-blue-600 hover:text-blue-800 hover:underline"
                                >
                                  {contact.value}
                                </a>
                              ) : (
                                <span className="text-gray-600">{contact.value}</span>
                              )}
                              {contact.type === 'wechat' && url && (
                                <span className="text-xs text-muted-foreground">{"(点击查看二维码)"}</span>
                              )}
                            </div>
                          );
                        })
                      ) : (
                        <p className="text-sm text-muted-foreground">{"暂无联系方式"}</p>
                      )}
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <p className="text-gray-500">{"该用户尚未配置分销商信息"}</p>
            )}
          </CardContent>
        </Card>
      )}

      {/* 钱包信息 */}
      {userDetail.wallet && (
        <Card>
          <CardHeader>
            <CardTitle>{"钱包信息"}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
              <div>
                <p className="text-sm text-muted-foreground">{"总余额"}</p>
                <p className="text-lg font-medium">
                  {"$"}{(userDetail.wallet.balance / 100).toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">{"可用余额"}</p>
                <p className="text-lg font-medium text-green-600">
                  {"$"}{(userDetail.wallet.availableBalance / 100).toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">{"冻结余额"}</p>
                <p className="text-lg font-medium text-orange-600">
                  {"$"}{(userDetail.wallet.frozenBalance / 100).toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">{"累计收入"}</p>
                <p className="text-lg">
                  {"$"}{(userDetail.wallet.totalIncome / 100).toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">{"累计提现"}</p>
                <p className="text-lg">
                  {"$"}{(userDetail.wallet.totalWithdrawn / 100).toFixed(2)}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 钱包变更记录 */}
      {userDetail.walletChanges && userDetail.walletChanges.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>{"钱包变更记录 ("}{userDetail.walletChanges.length}{")"}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{"类型"}</TableHead>
                  <TableHead>{"金额"}</TableHead>
                  <TableHead>{"变更后余额"}</TableHead>
                  <TableHead>{"说明"}</TableHead>
                  <TableHead>{"冻结至"}</TableHead>
                  <TableHead>{"时间"}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {userDetail.walletChanges.map((change) => (
                  <TableRow key={change.id}>
                    <TableCell>
                      <Badge variant={change.type === 'income' ? 'default' : 'secondary'}>
                        {change.type === 'income' ? '收入' : '支出'}
                      </Badge>
                    </TableCell>
                    <TableCell className={change.type === 'income' ? 'text-green-600' : 'text-red-600'}>
                      {change.type === 'income' ? '+' : '-'}{"$"}{Math.abs(change.amount / 100).toFixed(2)}
                    </TableCell>
                    <TableCell>
                      {"$"}{(change.balanceAfter / 100).toFixed(2)}
                    </TableCell>
                    <TableCell>{change.description || '-'}</TableCell>
                    <TableCell>
                      {change.frozenUntil ? safeFormatDate(change.frozenUntil) : '-'}
                    </TableCell>
                    <TableCell>{safeFormatDate(change.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {/* 设备列表 */}
      <Card>
        <CardHeader>
          <CardTitle>{"设备列表 ("}{devices.length}{")"}</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoadingDevices ? (
            <div className="flex items-center justify-center py-8">
              <div className="animate-spin rounded-full h-6 w-6 border-t-2 border-b-2 border-primary"></div>
            </div>
          ) : devices.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{"UDID"}</TableHead>
                  <TableHead>{"备注"}</TableHead>
                  <TableHead>{"App 版本"}</TableHead>
                  <TableHead>{"平台"}</TableHead>
                  <TableHead>{"架构"}</TableHead>
                  <TableHead>{"Token 签发"}</TableHead>
                  <TableHead>{"上次使用"}</TableHead>
                  <TableHead>{"操作"}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {devices.map((device, index) => (
                  <TableRow key={device.udid || index}>
                    <TableCell className="font-mono text-sm">
                      {device.udid || "-"}
                    </TableCell>
                    <TableCell>{device.remark || "-"}</TableCell>
                    <TableCell>{device.appVersion || "-"}</TableCell>
                    <TableCell>{device.appPlatform || "-"}</TableCell>
                    <TableCell>{device.appArch || "-"}</TableCell>
                    <TableCell>{safeFormatDate(device.tokenIssueAt)}</TableCell>
                    <TableCell>{safeFormatDate(device.tokenLastUsedAt)}</TableCell>
                    <TableCell>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleIssueTestToken(userDetail.uuid, device.udid)}
                      >
                        {"签发 Token"}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500 text-center py-4">{"暂无设备记录"}</p>
          )}
        </CardContent>
      </Card>

      {/* 订单历史 */}
      <Card>
        <CardHeader>
          <CardTitle>{"订单历史 ("}{userDetail.orders?.length || 0}{")"}</CardTitle>
        </CardHeader>
        <CardContent>
          {userDetail.orders && userDetail.orders.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{"订单号"}</TableHead>
                  <TableHead>{"标题"}</TableHead>
                  <TableHead>{"金额"}</TableHead>
                  <TableHead>{"状态"}</TableHead>
                  <TableHead>{"创建时间"}</TableHead>
                  <TableHead>{"支付时间"}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {userDetail.orders.map((order, index) => (
                  <TableRow key={order.uuid || index}>
                    <TableCell className="font-mono text-sm">
                      {order.uuid || "-"}
                    </TableCell>
                    <TableCell>{order.title || "-"}</TableCell>
                    <TableCell>
                      {"$"}{((order.payAmount || 0) / 100).toFixed(2)}
                    </TableCell>
                    <TableCell>
                      {order.isPaid ? (
                        <Badge>{"已支付"}</Badge>
                      ) : (
                        <Badge variant="secondary">{"未支付"}</Badge>
                      )}
                    </TableCell>
                    <TableCell>{safeFormatDate(order.createdAt)}</TableCell>
                    <TableCell>{safeFormatDate(order.payAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500 text-center py-4">{"暂无订单记录"}</p>
          )}
        </CardContent>
      </Card>

      {/* Pro会员历史 */}
      <Card>
        <CardHeader>
          <CardTitle>
            {"Pro会员历史 ("}{userDetail.proHistories?.length || 0}{")"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {userDetail.proHistories && userDetail.proHistories.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{"类型"}</TableHead>
                  <TableHead>{"天数"}</TableHead>
                  <TableHead>{"原因"}</TableHead>
                  <TableHead>{"关联订单"}</TableHead>
                  <TableHead>{"时间"}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {userDetail.proHistories.map((history, index) => (
                  <TableRow key={index}>
                    <TableCell>{history.type || "-"}</TableCell>
                    <TableCell>{history.days || 0}</TableCell>
                    <TableCell>{history.reason || "-"}</TableCell>
                    <TableCell>
                      {history.order ? `订单: ${history.order.uuid}` : "-"}
                    </TableCell>
                    <TableCell>{safeFormatDate(history.createdAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500 text-center py-4">
              {"暂无Pro会员历史记录"}
            </p>
          )}
        </CardContent>
      </Card>

      {/* 邀请信息 */}
      <Card>
        <CardHeader>
          <CardTitle>
            {"邀请信息 ("}{userDetail.inviteCodes?.length || 0}{")"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {userDetail.inviteCodes && userDetail.inviteCodes.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{"邀请码"}</TableHead>
                  <TableHead>{"邀请链接"}</TableHead>
                  <TableHead>{"注册数"}</TableHead>
                  <TableHead>{"购买数"}</TableHead>
                  <TableHead>{"购买奖励(天)"}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {userDetail.inviteCodes.map((code, index) => (
                  <TableRow key={code.code || index}>
                    <TableCell className="font-mono">
                      {code.code || "-"}
                    </TableCell>
                    <TableCell className="max-w-xs truncate">
                      {code.link || "-"}
                    </TableCell>
                    <TableCell>{code.registerCount || 0}</TableCell>
                    <TableCell>{code.purchaseCount || 0}</TableCell>
                    <TableCell>{code.purchaseReward || 0}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-gray-500 text-center py-4">{"暂无邀请记录"}</p>
          )}
        </CardContent>
      </Card>

      {/* 添加会员时长对话框 */}
      <Dialog open={isAddingMembership} onOpenChange={setIsAddingMembership}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"添加会员时长"}</DialogTitle>
            <DialogDescription>
              {"为用户添加会员有效期。如果用户当前有有效期，将从有效期末尾追加；如果已过期或未开通，将从当前时间开始计算。"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">{"选择月数"}</label>
              <Select value={membershipMonths} onValueChange={setMembershipMonths}>
                <SelectTrigger>
                  <SelectValue placeholder="选择月数" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">{"1 个月"}</SelectItem>
                  <SelectItem value="3">{"3 个月"}</SelectItem>
                  <SelectItem value="6">{"6 个月"}</SelectItem>
                  <SelectItem value="12">{"12 个月"}</SelectItem>
                  <SelectItem value="24">{"24 个月"}</SelectItem>
                  <SelectItem value="36">{"36 个月"}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">{"变更原因（可选）"}</label>
              <Input
                placeholder="例如：活动赠送、补偿等"
                value={membershipReason}
                onChange={(e) => setMembershipReason(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setIsAddingMembership(false)}
              disabled={isSavingMembership}
            >
              {"取消"}
            </Button>
            <Button onClick={handleSaveMembership} disabled={isSavingMembership}>
              {isSavingMembership ? "添加中..." : "确认添加"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Token 签发结果对话框 */}
      <Dialog open={tokenDialog.show} onOpenChange={(open) => setTokenDialog({ show: open })}>
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>{"Token 签发成功"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            {/* UDID 和 Password - 重要信息放在最前面 */}
            <div className="grid grid-cols-2 gap-4 p-4 bg-blue-50 dark:bg-blue-950 rounded-lg border border-blue-200 dark:border-blue-800">
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium text-blue-700 dark:text-blue-300">{"UDID (用户名)"}</label>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 px-2 text-xs"
                    onClick={() => handleCopyToClipboard(tokenDialog.udid || '', 'UDID')}
                  >
                    {"复制"}
                  </Button>
                </div>
                <div className="p-2 bg-white dark:bg-gray-900 rounded font-mono text-sm break-all">
                  {tokenDialog.udid || '-'}
                </div>
              </div>
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium text-blue-700 dark:text-blue-300">{"Password (密码)"}</label>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 px-2 text-xs"
                    onClick={() => handleCopyToClipboard(tokenDialog.tokenResponse?.password || '', 'Password')}
                  >
                    {"复制"}
                  </Button>
                </div>
                <div className="p-2 bg-white dark:bg-gray-900 rounded font-mono text-sm break-all">
                  {tokenDialog.tokenResponse?.password || '-'}
                </div>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              {"k2oc 协议认证: 用户名 = UDID, 密码 = MD5(AccessToken)"}
            </p>

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">{"Access Token"}</label>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => handleCopyToClipboard(tokenDialog.tokenResponse?.accessToken || '', 'Access Token')}
                >
                  {"复制"}
                </Button>
              </div>
              <div className="p-3 bg-muted rounded-md font-mono text-xs break-all max-h-24 overflow-y-auto">
                {tokenDialog.tokenResponse?.accessToken || '-'}
              </div>
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">{"Refresh Token"}</label>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => handleCopyToClipboard(tokenDialog.tokenResponse?.refreshToken || '', 'Refresh Token')}
                >
                  {"复制"}
                </Button>
              </div>
              <div className="p-3 bg-muted rounded-md font-mono text-xs break-all max-h-24 overflow-y-auto">
                {tokenDialog.tokenResponse?.refreshToken || '-'}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <span className="text-muted-foreground">{"签发时间: "}</span>
                {safeFormatDate(tokenDialog.tokenResponse?.issuedAt || 0)}
              </div>
              <div>
                <span className="text-muted-foreground">{"有效期: "}</span>
                {tokenDialog.tokenResponse?.expiresIn ? `${Math.floor(tokenDialog.tokenResponse.expiresIn / 86400)} 天` : '-'}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setTokenDialog({ show: false })}>
              {"关闭"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 修改邮箱对话框 */}
      <EditEmailDialog
        open={isEditingEmail}
        onOpenChange={setIsEditingEmail}
        userUUID={userDetail.uuid}
        currentEmail={email}
        onSuccess={fetchUserDetail}
      />
    </div>
  );
}

export default function UserDetailPage() {
  return (
    <Suspense fallback={<div>{"Loading..."}</div>}>
      <UserDetailContent />
    </Suspense>
  );
} 