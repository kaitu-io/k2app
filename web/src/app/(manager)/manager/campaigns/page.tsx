"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
  getPaginationRowModel,
} from "@tanstack/react-table";
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
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { api, CampaignResponse, CampaignRequest } from "@/lib/api";
import { toast } from "sonner";
import { Plus, Edit, Trash2, Tag, TrendingUp, BarChart3, Calendar } from "lucide-react";

export default function CampaignsPage() {
  const [campaigns, setCampaigns] = useState<CampaignResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editingCampaign, setEditingCampaign] = useState<CampaignResponse | null>(null);
  const [pagination, setPagination] = useState({
    page: 0,
    pageSize: 10,
    total: 0
  });
  const [filters, setFilters] = useState({
    type: "",
    isActive: undefined as boolean | undefined
  });

  // 表单状态
  const [formData, setFormData] = useState<CampaignRequest>({
    code: "",
    name: "",
    type: "discount",
    value: 80,
    startAt: Math.floor(Date.now() / 1000),
    endAt: Math.floor(Date.now() / 1000) + 30 * 24 * 3600, // 30天后
    description: "",
    isActive: true,
    matcherType: "all",
    maxUsage: 0
  });

  const campaignTypes = [
    { value: "discount", label: "折扣", icon: "%" },
    { value: "coupon", label: "优惠券", icon: "$" }
  ];

  const matcherTypes = [
    { value: "first_order", label: "首单用户" },
    { value: "vip", label: "VIP用户" },
    { value: "all", label: "所有用户" }
  ];

  const columns: ColumnDef<CampaignResponse>[] = [
    {
      accessorKey: "code",
      header: "优惠码",
      cell: ({ row }) => (
        <div className="flex items-center space-x-2">
          <Tag className="h-4 w-4 text-muted-foreground" />
          <div>
            <div className="font-mono font-medium">{row.getValue("code")}</div>
            <div className="text-sm text-muted-foreground">
              {row.original.name}
            </div>
          </div>
        </div>
      ),
    },
    {
      accessorKey: "type",
      header: "类型",
      cell: ({ row }) => {
        const type = row.getValue("type") as string;
        const typeInfo = campaignTypes.find(t => t.value === type);
        return (
          <div className="flex items-center space-x-2">
            <Badge variant={type === "discount" ? "default" : "secondary"}>
              {typeInfo?.icon} {typeInfo?.label || type}
            </Badge>
          </div>
        );
      },
    },
    {
      accessorKey: "value",
      header: "折扣值",
      cell: ({ row }) => {
        const type = row.original.type;
        const value = row.getValue("value") as number;
        if (type === "discount") {
          return <span className="font-medium">{value}{'% OFF'}</span>;
        } else {
          return <span className="font-medium">{'$'}{(value / 100).toFixed(2)}</span>;
        }
      },
    },
    {
      accessorKey: "matcherType",
      header: "适用对象",
      cell: ({ row }) => {
        const matcherType = row.getValue("matcherType") as string;
        const typeInfo = matcherTypes.find(t => t.value === matcherType);
        return <Badge variant="outline">{typeInfo?.label || matcherType}</Badge>;
      },
    },
    {
      header: "使用情况",
      cell: ({ row }) => {
        const campaign = row.original;
        const usageRate = campaign.maxUsage > 0
          ? (campaign.usageCount / campaign.maxUsage * 100).toFixed(1)
          : "无限制";

        return (
          <div className="flex items-center space-x-2">
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
            <div className="text-sm">
              <div className="font-medium">{campaign.usageCount.toLocaleString()}</div>
              <div className="text-muted-foreground">
                {campaign.maxUsage > 0 ? `/ ${campaign.maxUsage.toLocaleString()} (${usageRate}%)` : "无限制"}
              </div>
            </div>
          </div>
        );
      },
    },
    {
      accessorKey: "isActive",
      header: "状态",
      cell: ({ row }) => {
        const isActive = row.getValue("isActive") as boolean;
        const campaign = row.original;
        const now = Date.now() / 1000;
        const isExpired = now > campaign.endAt;
        const isNotStarted = now < campaign.startAt;

        let status = "未启用";
        let variant: "default" | "secondary" | "destructive" | "outline" = "outline";

        if (isActive) {
          if (isExpired) {
            status = "已过期";
            variant = "destructive";
          } else if (isNotStarted) {
            status = "未开始";
            variant = "secondary";
          } else {
            status = "进行中";
            variant = "default";
          }
        }

        return <Badge variant={variant}>{status}</Badge>;
      },
    },
    {
      header: "活动时间",
      cell: ({ row }) => {
        const campaign = row.original;
        const startDate = new Date(campaign.startAt * 1000).toLocaleDateString();
        const endDate = new Date(campaign.endAt * 1000).toLocaleDateString();

        return (
          <div className="flex items-center space-x-2">
            <Calendar className="h-4 w-4 text-muted-foreground" />
            <div className="text-sm">
              <div>{startDate}</div>
              <div className="text-muted-foreground">{"至"} {endDate}</div>
            </div>
          </div>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }) => {
        const campaign = row.original;
        return (
          <div className="flex items-center space-x-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleEdit(campaign)}
            >
              <Edit className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleViewStats(campaign.code)}
            >
              <BarChart3 className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleDelete(campaign.id)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        );
      },
    },
  ];

  const table = useReactTable({
    data: campaigns,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount: Math.ceil(pagination.total / pagination.pageSize),
    state: {
      pagination: {
        pageIndex: pagination.page,
        pageSize: pagination.pageSize,
      },
    },
    onPaginationChange: (updater) => {
      const newPagination = typeof updater === 'function'
        ? updater({ pageIndex: pagination.page, pageSize: pagination.pageSize })
        : updater;

      setPagination(prev => ({
        ...prev,
        page: newPagination.pageIndex,
        pageSize: newPagination.pageSize
      }));
    },
  });

  const fetchCampaigns = useCallback(async () => {
    try {
      setLoading(true);
      const response = await api.getCampaigns({
        page: pagination.page,
        pageSize: pagination.pageSize,
        ...filters
      });
      setCampaigns(response.items);
      setPagination(prev => ({ ...prev, total: response.pagination.total }));
    } catch (error) {
      toast.error("获取活动列表失败");
      console.error("Error fetching campaigns:", error);
    } finally {
      setLoading(false);
    }
  }, [pagination.page, pagination.pageSize, filters]);

  useEffect(() => {
    fetchCampaigns();
  }, [fetchCampaigns]);

  const resetForm = () => {
    setFormData({
      code: "",
      name: "",
      type: "discount",
      value: 80,
      startAt: Math.floor(Date.now() / 1000),
      endAt: Math.floor(Date.now() / 1000) + 30 * 24 * 3600,
      description: "",
      isActive: true,
      matcherType: "all",
      maxUsage: 0
    });
  };

  const handleCreate = async () => {
    try {
      await api.createCampaign(formData);
      toast.success("活动创建成功");
      setCreateDialogOpen(false);
      resetForm();
      fetchCampaigns();
    } catch (error) {
      toast.error("活动创建失败");
      console.error("Error creating campaign:", error);
    }
  };

  const handleEdit = (campaign: CampaignResponse) => {
    setEditingCampaign(campaign);
    setFormData({
      code: campaign.code,
      name: campaign.name,
      type: campaign.type,
      value: campaign.value,
      startAt: campaign.startAt,
      endAt: campaign.endAt,
      description: campaign.description,
      isActive: campaign.isActive,
      matcherType: campaign.matcherType,
      maxUsage: campaign.maxUsage
    });
    setEditDialogOpen(true);
  };

  const handleUpdate = async () => {
    if (!editingCampaign) return;

    try {
      await api.updateCampaign(editingCampaign.id, formData);
      toast.success("活动更新成功");
      setEditDialogOpen(false);
      setEditingCampaign(null);
      resetForm();
      fetchCampaigns();
    } catch (error) {
      toast.error("活动更新失败");
      console.error("Error updating campaign:", error);
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm("确定要删除这个活动吗？")) return;

    try {
      await api.deleteCampaign(id);
      toast.success("活动删除成功");
      fetchCampaigns();
    } catch (error) {
      toast.error("活动删除失败");
      console.error("Error deleting campaign:", error);
    }
  };

  const handleViewStats = async (code: string) => {
    // 这里可以跳转到统计页面或打开统计对话框
    toast.info(`查看活动 ${code} 的统计数据`);
  };


  const handleDateTimeChange = (field: 'startAt' | 'endAt', value: string) => {
    const timestamp = Math.floor(new Date(value).getTime() / 1000);
    setFormData(prev => ({ ...prev, [field]: timestamp }));
  };

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold">{"优惠活动管理"}</h1>
          <p className="text-muted-foreground">
            {"管理推广活动和优惠码"}
          </p>
        </div>
        <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button onClick={resetForm}>
              <Plus className="mr-2 h-4 w-4" />
              {"创建活动"}
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>{"创建新活动"}</DialogTitle>
              <DialogDescription>
                {"填写活动信息以创建新的优惠活动"}
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="code">{"优惠码 *"}</Label>
                  <Input
                    id="code"
                    value={formData.code}
                    onChange={(e) => setFormData(prev => ({ ...prev, code: e.target.value }))}
                    placeholder={"输入优惠码"}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="name">{"活动名称 *"}</Label>
                  <Input
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
                    placeholder={"输入活动名称"}
                  />
                </div>
              </div>

              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="type">{"活动类型"} {'*'}</Label>
                  <Select
                    value={formData.type}
                    onValueChange={(value) => setFormData(prev => ({ ...prev, type: value }))}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {campaignTypes.map(type => (
                        <SelectItem key={type.value} value={type.value}>
                          {type.icon} {type.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="value">
                    {"折扣值"} {'*'} {formData.type === 'discount' ? '(%)' : '($)'}
                  </Label>
                  <Input
                    id="value"
                    type="number"
                    value={formData.value}
                    onChange={(e) => setFormData(prev => ({ ...prev, value: Number(e.target.value) }))}
                    placeholder={formData.type === 'discount' ? '80' : '500'}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="matcherType">{"适用对象"} {'*'}</Label>
                  <Select
                    value={formData.matcherType}
                    onValueChange={(value) => setFormData(prev => ({ ...prev, matcherType: value }))}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {matcherTypes.map(type => (
                        <SelectItem key={type.value} value={type.value}>
                          {type.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="startAt">{"开始时间"} {'*'}</Label>
                  <Input
                    id="startAt"
                    type="datetime-local"
                    value={new Date(formData.startAt * 1000).toISOString().slice(0, 16)}
                    onChange={(e) => handleDateTimeChange('startAt', e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="endAt">{"结束时间"} {'*'}</Label>
                  <Input
                    id="endAt"
                    type="datetime-local"
                    value={new Date(formData.endAt * 1000).toISOString().slice(0, 16)}
                    onChange={(e) => handleDateTimeChange('endAt', e.target.value)}
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="maxUsage">{"最大使用次数"} {'(0='}{"无限制"}{')'}</Label>
                  <Input
                    id="maxUsage"
                    type="number"
                    value={formData.maxUsage}
                    onChange={(e) => setFormData(prev => ({ ...prev, maxUsage: Number(e.target.value) }))}
                    placeholder="0"
                  />
                </div>
                <div className="space-y-2 flex items-center">
                  <div className="flex items-center space-x-2">
                    <Switch
                      id="isActive"
                      checked={formData.isActive}
                      onCheckedChange={(checked) => setFormData(prev => ({ ...prev, isActive: checked }))}
                    />
                    <Label htmlFor="isActive">{"启用活动"}</Label>
                  </div>
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="description">{"活动描述"}</Label>
                <Textarea
                  id="description"
                  value={formData.description}
                  onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
                  placeholder="输入活动的详细描述..."
                  rows={3}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
                {"取消"}
              </Button>
              <Button onClick={handleCreate}>
                {"创建活动"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>{"筛选条件"}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex space-x-4">
            <Select
              value={filters.type}
              onValueChange={(value) => setFilters(prev => ({ ...prev, type: value }))}
            >
              <SelectTrigger className="w-48">
                <SelectValue placeholder={"全部类型"} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{"全部类型"}</SelectItem>
                {campaignTypes.map(type => (
                  <SelectItem key={type.value} value={type.value}>
                    {type.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select
              value={filters.isActive?.toString() || ""}
              onValueChange={(value) => setFilters(prev => ({
                ...prev,
                isActive: value === "" ? undefined : value === "true"
              }))}
            >
              <SelectTrigger className="w-48">
                <SelectValue placeholder={"全部状态"} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{"全部状态"}</SelectItem>
                <SelectItem value="true">{"已启用"}</SelectItem>
                <SelectItem value="false">{"已禁用"}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      {/* Table */}
      <Card>
        <CardContent className="p-0">
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
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={columns.length} className="h-24 text-center">
                      {"加载中..."}
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
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : (
                  <TableRow>
                    <TableCell colSpan={columns.length} className="h-24 text-center">
                      {"暂无活动数据"}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
          <div className="flex items-center justify-between space-x-2 py-4 px-4">
            <div className="text-sm text-muted-foreground">
              {`共 ${pagination.total} 条记录`}
            </div>
            <div className="flex items-center space-x-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => table.previousPage()}
                disabled={!table.getCanPreviousPage()}
              >
{"上一页"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => table.nextPage()}
                disabled={!table.getCanNextPage()}
              >
{"下一页"}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Edit Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{"编辑活动"}</DialogTitle>
            <DialogDescription>
              {"修改活动信息"}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            {/* Same form fields as create dialog */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="edit-code">{"活动代码"} {'*'}</Label>
                <Input
                  id="edit-code"
                  value={formData.code}
                  onChange={(e) => setFormData(prev => ({ ...prev, code: e.target.value }))}
                  placeholder="SUMMER2024"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-name">{"活动名称"} {'*'}</Label>
                <Input
                  id="edit-name"
                  value={formData.name}
                  onChange={(e) => setFormData(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="夏季促销活动"
                />
              </div>
            </div>

            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label htmlFor="edit-type">{"活动类型"} {'*'}</Label>
                <Select
                  value={formData.type}
                  onValueChange={(value) => setFormData(prev => ({ ...prev, type: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {campaignTypes.map(type => (
                      <SelectItem key={type.value} value={type.value}>
                        {type.icon} {type.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-value">
                  {"折扣值"} {'*'} {formData.type === 'discount' ? '(%)' : '($)'}
                </Label>
                <Input
                  id="edit-value"
                  type="number"
                  value={formData.value}
                  onChange={(e) => setFormData(prev => ({ ...prev, value: Number(e.target.value) }))}
                  placeholder={formData.type === 'discount' ? '80' : '500'}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-matcherType">{"适用对象"} {'*'}</Label>
                <Select
                  value={formData.matcherType}
                  onValueChange={(value) => setFormData(prev => ({ ...prev, matcherType: value }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {matcherTypes.map(type => (
                      <SelectItem key={type.value} value={type.value}>
                        {type.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="edit-startAt">{"开始时间"} {'*'}</Label>
                <Input
                  id="edit-startAt"
                  type="datetime-local"
                  value={new Date(formData.startAt * 1000).toISOString().slice(0, 16)}
                  onChange={(e) => handleDateTimeChange('startAt', e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="edit-endAt">{"结束时间"} {'*'}</Label>
                <Input
                  id="edit-endAt"
                  type="datetime-local"
                  value={new Date(formData.endAt * 1000).toISOString().slice(0, 16)}
                  onChange={(e) => handleDateTimeChange('endAt', e.target.value)}
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="edit-maxUsage">{"最大使用次数"} {'(0='}{"无限制"}{')'}</Label>
                <Input
                  id="edit-maxUsage"
                  type="number"
                  value={formData.maxUsage}
                  onChange={(e) => setFormData(prev => ({ ...prev, maxUsage: Number(e.target.value) }))}
                  placeholder="0"
                />
              </div>
              <div className="space-y-2 flex items-center">
                <div className="flex items-center space-x-2">
                  <Switch
                    id="edit-isActive"
                    checked={formData.isActive}
                    onCheckedChange={(checked) => setFormData(prev => ({ ...prev, isActive: checked }))}
                  />
                  <Label htmlFor="edit-isActive">{"启用活动"}</Label>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="edit-description">{"活动描述"}</Label>
              <Textarea
                id="edit-description"
                value={formData.description}
                onChange={(e) => setFormData(prev => ({ ...prev, description: e.target.value }))}
                placeholder="输入活动的详细描述..."
                rows={3}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              {"取消"}
            </Button>
            <Button onClick={handleUpdate}>
              {"更新活动"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}