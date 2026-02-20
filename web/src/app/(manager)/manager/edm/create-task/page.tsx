"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { useRouter } from "next/navigation";
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
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { api } from "@/lib/api";
import { toast } from "sonner";
import {
  Send,
  Users,
  Mail,
  Calendar,
  Search,
  X,
  UserPlus,
  Loader2,
  Filter,
  Settings,
} from "lucide-react";
import type { EmailTemplateResponse, User, UserFilter, EmailTaskPreviewResponse } from "@/lib/api";
import Link from "next/link";

// 辅助函数：从用户对象中获取邮箱
const getUserEmail = (user: User): string => {
  const emailIdentity = user.loginIdentifies?.find(li => li.type === 'email');
  return emailIdentity?.value || user.uuid; // 如果没有邮箱，返回UUID
};

export default function EDMPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [templates, setTemplates] = useState<EmailTemplateResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [previewData, setPreviewData] = useState<EmailTaskPreviewResponse | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  // 用户搜索相关状态
  const [emailSearchQuery, setEmailSearchQuery] = useState("");
  const [searchedUsers, setSearchedUsers] = useState<User[]>([]);
  const [selectedUsers, setSelectedUsers] = useState<User[]>([]);
  const [userSearchLoading, setUserSearchLoading] = useState(false);
  const [, setSearchTimeout] = useState<NodeJS.Timeout | null>(null);
  const [activeTab, setActiveTab] = useState("filters");

  // 从 URL 参数获取预设的模板 ID 和用户 UUIDs
  const presetTemplateId = searchParams.get('templateId');
  const presetUserUuids = searchParams.get('userUuids'); // 支持多个UUID，逗号分隔

  const [formData, setFormData] = useState({
    name: "",
    description: "",
    templateId: presetTemplateId || "",
    userFilters: {
      userStatus: "",
      activatedDate: { start: "", end: "" },
      expireDays: "", // 过期天数筛选（固定预设选项）
      specificUsers: [], // Special users from URL parameter
      retailerLevels: [], // 分销商等级筛选（多选）
    } as UserFilter,
    type: "once" as "once" | "repeat", // Task type: once or repeat
    scheduledAt: "", // 必填：指定执行时间
    repeatEvery: 86400, // Default: 1 day in seconds
  });

  // 【所有用户】状态：当所有筛选条件都为空时为 true
  const [selectAllUsers, setSelectAllUsers] = useState(true);

  // Define filter option constants based on backend implementation
  const USER_STATUS_OPTIONS = [
    { value: "not_activated", label: "未激活", desc: "用户注册但未激活" },
    { value: "activated_no_order", label: "已激活无订单", desc: "已激活但没有订单" },
    { value: "first_order_done", label: "已首单", desc: "已完成首次订单" },
    { value: "first_order_done_but_expired", label: "已首单但过期", desc: "完成首单但已过期" },
  ];

  // 过期天数选项（精确到天，适合定期任务）
  const EXPIRE_DAYS_OPTIONS = [
    { value: "expire_in_30", label: "30天内过期", desc: "将在30天内过期" },
    { value: "expire_in_14", label: "14天内过期", desc: "将在14天内过期" },
    { value: "expire_in_7", label: "7天内过期", desc: "将在7天内过期" },
    { value: "expire_in_3", label: "3天内过期", desc: "将在3天内过期" },
    { value: "expire_in_1", label: "1天内过期", desc: "将在1天内过期" },
    { value: "expired_1", label: "已过期1天", desc: "已过期1天" },
    { value: "expired_3", label: "已过期3天", desc: "已过期3天" },
    { value: "expired_7", label: "已过期7天", desc: "已过期7天" },
    { value: "expired_14", label: "已过期14天", desc: "已过期14天" },
    { value: "expired_30", label: "已过期30天", desc: "已过期30天" },
    { value: "expired", label: "已过期", desc: "已过期用户" },
  ];

  // 分销商等级选项（多选）
  const RETAILER_LEVEL_OPTIONS = [
    { value: 1, label: "1级分销商", desc: "一级分销商" },
    { value: 2, label: "2级分销商", desc: "二级分销商" },
    { value: 3, label: "3级分销商", desc: "三级分销商" },
    { value: 4, label: "4级分销商", desc: "四级分销商" },
  ];

  // Preview user count based on current filters
  const previewUserCount = useCallback(async (filters: UserFilter) => {
    try {
      setPreviewLoading(true);
      const response = await api.previewEmailTask({ userFilters: filters });
      setPreviewData(response);
    } catch (error) {
      console.error("Error previewing user count:", error);
      toast.error("无法预览用户数量");
      setPreviewData(null);
    } finally {
      setPreviewLoading(false);
    }
  }, []);


  const fetchTemplates = useCallback(async () => {
    try {
      const data = await api.getEmailTemplates({ limit: 100 });
      setTemplates(data.items);
    } catch (error) {
      console.error("Error fetching templates:", error);
    } finally {
      setLoading(false);
    }
  }, []);

  // 搜索用户功能
  const searchUsers = useCallback(async (email: string) => {
    try {
      // 验证邮箱格式
      const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
      if (!emailRegex.test(email)) {
        setSearchedUsers([]);
        return;
      }

      // 调用真实的用户搜索 API
      const response = await api.searchUsers({ email });
      setSearchedUsers(response.items);
    } catch (error) {
      console.error("Error searching users:", error);
      setSearchedUsers([]);
    } finally {
      setUserSearchLoading(false);
    }
  }, []);

  // 处理搜索输入的防抖逻辑
  const handleSearchInput = useCallback((email: string) => {
    setSearchTimeout(prev => {
      if (prev) {
        clearTimeout(prev);
      }

      if (!email.trim()) {
        setSearchedUsers([]);
        setUserSearchLoading(false);
        return null;
      }

      setUserSearchLoading(true);
      return setTimeout(() => searchUsers(email), 500);
    });
  }, [searchUsers]);

  // 添加用户到选中列表
  const addUserToSelected = useCallback((user: User) => {
    if (!selectedUsers.find(u => u.uuid === user.uuid)) {
      const newSelectedUsers = [...selectedUsers, user];
      setSelectedUsers(newSelectedUsers);
      setFormData(prev => ({
        ...prev,
        userFilters: {
          ...prev.userFilters,
          specificUsers: newSelectedUsers.map(u => u.uuid),
        },
      }));
    }
    setEmailSearchQuery("");
    setSearchedUsers([]);
  }, [selectedUsers]);

  // 从选中列表移除用户
  const removeUserFromSelected = useCallback((userUuid: string) => {
    const newSelectedUsers = selectedUsers.filter(u => u.uuid !== userUuid);
    setSelectedUsers(newSelectedUsers);
    setFormData(prev => ({
      ...prev,
      userFilters: {
        ...prev.userFilters,
        specificUsers: newSelectedUsers.map(u => u.uuid),
      },
    }));
  }, [selectedUsers]);

  // Trigger preview when filters change
  const handleFiltersUpdate = useCallback((newFilters: UserFilter) => {
    // Update form data
    setFormData(prev => ({ ...prev, userFilters: newFilters }));

    // Preview user count using real API
    previewUserCount(newFilters);
  }, [previewUserCount]);

  const handleCreate = async () => {
    try {
      // 验证必填字段
      if (!formData.name || !formData.templateId) {
        toast.error("请填写必填字段");
        return;
      }

      // 验证执行时间必填
      if (!formData.scheduledAt) {
        toast.error("请指定执行时间");
        return;
      }

      // 如果是循环任务，验证 repeatEvery
      if (formData.type === "repeat" && (!formData.repeatEvery || formData.repeatEvery <= 0)) {
        toast.error("请设置循环间隔");
        return;
      }

      // 构建 API 请求数据
      const requestData = {
        name: formData.name,
        templateId: parseInt(formData.templateId),
        userFilters: formData.userFilters,
        type: formData.type, // 任务类型: once / repeat
        scheduledAt: Math.floor(new Date(formData.scheduledAt).getTime() / 1000), // 必填
        repeatEvery: formData.type === "repeat" ? formData.repeatEvery : undefined, // 循环间隔（秒）
      };

      const response = await api.createEmailTask(requestData);

      toast.success("任务创建成功");
      // Redirect to send-logs page with the batchId for tracking
      router.push(`/manager/edm/send-logs?batchId=${response.batchId}`);
    } catch (error) {
      toast.error("任务创建失败");
      console.error("Error creating task:", error);
    }
  };


  const handleFilterChange = <K extends keyof UserFilter>(filterType: K, value: UserFilter[K]) => {
    const newFilters: UserFilter = {
      ...formData.userFilters,
      [filterType]: value,
    };
    handleFiltersUpdate(newFilters);

    // 如果设置了任何筛选条件，取消"所有用户"选项
    setSelectAllUsers(false);
  };

  // 处理"所有用户"选项的切换
  const handleSelectAllUsersToggle = (checked: boolean) => {
    setSelectAllUsers(checked);

    if (checked) {
      // 清空所有筛选条件
      const emptyFilters: UserFilter = {
        userStatus: "",
        activatedDate: { start: "", end: "" },
        expireDays: "",
        specificUsers: [],
        retailerLevels: [],
      };
      handleFiltersUpdate(emptyFilters);
    }
  };

  // Handle preset user UUIDs from URL parameters
  const handlePresetUserUuids = useCallback(async () => {
    if (presetUserUuids && presetUserUuids.trim() !== '') {
      const uuidList = presetUserUuids.split(',').map(uuid => uuid.trim()).filter(Boolean);

      if (uuidList.length > 0) {
        console.log('处理预设用户UUIDs:', uuidList);

        // Create filter with specific users
        const filtersWithUsers: UserFilter = {
          ...formData.userFilters,
          specificUsers: uuidList,
        };

        // Use the preview API to get user details
        try {
          const previewResponse = await api.previewEmailTask({ userFilters: filtersWithUsers });
          setPreviewData(previewResponse);

          // Update form data with the specific users
          setFormData(prev => ({
            ...prev,
            userFilters: filtersWithUsers,
          }));

          // Switch to condition filters tab to show the results
          setActiveTab('filters');

          console.log('成功加载预设用户:', previewResponse.totalCount, '个用户');
        } catch (error) {
          console.error('加载预设用户失败:', error);
          toast.error('加载预设用户失败');
        }
      }
    }
  }, [presetUserUuids, formData.userFilters]);

  useEffect(() => {
    fetchTemplates();
  }, [fetchTemplates]);

  // Initial preview with empty filters
  useEffect(() => {
    previewUserCount(formData.userFilters);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    handlePresetUserUuids();
  }, [handlePresetUserUuids]);

  if (loading) {
    return <div className="container mx-auto py-6">{"加载中..."}</div>;
  }

  const selectedTemplate = templates.find(tmpl => tmpl.id.toString() === formData.templateId);

  return (
    <div className="container mx-auto py-6 h-screen flex flex-col">
      {/* 页面头部 */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{"创建邮件任务"}</h1>
          <p className="text-sm text-muted-foreground">
            {"设置邮件任务参数并选择目标用户"}
          </p>
        </div>
        <Button onClick={handleCreate}>
          <Send className="mr-2 h-4 w-4" />
          {"创建并发送"}
        </Button>
      </div>

      {/* 主要内容区域 - 左右布局 */}
      <div className="flex gap-6 flex-1 overflow-hidden">
        {/* 左侧内容 */}
        <div className="flex-1 flex flex-col overflow-y-auto">
          {/* 预估统计卡片 - 紧凑布局 */}
          <div className="grid gap-3 grid-cols-3 mb-6">
            <Card className="p-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">{"预估用户数"}</div>
                  <div className="text-xl font-bold">
                    {previewLoading ? (
                      <Loader2 className="h-5 w-5 animate-spin" />
                    ) : (
                      (previewData?.totalCount || 0).toLocaleString()
                    )}
                  </div>
                </div>
                <Users className="h-4 w-4 text-muted-foreground" />
              </div>
            </Card>

            <Card className="p-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">{"已选模板"}</div>
                  <div className="text-sm font-medium truncate">{selectedTemplate?.name || "未选择模板"}</div>
                </div>
                <Mail className="h-4 w-4 text-muted-foreground" />
              </div>
            </Card>

            <Card className="p-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">
                    {formData.type === "once" ? "发送时间" : "计划信息"}
                  </div>
                  <div className="text-sm font-medium">
                    {formData.type === "once" ? (
                      formData.scheduledAt ? new Date(formData.scheduledAt).toLocaleDateString() : "未设置"
                    ) : (
                      `${"循环任务"} - ${
                        formData.repeatEvery === 3600 ? "每小时" :
                        formData.repeatEvery === 86400 ? "每天" :
                        formData.repeatEvery === 604800 ? "每周" :
                        formData.repeatEvery === 2592000 ? "每月" : ""
                      }`
                    )}
                  </div>
                </div>
                <Calendar className="h-4 w-4 text-muted-foreground" />
              </div>
            </Card>
          </div>

          {/* 左侧表单内容 */}
          <div className="space-y-6">
            {/* 基本信息 Section */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center space-x-2">
                  <Settings className="h-4 w-4" />
                  <span>{"基本信息"}</span>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <Label htmlFor="name">{"任务名称"} {"*"}</Label>
                  <Input
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
                    placeholder={"输入任务名称"}
                    className="mt-1"
                  />
                </div>
                <div>
                  <Label htmlFor="description">{"任务描述"}</Label>
                  <Textarea
                    id="description"
                    value={formData.description}
                    onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
                    placeholder={"输入任务描述（可选）"}
                    className="mt-1"
                    rows={2}
                  />
                </div>
              </CardContent>
            </Card>


            {/* 邮件内容 Section */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center space-x-2">
                  <Mail className="h-4 w-4" />
                  <span>{"邮件内容"}</span>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <Label htmlFor="template">{"邮件模板"} {"*"}</Label>
                  <Select
                    value={formData.templateId}
                    onValueChange={(value) => setFormData(prev => ({ ...prev, templateId: value }))}
                  >
                    <SelectTrigger className="mt-1">
                      <SelectValue placeholder={"选择邮件模板"} />
                    </SelectTrigger>
                    <SelectContent>
                      {templates.map((template) => (
                        <SelectItem key={template.id} value={template.id.toString()}>
                          <div>
                            <div className="font-medium">{template.name}</div>
                            <div className="text-sm text-muted-foreground">{template.subject}</div>
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  {selectedTemplate && (
                    <div className="mt-3 p-3 border rounded-lg bg-muted/50">
                      <div className="text-sm space-y-1">
                        <div><strong>{"主题"}{":"}</strong> {selectedTemplate.subject}</div>
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end mt-3">
                    <Link href="/manager/edm/templates">
                      <Button variant="outline" size="sm">
                        {"管理模板"}
                      </Button>
                    </Link>
                  </div>
                </div>
              </CardContent>
            </Card>

            {/* 发送计划 Section */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center space-x-2">
                  <Calendar className="h-4 w-4" />
                  <span>{"发送计划"}</span>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  {/* Task Type Selection */}
                  <div>
                    <Label htmlFor="task-type">{"任务类型"}</Label>
                    <Select
                      value={formData.type}
                      onValueChange={(value) => setFormData(prev => ({ ...prev, type: value as "once" | "repeat" }))}
                    >
                      <SelectTrigger className="mt-1">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="once">{"单次发送"}</SelectItem>
                        <SelectItem value="repeat">{"循环发送"}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {/* Repeat Interval (only for repeat tasks) */}
                  {formData.type === "repeat" && (
                    <div>
                      <Label htmlFor="repeat-every">{"循环间隔"}</Label>
                      <Select
                        value={formData.repeatEvery.toString()}
                        onValueChange={(value) => setFormData(prev => ({ ...prev, repeatEvery: parseInt(value) }))}
                      >
                        <SelectTrigger className="mt-1">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="3600">{"每小时"}</SelectItem>
                          <SelectItem value="86400">{"每天"}</SelectItem>
                          <SelectItem value="604800">{"每周"}</SelectItem>
                          <SelectItem value="2592000">{"每月"}</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  {/* Scheduled Time (always required) */}
                  <div>
                    <Label htmlFor="scheduled-at">
                      {formData.type === "once"
                        ? "发送时间"
                        : "首次执行时间"} {" *"}
                    </Label>
                    <Input
                      id="scheduled-at"
                      type="datetime-local"
                      value={formData.scheduledAt}
                      onChange={(e) => setFormData(prev => ({ ...prev, scheduledAt: e.target.value }))}
                      className="mt-1"
                      required
                    />
                    <p className="text-xs text-muted-foreground mt-1">
                      {formData.type === "once"
                        ? "指定任务执行的具体时间"
                        : "指定循环任务首次执行的时间"}
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>

        {/* 右侧用户筛选面板 */}
        <div className="w-96 flex-shrink-0">
          <Card className="h-full flex flex-col">
            <CardHeader className="flex-shrink-0">
              <CardTitle className="flex items-center space-x-2">
                <Users className="h-4 w-4" />
                <span>{"目标用户"}</span>
              </CardTitle>
            </CardHeader>
            <CardContent className="flex-1 overflow-hidden p-0">
              <Tabs value={activeTab} onValueChange={setActiveTab} className="h-full flex flex-col">
                <div className="px-6 py-2 border-b">
                  <TabsList className="grid w-full grid-cols-2">
                    <TabsTrigger value="filters" className="flex items-center space-x-1">
                      <Filter className="h-3 w-3" />
                      <span>{"条件筛选"}</span>
                    </TabsTrigger>
                    <TabsTrigger value="search" className="flex items-center space-x-1">
                      <Search className="h-3 w-3" />
                      <span>{"搜索用户"}</span>
                    </TabsTrigger>
                  </TabsList>
                </div>

                <div className="flex-1 overflow-y-auto">
                  <TabsContent value="filters" className="p-6 space-y-6 mt-0">
                    {/* 所有用户快捷选项 */}
                    <div className="p-4 border-2 border-primary/20 rounded-lg bg-primary/5">
                      <div className="flex items-center space-x-3">
                        <Checkbox
                          id="select-all-users"
                          checked={selectAllUsers}
                          onCheckedChange={handleSelectAllUsersToggle}
                          className="border-primary data-[state=checked]:bg-primary"
                        />
                        <div className="flex-1">
                          <Label
                            htmlFor="select-all-users"
                            className="text-base font-semibold cursor-pointer flex items-center space-x-2"
                          >
                            <Users className="h-4 w-4" />
                            <span>{"所有用户"}</span>
                          </Label>
                          <p className="text-xs text-muted-foreground mt-1">
                            {"发送给系统中的所有用户"}
                          </p>
                        </div>
                      </div>
                    </div>

                    {/* 用户状态筛选（单选） */}
                    <div>
                      <Label className="text-sm font-medium">{"用户状态"}</Label>
                      <p className="text-xs text-muted-foreground mb-3">{"根据用户状态筛选"}</p>
                      <RadioGroup
                        value={formData.userFilters.userStatus}
                        onValueChange={(value) => {
                          handleFilterChange('userStatus', value);
                        }}
                      >
                        <div className="space-y-2">
                          {/* 添加"不限"选项 */}
                          <div className="flex items-start space-x-2">
                            <RadioGroupItem value="" id="user-status-all" className="mt-1" />
                            <div className="flex-1">
                              <Label htmlFor="user-status-all" className="text-sm font-medium cursor-pointer">
                                {"不限"}
                              </Label>
                              <p className="text-xs text-muted-foreground">{"不限制用户状态"}</p>
                            </div>
                          </div>
                          {USER_STATUS_OPTIONS.map((option) => (
                            <div key={option.value} className="flex items-start space-x-2">
                              <RadioGroupItem value={option.value} id={`user-status-${option.value}`} className="mt-1" />
                              <div className="flex-1">
                                <Label htmlFor={`user-status-${option.value}`} className="text-sm font-medium cursor-pointer">
                                  {option.label}
                                </Label>
                                <p className="text-xs text-muted-foreground">{option.desc}</p>
                              </div>
                            </div>
                          ))}
                        </div>
                      </RadioGroup>
                    </div>

                    {/* 激活日期筛选 */}
                    <div>
                      <Label className="text-sm font-medium">{"激活日期"}</Label>
                      <p className="text-xs text-muted-foreground mb-3">{"按激活日期范围筛选"}</p>

                      <div className="space-y-3">
                        <div>
                          <Label htmlFor="activatedDate-start" className="text-xs text-muted-foreground">{"开始日期"}</Label>
                          <Input
                            id="activatedDate-start"
                            type="date"
                            value={formData.userFilters.activatedDate.start}
                            onChange={(e) => {
                              const newFilters = { ...formData.userFilters };
                              newFilters.activatedDate.start = e.target.value;
                              handleFiltersUpdate(newFilters);
                            }}
                            className="mt-1"
                          />
                        </div>
                        <div>
                          <Label htmlFor="activatedDate-end" className="text-xs text-muted-foreground">{"结束日期"}</Label>
                          <Input
                            id="activatedDate-end"
                            type="date"
                            value={formData.userFilters.activatedDate.end}
                            onChange={(e) => {
                              const newFilters = { ...formData.userFilters };
                              newFilters.activatedDate.end = e.target.value;
                              handleFiltersUpdate(newFilters);
                            }}
                            className="mt-1"
                          />
                        </div>
                      </div>
                    </div>

                    {/* 过期天数筛选（单选） */}
                    <div>
                      <Label className="text-sm font-medium">{"过期天数"}</Label>
                      <p className="text-xs text-muted-foreground mb-3">{"按过期天数筛选"}</p>
                      <RadioGroup
                        value={formData.userFilters.expireDays}
                        onValueChange={(value) => {
                          handleFilterChange('expireDays', value);
                        }}
                      >
                        <div className="space-y-2">
                          {/* 添加"不限"选项 */}
                          <div className="flex items-start space-x-2">
                            <RadioGroupItem value="" id="expire-days-all" className="mt-1" />
                            <div className="flex-1">
                              <Label htmlFor="expire-days-all" className="text-sm font-medium cursor-pointer">
                                {"不限"}
                              </Label>
                              <p className="text-xs text-muted-foreground">{"不限制过期天数"}</p>
                            </div>
                          </div>
                          {EXPIRE_DAYS_OPTIONS.map((option) => (
                            <div key={option.value} className="flex items-start space-x-2">
                              <RadioGroupItem value={option.value} id={`expire-days-${option.value}`} className="mt-1" />
                              <div className="flex-1">
                                <Label htmlFor={`expire-days-${option.value}`} className="text-sm font-medium cursor-pointer">
                                  {option.label}
                                </Label>
                                <p className="text-xs text-muted-foreground">{option.desc}</p>
                              </div>
                            </div>
                          ))}
                        </div>
                      </RadioGroup>
                    </div>

                    {/* 分销商等级筛选（多选） */}
                    <div>
                      <Label className="text-sm font-medium">{"分销商等级"}</Label>
                      <p className="text-xs text-muted-foreground mb-3">{"按分销商等级筛选（多选）"}</p>
                      <div className="space-y-2">
                        {RETAILER_LEVEL_OPTIONS.map((option) => (
                          <div key={option.value} className="flex items-start space-x-2">
                            <Checkbox
                              id={`retailer-level-${option.value}`}
                              checked={formData.userFilters.retailerLevels.includes(option.value)}
                              onCheckedChange={(checked) => {
                                const currentLevels = formData.userFilters.retailerLevels;
                                const newLevels = checked
                                  ? [...currentLevels, option.value]
                                  : currentLevels.filter(level => level !== option.value);
                                handleFilterChange('retailerLevels', newLevels);
                              }}
                              className="mt-1"
                            />
                            <div className="flex-1">
                              <Label htmlFor={`retailer-level-${option.value}`} className="text-sm font-medium cursor-pointer">
                                {option.label}
                              </Label>
                              <p className="text-xs text-muted-foreground">{option.desc}</p>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>

                    {/* Sample users display */}
                    {previewData && previewData.sampleUsers.length > 0 && (
                      <div>
                        <Label className="text-sm font-medium">{"示例用户（最多10个）"}</Label>
                        <p className="text-xs text-muted-foreground mb-3">{"基于当前筛选条件的示例用户"}</p>
                        <div className="border rounded-lg bg-background shadow-sm max-h-40 overflow-y-auto">
                          {previewData.sampleUsers.map((user) => (
                            <div
                              key={user.userId}
                              className="flex items-center justify-between p-3 hover:bg-muted/50 border-b last:border-b-0"
                            >
                              <div className="flex items-center space-x-2">
                                <Users className="h-3 w-3 text-muted-foreground" />
                                <div>
                                  <div className="text-xs font-medium">{user.email}</div>
                                  <div className="text-xs text-muted-foreground">
                                    {user.subscriptionStatus} {" • "} {user.deviceCount} {"设备"}
                                  </div>
                                </div>
                              </div>
                              <div className="text-xs text-muted-foreground">
                                {user.status}
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </TabsContent>

                  <TabsContent value="search" className="p-6 space-y-4 mt-0">
                    {/* 用户搜索 */}
                    <div>
                      <Label className="text-sm font-medium">{"邮箱搜索"}</Label>
                      <p className="text-xs text-muted-foreground mb-3">{"通过邮箱搜索特定用户"}</p>

                      {/* 搜索输入框 */}
                      <div className="relative">
                        <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                        <Input
                          type="email"
                          placeholder={"输入邮箱地址搜索"}
                          value={emailSearchQuery}
                          onChange={(e) => {
                            const value = e.target.value;
                            setEmailSearchQuery(value);
                            handleSearchInput(value);
                          }}
                          className="pl-10"
                        />
                        {userSearchLoading && (
                          <Loader2 className="absolute right-3 top-1/2 transform -translate-y-1/2 h-4 w-4 animate-spin text-muted-foreground" />
                        )}
                      </div>

                      {/* 搜索结果 */}
                      {searchedUsers.length > 0 && (
                        <div className="mt-2 border rounded-lg bg-background shadow-sm max-h-40 overflow-y-auto">
                          {searchedUsers.map((user) => (
                            <div
                              key={user.uuid}
                              className="flex items-center justify-between p-3 hover:bg-muted/50 cursor-pointer border-b last:border-b-0"
                              onClick={() => addUserToSelected(user)}
                            >
                              <div className="flex items-center space-x-2">
                                <UserPlus className="h-3 w-3 text-muted-foreground" />
                                <div>
                                  <div className="text-xs font-medium">{getUserEmail(user)}</div>
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>

                    {/* 选中的用户列表 */}
                    {selectedUsers.length > 0 && (
                      <div>
                        <Label className="text-sm font-medium">{"已选用户"} {"("}{selectedUsers.length}{")"}</Label>
                        <div className="mt-2 space-y-1 max-h-48 overflow-y-auto">
                          {selectedUsers.map((user) => (
                            <div key={user.uuid} className="flex items-center justify-between p-2 bg-muted/30 rounded text-xs">
                              <div className="flex items-center space-x-2 flex-1 min-w-0">
                                <Users className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                                <div className="min-w-0 flex-1">
                                  <div className="font-medium truncate">{getUserEmail(user)}</div>
                                  <div className="text-xs text-muted-foreground">
                                    {user.isFirstOrderDone ? "付费" : "免费"}
                                  </div>
                                </div>
                              </div>
                              <button
                                onClick={() => removeUserFromSelected(user.uuid)}
                                className="p-1 hover:bg-muted rounded flex-shrink-0"
                                type="button"
                              >
                                <X className="h-3 w-3" />
                              </button>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </TabsContent>
                </div>
              </Tabs>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* 底部操作栏 */}
      <div className="flex justify-end items-center pt-4 border-t">
        <Button onClick={handleCreate} className="px-6">
          <Send className="mr-2 h-4 w-4" />
          {"创建并发送"}
        </Button>
      </div>
    </div>
  );
}
