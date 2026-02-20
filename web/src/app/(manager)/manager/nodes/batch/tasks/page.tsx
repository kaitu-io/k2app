"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { api, BatchTaskResponse, BatchScriptResponse } from "@/lib/api";
import { SchedulePicker, ScheduleConfig } from "@/components/batch/schedule-picker";
import { Plus, Eye, Pause, Play, Trash2, List, Loader2, ArrowLeft, RefreshCcw, Link as LinkIcon } from "lucide-react";
import Link from "next/link";

interface SlaveNode {
  id: number;
  name: string;
  ipv4: string;
}

export default function BatchTasksPage() {
  const router = useRouter();
  const [tasks, setTasks] = useState<BatchTaskResponse[]>([]);
  const [scripts, setScripts] = useState<BatchScriptResponse[]>([]);
  const [nodes, setNodes] = useState<SlaveNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [pauseDialogOpen, setPauseDialogOpen] = useState(false);
  const [resumeDialogOpen, setResumeDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedTaskId, setSelectedTaskId] = useState<number | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [formData, setFormData] = useState({
    scriptId: 0,
    nodeIds: [] as number[],
  });

  const [scheduleConfig, setScheduleConfig] = useState<ScheduleConfig>({
    type: "once",
    frequency: "daily",
    time: "02:00",
  });

  const loadTasks = async () => {
    try {
      setLoading(true);
      const response = await api.listBatchTasks({ page: 1, pageSize: 100 });
      setTasks(response.items || []);
    } catch (error) {
      toast.error("加载任务列表失败");
      console.error("Failed to load tasks:", error);
    } finally {
      setLoading(false);
    }
  };

  const loadScripts = async () => {
    try {
      const response = await api.listBatchScripts({ page: 1, pageSize: 100 });
      setScripts(response.items || []);
    } catch (error) {
      console.error("Failed to load scripts:", error);
    }
  };

  const loadNodes = async () => {
    try {
      const response = await api.listSlaveNodes({ page: 1, pageSize: 200 });
      setNodes(response.items || []);
    } catch (error) {
      console.error("Failed to load nodes:", error);
    }
  };

  useEffect(() => {
    loadTasks();
    loadScripts();
    loadNodes();
  }, []);

  // Build cron expression from schedule config
  const buildCronExpr = (config: ScheduleConfig): string => {
    if (config.type !== "cron") return "";

    const [hours, minutes] = (config.time || "02:00").split(":").map(Number);

    switch (config.frequency) {
      case "hourly":
        return "0 * * * *";
      case "daily":
        return `${minutes} ${hours} * * *`;
      case "weekly":
        const days = config.dayOfWeek?.sort().join(",") || "1";
        return `${minutes} ${hours} * * ${days}`;
      case "monthly":
        const dates = config.dayOfMonth?.sort().join(",") || "1";
        return `${minutes} ${hours} ${dates} * *`;
      case "custom":
        return config.cronExpr || "";
      default:
        return "";
    }
  };

  const handleCreate = async () => {
    if (!formData.scriptId) {
      toast.error("请选择脚本");
      return;
    }
    if (formData.nodeIds.length === 0) {
      toast.error("请选择至少一个节点");
      return;
    }

    if (scheduleConfig.type === "cron") {
      const cronExpr = buildCronExpr(scheduleConfig);
      if (!cronExpr) {
        toast.error("请配置有效的执行计划");
        return;
      }
    }

    try {
      setSubmitting(true);

      if (scheduleConfig.type === "once") {
        await api.createBatchTask({
          scriptId: formData.scriptId,
          nodeIds: formData.nodeIds,
          scheduleType: "once",
          executeAt: Date.now(), // Execute immediately
        });
      } else {
        const cronExpr = buildCronExpr(scheduleConfig);
        await api.createBatchTask({
          scriptId: formData.scriptId,
          nodeIds: formData.nodeIds,
          scheduleType: "cron",
          cronExpr,
        });
      }

      toast.success("任务创建成功");
      setCreateDialogOpen(false);
      setFormData({ scriptId: 0, nodeIds: [] });
      setScheduleConfig({ type: "once", frequency: "daily", time: "02:00" });
      loadTasks();
    } catch (error) {
      toast.error("任务创建失败");
      console.error("Failed to create task:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handlePause = async () => {
    if (!selectedTaskId) return;

    try {
      setSubmitting(true);
      await api.pauseBatchTask(selectedTaskId);
      toast.success("任务已暂停");
      setPauseDialogOpen(false);
      setSelectedTaskId(null);
      loadTasks();
    } catch (error) {
      toast.error("暂停任务失败");
      console.error("Failed to pause task:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleResume = async () => {
    if (!selectedTaskId) return;

    try {
      setSubmitting(true);
      await api.resumeBatchTask(selectedTaskId);
      toast.success("任务已继续");
      setResumeDialogOpen(false);
      setSelectedTaskId(null);
      loadTasks();
    } catch (error) {
      toast.error("继续任务失败");
      console.error("Failed to resume task:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!selectedTaskId) return;

    try {
      setSubmitting(true);
      await api.deleteBatchTask(selectedTaskId);
      toast.success("任务删除成功");
      setDeleteDialogOpen(false);
      setSelectedTaskId(null);
      loadTasks();
    } catch (error) {
      toast.error("任务删除失败");
      console.error("Failed to delete task:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleViewDetails = (taskId: number) => {
    router.push(`/manager/nodes/batch/tasks/${taskId}`);
  };

  const formatDate = (timestamp: number) => {
    return new Date(timestamp).toLocaleString("zh-CN");
  };

  const getStatusBadge = (status: string) => {
    const config: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
      pending: { label: "等待中", variant: "secondary" },
      running: { label: "执行中", variant: "default" },
      paused: { label: "已暂停", variant: "outline" },
      completed: { label: "已完成", variant: "default" },
      failed: { label: "失败", variant: "destructive" },
    };

    const { label, variant } = config[status] || { label: status, variant: "default" as const };
    return <Badge variant={variant}>{label}</Badge>;
  };

  const toggleNode = (nodeId: number) => {
    setFormData((prev) => {
      const nodeIds = prev.nodeIds.includes(nodeId)
        ? prev.nodeIds.filter((id) => id !== nodeId)
        : [...prev.nodeIds, nodeId];
      return { ...prev, nodeIds };
    });
  };

  const toggleAllNodes = () => {
    setFormData((prev) => {
      const allNodeIds = nodes.map((n) => n.id);
      const nodeIds = prev.nodeIds.length === allNodeIds.length ? [] : allNodeIds;
      return { ...prev, nodeIds };
    });
  };

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" size="sm" asChild>
            <Link href="/manager/nodes">
              <ArrowLeft className="h-4 w-4 mr-2" />
              返回节点
            </Link>
          </Button>
          <div>
            <h1 className="text-3xl font-bold">批量任务管理</h1>
            <p className="text-muted-foreground mt-1">管理批量执行任务</p>
          </div>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          创建任务
        </Button>
      </div>

      <Card className="p-6">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        ) : tasks.length === 0 ? (
          <div className="text-center py-12">
            <List className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="text-lg font-semibold mb-2">暂无任务</h3>
            <p className="text-muted-foreground mb-4">您还没有创建任何批量任务</p>
            <Button onClick={() => setCreateDialogOpen(true)} variant="outline">
              创建第一个任务
            </Button>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>脚本</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>进度</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tasks.map((task) => (
                <TableRow key={task.id}>
                  <TableCell className="font-medium">
                    <div className="flex items-center gap-2">
                      {task.scriptName}
                      {task.parentTaskId && (
                        <Badge variant="outline" className="text-xs gap-1">
                          <RefreshCcw className="h-3 w-3" />
                          重试
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    {task.scheduleType === "cron" ? (
                      <Badge variant="secondary">定时</Badge>
                    ) : (
                      <Badge variant="outline">一次</Badge>
                    )}
                  </TableCell>
                  <TableCell>{getStatusBadge(task.status)}</TableCell>
                  <TableCell>
                    {task.currentIndex} / {task.totalNodes}
                  </TableCell>
                  <TableCell>{formatDate(task.createdAt)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleViewDetails(task.id)}
                        className="gap-2"
                      >
                        <Eye className="h-4 w-4" />
                        查看
                      </Button>
                      {task.status === "running" || task.status === "pending" ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setSelectedTaskId(task.id);
                            setPauseDialogOpen(true);
                          }}
                          className="gap-2"
                        >
                          <Pause className="h-4 w-4" />
                          暂停
                        </Button>
                      ) : task.status === "paused" ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setSelectedTaskId(task.id);
                            setResumeDialogOpen(true);
                          }}
                          className="gap-2"
                        >
                          <Play className="h-4 w-4" />
                          继续
                        </Button>
                      ) : null}
                      {task.parentTaskId && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => router.push(`/manager/nodes/batch/tasks/${task.parentTaskId}`)}
                          className="gap-1"
                          title="查看原任务"
                        >
                          <LinkIcon className="h-4 w-4" />
                        </Button>
                      )}
                      {(task.status === "completed" || task.status === "failed") && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setSelectedTaskId(task.id);
                            setDeleteDialogOpen(true);
                          }}
                          className="gap-2 text-destructive hover:text-destructive"
                        >
                          <Trash2 className="h-4 w-4" />
                          删除
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent className="max-w-4xl w-[90vw] max-h-[85vh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>创建批量任务</DialogTitle>
          </DialogHeader>
          <div className="flex-1 overflow-y-auto space-y-4 py-4">
            <div>
              <Label htmlFor="script">选择脚本</Label>
              <Select
                value={formData.scriptId.toString()}
                onValueChange={(value) => setFormData({ ...formData, scriptId: parseInt(value) })}
              >
                <SelectTrigger>
                  <SelectValue placeholder="请选择要执行的脚本" />
                </SelectTrigger>
                <SelectContent>
                  {scripts.map((script) => (
                    <SelectItem key={script.id} value={script.id.toString()}>
                      {script.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div>
              <div className="flex items-center justify-between mb-2">
                <Label>选择节点 ({formData.nodeIds.length} 个已选)</Label>
                <Button variant="outline" size="sm" onClick={toggleAllNodes}>
                  {formData.nodeIds.length === nodes.length ? "取消全选" : "全选"}
                </Button>
              </div>
              <div className="border rounded-lg p-4 max-h-60 overflow-y-auto space-y-2">
                {nodes.map((node) => (
                  <div key={node.id} className="flex items-center space-x-2">
                    <Checkbox
                      id={`node-${node.id}`}
                      checked={formData.nodeIds.includes(node.id)}
                      onCheckedChange={() => toggleNode(node.id)}
                    />
                    <label
                      htmlFor={`node-${node.id}`}
                      className="flex-1 text-sm font-medium leading-none cursor-pointer"
                    >
                      {node.name} ({node.ipv4})
                    </label>
                  </div>
                ))}
              </div>
            </div>

            <div>
              <Label className="mb-2 block">执行计划</Label>
              <SchedulePicker value={scheduleConfig} onChange={setScheduleConfig} />
            </div>
          </div>
          <DialogFooter className="flex-shrink-0 border-t pt-4">
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pauseDialogOpen} onOpenChange={setPauseDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>暂停任务</AlertDialogTitle>
            <AlertDialogDescription>确定要暂停该任务吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handlePause} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              暂停
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={resumeDialogOpen} onOpenChange={setResumeDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>继续任务</AlertDialogTitle>
            <AlertDialogDescription>确定要继续该任务吗？</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleResume} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              继续
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除任务</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除该任务吗？只能删除已完成或失败的任务。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
