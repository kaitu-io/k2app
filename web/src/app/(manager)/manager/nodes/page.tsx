"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useMemo } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
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
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  api,
  NodeBatchMatrixResponse,
  NodeBatchMatrixResult,
  NodeBatchMatrixTunnel,
  BatchScriptResponse,
} from "@/lib/api";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import {
  MoreVertical,
  CheckCircle,
  XCircle,
  Minus,
  RefreshCw,
  Trash2,
  Terminal,
  FileText,
  ClipboardList,
  Play,
  Loader2,
} from "lucide-react";
import Link from "next/link";

// Format relative time (e.g., "5分钟前")
function formatRelativeTime(timestamp: number): string {
  const now = Date.now() / 1000;
  const diff = now - timestamp;

  if (diff < 60) return "刚刚";
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`;
  if (diff < 2592000) return `${Math.floor(diff / 86400)}天前`;
  return new Date(timestamp * 1000).toLocaleDateString("zh-CN");
}

// Format execution time
function formatExecutionTime(timestamp: number): string {
  return new Date(timestamp).toLocaleString("zh-CN");
}

// Task result cell component
function TaskResultCell({
  result,
  scriptName,
  onViewDetail,
}: {
  result: NodeBatchMatrixResult | null;
  scriptName: string;
  onViewDetail: () => void;
}) {
  if (!result) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="flex justify-center">
            <Minus className="h-5 w-5 text-gray-300" />
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <p>{scriptName}: 未执行</p>
        </TooltipContent>
      </Tooltip>
    );
  }

  const isSuccess = result.status === "success";

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={onViewDetail}
          className="flex justify-center cursor-pointer hover:scale-110 transition-transform"
        >
          {isSuccess ? (
            <CheckCircle className="h-5 w-5 text-green-500" />
          ) : (
            <XCircle className="h-5 w-5 text-red-500" />
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent>
        <div className="space-y-1">
          <p className="font-medium">{scriptName}</p>
          <p className="text-xs">
            状态: {isSuccess ? "成功" : "失败"}
          </p>
          <p className="text-xs">
            退出码: {result.exitCode}
          </p>
          <p className="text-xs text-muted-foreground">
            点击查看详情
          </p>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

export default function NodesPage() {
  const [data, setData] = useState<NodeBatchMatrixResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [deleteNodeIpv4, setDeleteNodeIpv4] = useState<string | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [selectedResult, setSelectedResult] = useState<{
    result: NodeBatchMatrixResult;
    scriptName: string;
    nodeName: string;
  } | null>(null);

  // Multi-selection state
  const [selectedNodeIds, setSelectedNodeIds] = useState<Set<number>>(new Set());
  const [quickActionDialogOpen, setQuickActionDialogOpen] = useState(false);
  const [scripts, setScripts] = useState<BatchScriptResponse[]>([]);
  const [selectedScriptId, setSelectedScriptId] = useState<number | null>(null);
  const [creatingTask, setCreatingTask] = useState(false);

  // All selected state
  const allSelected = useMemo(() => {
    if (!data?.nodes || data.nodes.length === 0) return false;
    return data.nodes.every((node) => selectedNodeIds.has(node.id));
  }, [data?.nodes, selectedNodeIds]);

  const someSelected = useMemo(() => {
    return selectedNodeIds.size > 0 && !allSelected;
  }, [selectedNodeIds.size, allSelected]);

  const toggleAllSelection = () => {
    if (allSelected) {
      setSelectedNodeIds(new Set());
    } else if (data?.nodes) {
      setSelectedNodeIds(new Set(data.nodes.map((node) => node.id)));
    }
  };

  const toggleNodeSelection = (nodeId: number) => {
    setSelectedNodeIds((prev) => {
      const next = new Set(prev);
      if (next.has(nodeId)) {
        next.delete(nodeId);
      } else {
        next.add(nodeId);
      }
      return next;
    });
  };

  const handleOpenQuickAction = async () => {
    setQuickActionDialogOpen(true);
    try {
      const response = await api.listBatchScripts({ page: 1, pageSize: 100 });
      setScripts(response.items || []);
    } catch (error) {
      toast.error("加载脚本列表失败");
      console.error("Failed to load scripts:", error);
    }
  };

  const handleCreateQuickTask = async () => {
    if (!selectedScriptId || selectedNodeIds.size === 0) return;

    try {
      setCreatingTask(true);
      const response = await api.createBatchTask({
        scriptId: selectedScriptId,
        nodeIds: Array.from(selectedNodeIds),
        scheduleType: "once",
        executeAt: Date.now(),
      });
      toast.success(`任务创建成功，ID: ${response.id}`);
      setQuickActionDialogOpen(false);
      setSelectedScriptId(null);
      setSelectedNodeIds(new Set());
      // Navigate to task detail
      window.location.href = `/manager/nodes/batch/tasks/${response.id}`;
    } catch (error) {
      toast.error("创建任务失败");
      console.error("Failed to create task:", error);
    } finally {
      setCreatingTask(false);
    }
  };

  const fetchData = async () => {
    setIsLoading(true);
    try {
      const response = await api.getNodesBatchMatrix();
      setData(response);
    } catch (error) {
      console.error("Failed to fetch nodes data:", error);
      toast.error("获取节点数据失败");
    } finally {
      setIsLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteNodeIpv4) return;

    setIsDeleting(true);
    try {
      await api.deleteSlaveNode(deleteNodeIpv4);
      toast.success("节点删除成功");
      setDeleteNodeIpv4(null);
      fetchData();
    } catch (error) {
      console.error("Failed to delete node:", error);
      if (error instanceof Error) {
        toast.error(error.message);
      } else {
        toast.error("删除节点失败");
      }
    } finally {
      setIsDeleting(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex justify-between items-center mb-6">
          <h1 className="text-3xl font-bold">节点运维</h1>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              asChild
            >
              <Link href="/manager/nodes/batch/scripts">
                <FileText className="h-4 w-4 mr-2" />
                批量脚本
              </Link>
            </Button>
            <Button
              variant="outline"
              size="sm"
              asChild
            >
              <Link href="/manager/nodes/batch/tasks">
                <ClipboardList className="h-4 w-4 mr-2" />
                批量任务
              </Link>
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={fetchData}
              disabled={isLoading}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
              刷新
            </Button>
          </div>
        </div>

        {/* Batch action bar */}
        {selectedNodeIds.size > 0 && (
          <div className="mb-4 flex items-center gap-4 p-3 rounded-lg bg-muted/50 border">
            <Badge variant="secondary" className="text-sm">
              已选择 {selectedNodeIds.size} 个节点
            </Badge>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                onClick={handleOpenQuickAction}
                className="gap-2"
              >
                <Play className="h-4 w-4" />
                执行脚本
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setSelectedNodeIds(new Set())}
              >
                取消选择
              </Button>
            </div>
          </div>
        )}

        {isLoading ? (
          <div className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
          </div>
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[50px]">
                    <Checkbox
                      checked={allSelected}
                      onCheckedChange={toggleAllSelection}
                      aria-label="全选"
                      className={someSelected ? "opacity-50" : ""}
                    />
                  </TableHead>
                  <TableHead className="min-w-[150px]">节点信息</TableHead>
                  <TableHead className="min-w-[150px]">网络</TableHead>
                  <TableHead className="min-w-[100px]">最后更新</TableHead>
                  {data?.scripts.map((script) => (
                    <TableHead
                      key={script.id}
                      className="text-center min-w-[80px]"
                      title={script.name}
                    >
                      <span className="truncate block max-w-[80px]">
                        {script.name}
                      </span>
                    </TableHead>
                  ))}
                  <TableHead className="w-[50px]">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.nodes && data.nodes.length > 0 ? (
                  data.nodes.map((node) => (
                    <TableRow key={node.id} className={selectedNodeIds.has(node.id) ? "bg-muted/30" : ""}>
                      {/* Checkbox */}
                      <TableCell>
                        <Checkbox
                          checked={selectedNodeIds.has(node.id)}
                          onCheckedChange={() => toggleNodeSelection(node.id)}
                          aria-label={`选择 ${node.name}`}
                        />
                      </TableCell>
                      {/* Node info: name + country/region + tunnel count */}
                      <TableCell>
                        <div className="space-y-0.5">
                          <div className="font-medium">{node.name}</div>
                          <div className="text-xs text-muted-foreground">
                            {node.country}
                            {node.region && ` · ${node.region}`}
                          </div>
                          {node.tunnels && node.tunnels.length > 0 ? (
                            <Collapsible>
                              <CollapsibleTrigger asChild>
                                <button className="text-xs text-muted-foreground hover:text-foreground underline cursor-pointer">
                                  {node.tunnelCount} 个隧道
                                </button>
                              </CollapsibleTrigger>
                              <CollapsibleContent className="mt-1 space-y-1">
                                {node.tunnels.map((tunnel: NodeBatchMatrixTunnel) => (
                                  <div key={tunnel.id} className="text-xs font-mono text-muted-foreground">
                                    {tunnel.domain} ({tunnel.protocol}:{tunnel.port})
                                  </div>
                                ))}
                              </CollapsibleContent>
                            </Collapsible>
                          ) : (
                            <div className="text-xs text-muted-foreground">
                              0 个隧道
                            </div>
                          )}
                        </div>
                      </TableCell>

                      {/* Network: IPv4 + IPv6 */}
                      <TableCell>
                        <div className="space-y-0.5 font-mono text-sm">
                          <div>{node.ipv4}</div>
                          {node.ipv6 && (
                            <div className="text-xs text-muted-foreground truncate max-w-[200px]" title={node.ipv6}>
                              {node.ipv6}
                            </div>
                          )}
                        </div>
                      </TableCell>

                      {/* Last updated */}
                      <TableCell>
                        <span className="text-sm text-muted-foreground">
                          {formatRelativeTime(node.updatedAt)}
                        </span>
                      </TableCell>

                      {/* Task result cells */}
                      {data?.scripts.map((script) => {
                        const result = node.results[script.id.toString()];
                        return (
                          <TableCell key={script.id} className="text-center">
                            <TaskResultCell
                              result={result}
                              scriptName={script.name}
                              onViewDetail={() => {
                                if (result) {
                                  setSelectedResult({
                                    result,
                                    scriptName: script.name,
                                    nodeName: node.name,
                                  });
                                }
                              }}
                            />
                          </TableCell>
                        );
                      })}

                      {/* Actions */}
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="sm">
                              <MoreVertical className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem
                              onClick={() => window.open(`/manager/nodes/${encodeURIComponent(node.ipv4)}/terminal`, '_blank')}
                            >
                              <Terminal className="h-4 w-4 mr-2" />
                              SSH Terminal
                            </DropdownMenuItem>
                            <DropdownMenuItem asChild>
                              <Link href={`/manager/nodes/batch/tasks?nodeId=${node.id}`}>
                                查看所有任务
                              </Link>
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              className="text-destructive focus:text-destructive"
                              onClick={() => setDeleteNodeIpv4(node.ipv4)}
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              删除节点
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  ))
                ) : (
                  <TableRow>
                    <TableCell
                      colSpan={5 + (data?.scripts.length || 0)}
                      className="h-24 text-center"
                    >
                      暂无节点数据
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        )}

        {/* Delete confirmation dialog */}
        <AlertDialog
          open={!!deleteNodeIpv4}
          onOpenChange={(open) => !open && setDeleteNodeIpv4(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>确认删除节点？</AlertDialogTitle>
              <AlertDialogDescription>
                此操作将删除节点 <strong>{deleteNodeIpv4}</strong> 及其所有关联数据（隧道、负载记录等）。
                此操作无法撤销。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={isDeleting}>取消</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleDelete}
                disabled={isDeleting}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                {isDeleting ? "删除中..." : "确认删除"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        {/* Task result detail dialog */}
        <Dialog
          open={!!selectedResult}
          onOpenChange={(open) => !open && setSelectedResult(null)}
        >
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>
                {selectedResult?.scriptName} - {selectedResult?.nodeName}
              </DialogTitle>
              <DialogDescription>
                执行时间: {selectedResult && formatExecutionTime(selectedResult.result.executedAt)}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="flex items-center gap-4">
                <span className="text-sm font-medium">状态:</span>
                {selectedResult?.result.status === "success" ? (
                  <span className="flex items-center gap-1 text-green-600">
                    <CheckCircle className="h-4 w-4" />
                    成功
                  </span>
                ) : (
                  <span className="flex items-center gap-1 text-red-600">
                    <XCircle className="h-4 w-4" />
                    失败
                  </span>
                )}
                <span className="text-sm text-muted-foreground">
                  退出码: {selectedResult?.result.exitCode}
                </span>
              </div>

              {selectedResult?.result.stdout && (
                <div>
                  <div className="text-sm font-medium mb-1">标准输出:</div>
                  <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto max-h-40">
                    {selectedResult.result.stdout}
                  </pre>
                </div>
              )}

              {selectedResult?.result.stderr && (
                <div>
                  <div className="text-sm font-medium mb-1">标准错误:</div>
                  <pre className="bg-red-50 dark:bg-red-950 p-3 rounded-md text-xs overflow-x-auto max-h-40 text-red-800 dark:text-red-200">
                    {selectedResult.result.stderr}
                  </pre>
                </div>
              )}

              <div className="flex gap-2 pt-2">
                <Button variant="outline" size="sm" asChild>
                  <Link href={`/manager/nodes/batch/tasks/${selectedResult?.result.taskId}`}>
                    查看任务详情
                  </Link>
                </Button>
              </div>
            </div>
          </DialogContent>
        </Dialog>

        {/* Quick action dialog */}
        <Dialog open={quickActionDialogOpen} onOpenChange={setQuickActionDialogOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>批量执行脚本</DialogTitle>
              <DialogDescription>
                选择要在 {selectedNodeIds.size} 个节点上执行的脚本
              </DialogDescription>
            </DialogHeader>
            <div className="py-4">
              <Select
                value={selectedScriptId?.toString() || ""}
                onValueChange={(value) => setSelectedScriptId(parseInt(value))}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择脚本" />
                </SelectTrigger>
                <SelectContent>
                  {scripts.map((script) => (
                    <SelectItem key={script.id} value={script.id.toString()}>
                      {script.name}
                      {script.description && (
                        <span className="text-muted-foreground ml-2">
                          - {script.description}
                        </span>
                      )}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {scripts.length === 0 && (
                <p className="text-sm text-muted-foreground mt-2">
                  暂无可用脚本，请先
                  <Link href="/manager/nodes/batch/scripts" className="text-primary underline ml-1">
                    创建脚本
                  </Link>
                </p>
              )}
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => {
                  setQuickActionDialogOpen(false);
                  setSelectedScriptId(null);
                }}
              >
                取消
              </Button>
              <Button
                onClick={handleCreateQuickTask}
                disabled={!selectedScriptId || creatingTask}
                className="gap-2"
              >
                {creatingTask ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Play className="h-4 w-4" />
                )}
                立即执行
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}
