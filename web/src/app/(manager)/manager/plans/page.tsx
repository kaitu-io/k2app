"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
  getPaginationRowModel,
  Row,
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
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { api } from "@/lib/api";
import { toast } from "sonner";

// 定义套餐数据结构
interface Plan {
  id: number;          // 数据库 ID
  createdAt: string;   // 创建时间
  updatedAt: string;   // 更新时间
  pid: string;         // 套餐标识符
  label: string;       // 套餐名称
  price: number;       // 价格（美分）
  originPrice: number; // 原价（美分）
  month: number;       // 月数
  highlight: boolean;  // 是否高亮显示
  isActive: boolean;   // 是否激活
}

interface PlanListResponse {
  items: Plan[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

interface PlanFormData {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  isActive: boolean;
}

export default function PlansPage() {
  const [data, setData] = useState<Plan[]>([]);
  const [pageCount, setPageCount] = useState(0);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [pageSize] = useState(10);
  const [isLoading, setIsLoading] = useState(false);

  // 对话框状态
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null);

  // 表单数据
  const [formData, setFormData] = useState<PlanFormData>({
    pid: "",
    label: "",
    price: 0,
    originPrice: 0,
    month: 0,
    highlight: false,
    isActive: true,
  });

  const columns: ColumnDef<Plan>[] = [
    {
      accessorKey: "pid",
      header: "套餐ID",
      cell: ({ row }: { row: Row<Plan> }) => (
        <span className="font-mono">{row.getValue("pid")}</span>
      ),
    },
    {
      accessorKey: "label",
      header: "套餐名称",
    },
    {
      accessorKey: "price",
      header: "价格",
      cell: ({ row }: { row: Row<Plan> }) => {
        const price = row.getValue("price") as number;
        return `¥${(price / 100).toFixed(2)}`;
      },
    },
    {
      accessorKey: "originPrice",
      header: "原价",
      cell: ({ row }: { row: Row<Plan> }) => {
        const originPrice = row.getValue("originPrice") as number;
        return `¥${(originPrice / 100).toFixed(2)}`;
      },
    },
    {
      accessorKey: "month",
      header: "月数",
    },
    {
      accessorKey: "highlight",
      header: "高亮",
      cell: ({ row }: { row: Row<Plan> }) =>
        row.getValue("highlight") ? (
          <Badge>{"推荐"}</Badge>
        ) : (
          <Badge variant="secondary">{"普通"}</Badge>
        ),
    },
    {
      accessorKey: "isActive",
      header: "状态",
      cell: ({ row }: { row: Row<Plan> }) => {
        const isActive = row.getValue("isActive") as boolean;
        return isActive ? (
          <Badge>{"激活"}</Badge>
        ) : (
          <Badge variant="destructive">{"禁用"}</Badge>
        );
      },
    },
    {
      id: "actions",
      header: "操作",
      cell: ({ row }: { row: Row<Plan> }) => {
        const plan = row.original;
        
        return (
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleEdit(plan)}
            >
              {"编辑"}
            </Button>
            {plan.isActive ? (
              <Button
                variant="destructive"
                size="sm"
                onClick={() => handleDelete(plan.id)}
              >
                {"禁用"}
              </Button>
            ) : (
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleRestore(plan.id)}
              >
                {"激活"}
              </Button>
            )}
          </div>
        );
      },
    },
  ];

  useEffect(() => {
    fetchPlans();
  }, [page, pageSize]); // eslint-disable-line react-hooks/exhaustive-deps

  const fetchPlans = async () => {
    setIsLoading(true);
    try {
      const response = await api.request<PlanListResponse>(
        `/app/plans?page=${page}&pageSize=${pageSize}`
      );

      setData(response.items || []);
      setPageCount(
        Math.ceil(response.pagination.total / response.pagination.pageSize)
      );
      setTotal(response.pagination.total);
    } catch (error) {
      console.error("Failed to fetch plans:", error);
    } finally {
      setIsLoading(false);
    }
  };

  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    pageCount,
    state: {
      pagination: {
        pageIndex: page,
        pageSize: pageSize,
      },
    },
    manualPagination: true,
  });

  const resetForm = () => {
    setFormData({
      pid: "",
      label: "",
      price: 0,
      originPrice: 0,
      month: 0,
      highlight: false,
      isActive: true,
    });
  };

  const handleCreate = async () => {
    try {
      await api.request("/app/plans", {
        method: "POST",
        body: JSON.stringify(formData),
      });
      toast.success("套餐创建成功");
      setIsCreateDialogOpen(false);
      resetForm();
      fetchPlans();
    } catch (error) {
      console.error("Failed to create plan:", error);
    }
  };

  const handleEdit = (plan: Plan) => {
    setEditingPlan(plan);
    setFormData({
      pid: plan.pid,
      label: plan.label,
      price: plan.price,
      originPrice: plan.originPrice,
      month: plan.month,
      highlight: plan.highlight,
      isActive: plan.isActive,
    });
    setIsEditDialogOpen(true);
  };

  const handleUpdate = async () => {
    if (!editingPlan) return;

    try {
      await api.request(`/app/plans/${editingPlan.id}`, {
        method: "PUT",
        body: JSON.stringify({
          label: formData.label,
          price: formData.price,
          originPrice: formData.originPrice,
          month: formData.month,
          highlight: formData.highlight,
          isActive: formData.isActive,
        }),
      });
      toast.success("套餐更新成功");
      setIsEditDialogOpen(false);
      setEditingPlan(null);
      resetForm();
      fetchPlans();
    } catch (error) {
      console.error("Failed to update plan:", error);
    }
  };

  const handleDelete = async (planId: number) => {
    if (!confirm("确定要禁用这个套餐吗？")) return;

    try {
      await api.request(`/app/plans/${planId}`, {
        method: "DELETE",
      });
      toast.success("套餐禁用成功");
      fetchPlans();
    } catch (error) {
      console.error("Failed to delete plan:", error);
    }
  };

  const handleRestore = async (planId: number) => {
    try {
      await api.request(`/app/plans/${planId}/restore`, {
        method: "POST",
      });
      toast.success("套餐激活成功");
      fetchPlans();
    } catch (error) {
      console.error("Failed to restore plan:", error);
    }
  };

  // 可用的套餐ID列表 (需要在翻译文件中定义)
  const availablePlanIds = ['1m', '3m', '6m', '1y', '2y', '3y'];

  return (
    <div className="container mx-auto py-10">
      {/* 警告提示 */}
      <div className="mb-6 p-4 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg">
        <div className="flex items-start">
          <div className="flex-shrink-0">
            <svg className="h-5 w-5 text-amber-400" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
            </svg>
          </div>
          <div className="ml-3">
            <h3 className="text-sm font-medium text-amber-800 dark:text-amber-200">
              {"套餐ID 翻译限制"}
            </h3>
            <div className="mt-2 text-sm text-amber-700 dark:text-amber-300">
              <p>
                {"套餐ID必须在翻译文件"} <code className="bg-amber-100 dark:bg-amber-800 dark:text-amber-100 px-1 rounded">{"web/messages/*.json"}</code> {"的"}
                <code className="bg-amber-100 dark:bg-amber-800 dark:text-amber-100 px-1 rounded">{"plan.pid"}</code> {"中存在，否则前端无法正确显示翻译文本。"}
              </p>
              <p className="mt-2">
                <strong>{"可用的套餐ID："}</strong> 
                {availablePlanIds.length > 0 ? (
                  <span className="ml-1">
                    {availablePlanIds.map((id, index) => (
                      <span key={id}>
                        <code className="bg-amber-100 dark:bg-amber-800 dark:text-amber-100 px-1 rounded text-xs">{id}</code>
                        {index < availablePlanIds.length - 1 && ", "}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="ml-1 text-red-600">{"未找到可用的套餐ID翻译"}</span>
                )}
              </p>
            </div>
          </div>
        </div>
      </div>

      <div className="flex justify-between items-center mb-4">
        <h1 className="text-2xl font-bold">{"套餐管理"}</h1>
        <Dialog open={isCreateDialogOpen} onOpenChange={setIsCreateDialogOpen}>
          <DialogTrigger asChild>
            <Button>{"添加套餐"}</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{"创建新套餐"}</DialogTitle>
              <DialogDescription>
                {"填写套餐信息来创建新的套餐。"}
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="pid" className="text-right">
                  {"套餐ID"}
                </Label>
                <Input
                  id="pid"
                  value={formData.pid}
                  onChange={(e) =>
                    setFormData({ ...formData, pid: e.target.value })
                  }
                  className="col-span-3"
                  placeholder="例如: 1y, 2y, 3y"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="label" className="text-right">
                  {"套餐名称"}
                </Label>
                <Input
                  id="label"
                  value={formData.label}
                  onChange={(e) =>
                    setFormData({ ...formData, label: e.target.value })
                  }
                  className="col-span-3"
                  placeholder="例如: 1年套餐"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="price" className="text-right">
                  {"价格（美分）"}
                </Label>
                <Input
                  id="price"
                  type="number"
                  value={formData.price}
                  onChange={(e) =>
                    setFormData({ ...formData, price: parseInt(e.target.value) })
                  }
                  className="col-span-3"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="originPrice" className="text-right">
                  {"原价（美分）"}
                </Label>
                <Input
                  id="originPrice"
                  type="number"
                  value={formData.originPrice}
                  onChange={(e) =>
                    setFormData({
                      ...formData,
                      originPrice: parseInt(e.target.value),
                    })
                  }
                  className="col-span-3"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="month" className="text-right">
                  {"月数"}
                </Label>
                <Input
                  id="month"
                  type="number"
                  value={formData.month}
                  onChange={(e) =>
                    setFormData({ ...formData, month: parseInt(e.target.value) })
                  }
                  className="col-span-3"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label className="text-right">{"高亮推荐"}</Label>
                <div className="col-span-3">
                  <Checkbox
                    checked={formData.highlight}
                    onCheckedChange={(checked) =>
                      setFormData({ ...formData, highlight: checked as boolean })
                    }
                  />
                </div>
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label className="text-right">{"激活状态"}</Label>
                <div className="col-span-3">
                  <Checkbox
                    checked={formData.isActive}
                    onCheckedChange={(checked) =>
                      setFormData({ ...formData, isActive: checked as boolean })
                    }
                  />
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate}>{"创建"}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* 编辑对话框 */}
      <Dialog open={isEditDialogOpen} onOpenChange={setIsEditDialogOpen} key={editingPlan?.id}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{"编辑套餐"}</DialogTitle>
            <DialogDescription>
              {"修改套餐信息。"}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="edit-pid" className="text-right">
                {"套餐ID"}
              </Label>
              <Input
                id="edit-pid"
                value={formData.pid}
                disabled
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="edit-label" className="text-right">
                {"套餐名称"}
              </Label>
              <Input
                id="edit-label"
                value={formData.label}
                onChange={(e) =>
                  setFormData({ ...formData, label: e.target.value })
                }
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="edit-price" className="text-right">
                {"价格（美分）"}
              </Label>
              <Input
                id="edit-price"
                type="number"
                value={formData.price}
                onChange={(e) =>
                  setFormData({ ...formData, price: parseInt(e.target.value) })
                }
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="edit-originPrice" className="text-right">
                {"原价（美分）"}
              </Label>
              <Input
                id="edit-originPrice"
                type="number"
                value={formData.originPrice}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    originPrice: parseInt(e.target.value),
                  })
                }
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="edit-month" className="text-right">
                {"月数"}
              </Label>
              <Input
                id="edit-month"
                type="number"
                value={formData.month}
                onChange={(e) =>
                  setFormData({ ...formData, month: parseInt(e.target.value) })
                }
                className="col-span-3"
              />
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label className="text-right">{"高亮推荐"}</Label>
              <div className="col-span-3">
                <Checkbox
                  checked={formData.highlight}
                  onCheckedChange={(checked) =>
                    setFormData({ ...formData, highlight: checked as boolean })
                  }
                />
              </div>
            </div>
            <div className="grid grid-cols-4 items-center gap-4">
              <Label className="text-right">{"激活状态"}</Label>
              <div className="col-span-3">
                <Checkbox
                  checked={formData.isActive}
                  onCheckedChange={(checked) =>
                    setFormData({ ...formData, isActive: checked as boolean })
                  }
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={handleUpdate}>{"更新"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
            {table.getRowModel().rows?.length ? (
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
                  {isLoading ? "加载中..." : "暂无数据"}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-between space-x-2 py-4">
        <div className="text-sm text-muted-foreground">
          {"共 "}{total}{" 条记录"}
        </div>
        <div className="flex items-center space-x-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage(Math.max(0, page - 1))}
            disabled={page === 0}
          >
            {"上一页"}
          </Button>
          <div className="text-sm">
            {"第 "}{page + 1}{" 页，共 "}{pageCount}{" 页"}
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage(Math.min(pageCount - 1, page + 1))}
            disabled={page >= pageCount - 1}
          >
            {"下一页"}
          </Button>
        </div>
      </div>
    </div>
  );
} 