"use client";

export const dynamic = "force-dynamic";

import React, { useEffect, useState, useCallback, useMemo } from "react";
import { useParams, useRouter } from "next/navigation";
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
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { toast } from "sonner";
import { api, BatchTaskDetailResponse, TaskResultItem } from "@/lib/api";
import {
  ArrowLeft,
  Loader2,
  CheckCircle,
  XCircle,
  Clock,
  LucideIcon,
  RefreshCcw,
  Download,
  Search,
  ChevronDown,
  ChevronRight,
  Copy,
  Maximize2,
  Link as LinkIcon,
} from "lucide-react";

export default function BatchTaskDetailPage() {
  const params = useParams();
  const router = useRouter();
  const [task, setTask] = useState<BatchTaskDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedNodes, setSelectedNodes] = useState<Set<number>>(new Set());
  const [retryDialogOpen, setRetryDialogOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [outputDialogOpen, setOutputDialogOpen] = useState(false);
  const [selectedOutput, setSelectedOutput] = useState<{
    title: string;
    content: string;
    type: "stdout" | "stderr";
  } | null>(null);

  const taskId = parseInt(params.id as string);

  const loadTaskDetail = useCallback(async () => {
    try {
      setLoading(true);
      const response = await api.getBatchTask(taskId);
      setTask(response);
    } catch (error) {
      toast.error("查看任务详情失败");
      console.error("Failed to load task detail:", error);
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  useEffect(() => {
    if (taskId) {
      loadTaskDetail();
      const interval = setInterval(() => {
        if (task?.status === "running" || task?.status === "pending") {
          loadTaskDetail();
        }
      }, 5000);
      return () => clearInterval(interval);
    }
  }, [taskId, task?.status, loadTaskDetail]);

  const formatDate = (timestamp: number) => {
    return new Date(timestamp).toLocaleString("zh-CN");
  };

  const formatDuration = (ms: number | null | undefined) => {
    if (!ms) return "-";
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);

    if (hours > 0) {
      return `${hours}时${minutes % 60}分${seconds % 60}秒`;
    } else if (minutes > 0) {
      return `${minutes}分${seconds % 60}秒`;
    } else {
      return `${seconds}秒`;
    }
  };

  const getStatusBadge = (status: string) => {
    const config: Record<
      string,
      { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: LucideIcon }
    > = {
      pending: { label: "等待中", variant: "secondary", icon: Clock },
      running: { label: "执行中", variant: "default", icon: Loader2 },
      success: { label: "成功", variant: "default", icon: CheckCircle },
      failed: { label: "失败", variant: "destructive", icon: XCircle },
      completed: { label: "已完成", variant: "default", icon: CheckCircle },
      paused: { label: "已暂停", variant: "outline", icon: Clock },
      skipped: { label: "已跳过", variant: "secondary", icon: Clock },
    };

    const { label, variant, icon: Icon } = config[status] || {
      label: status,
      variant: "default" as const,
      icon: Clock,
    };

    return (
      <Badge variant={variant} className="gap-1">
        <Icon className={`h-3 w-3 ${status === "running" ? "animate-spin" : ""}`} />
        {label}
      </Badge>
    );
  };

  const successCount = task?.results?.filter((r) => r.status === "success").length || 0;
  const failedCount =
    task?.results?.filter((r) => r.status === "failed" || r.status === "skipped").length || 0;

  // Filter results based on search query
  const filteredResults = useMemo(() => {
    if (!task?.results) return [];
    if (!searchQuery) return task.results;

    const query = searchQuery.toLowerCase();
    return task.results.filter(
      (r) =>
        r.nodeName.toLowerCase().includes(query) ||
        r.nodeIpv4.toLowerCase().includes(query) ||
        r.stdout?.toLowerCase().includes(query) ||
        r.stderr?.toLowerCase().includes(query) ||
        r.error?.toLowerCase().includes(query)
    );
  }, [task?.results, searchQuery]);

  // Get results for current tab
  const getTabResults = (tab: string) => {
    switch (tab) {
      case "success":
        return filteredResults.filter((r) => r.status === "success");
      case "failed":
        return filteredResults.filter((r) => r.status === "failed" || r.status === "skipped");
      default:
        return filteredResults;
    }
  };

  // Toggle node selection
  const toggleNodeSelection = (nodeId: number) => {
    const newSet = new Set(selectedNodes);
    if (newSet.has(nodeId)) {
      newSet.delete(nodeId);
    } else {
      newSet.add(nodeId);
    }
    setSelectedNodes(newSet);
  };

  // Select all failed nodes
  const selectAllFailed = () => {
    const failedNodeIds =
      task?.results
        ?.filter((r) => r.status === "failed" || r.status === "skipped")
        .map((r) => r.nodeId) || [];
    setSelectedNodes(new Set(failedNodeIds));
  };

  // Handle retry
  const handleRetry = async () => {
    if (!task) return;

    try {
      setRetrying(true);
      const nodeIds = selectedNodes.size > 0 ? Array.from(selectedNodes) : undefined;
      const response = await api.retryBatchTask(task.id, { nodeIds });
      toast.success("重试任务已创建");
      setRetryDialogOpen(false);
      // Navigate to new task
      router.push(`/manager/nodes/batch/tasks/${response.taskId}`);
    } catch (error) {
      toast.error("重试失败");
      console.error("Failed to retry task:", error);
    } finally {
      setRetrying(false);
    }
  };

  // Export results
  const exportResults = (format: "json" | "csv" | "text") => {
    if (!task?.results) return;

    let content: string;
    let filename: string;
    let mimeType: string;

    switch (format) {
      case "json":
        content = JSON.stringify(
          {
            task: {
              id: task.id,
              scriptName: task.scriptName,
              status: task.status,
              totalNodes: task.totalNodes,
              successCount,
              failedCount,
              createdAt: formatDate(task.createdAt),
              completedAt: task.completedAt ? formatDate(task.completedAt) : null,
            },
            results: task.results,
          },
          null,
          2
        );
        filename = `batch-task-${task.id}.json`;
        mimeType = "application/json";
        break;

      case "csv":
        const headers = [
          "序号",
          "节点名称",
          "IP地址",
          "状态",
          "退出码",
          "执行时长(ms)",
          "错误信息",
        ];
        const rows = task.results.map((r) => [
          r.nodeIndex + 1,
          r.nodeName,
          r.nodeIpv4,
          r.status,
          r.exitCode,
          r.duration || "",
          r.error || "",
        ]);
        content = [headers.join(","), ...rows.map((r) => r.join(","))].join("\n");
        filename = `batch-task-${task.id}.csv`;
        mimeType = "text/csv";
        break;

      case "text":
        const lines = [
          `批量任务执行报告`,
          `================`,
          `任务ID: ${task.id}`,
          `脚本: ${task.scriptName}`,
          `状态: ${task.status}`,
          `总节点: ${task.totalNodes}`,
          `成功: ${successCount}`,
          `失败: ${failedCount}`,
          `创建时间: ${formatDate(task.createdAt)}`,
          task.completedAt ? `完成时间: ${formatDate(task.completedAt)}` : "",
          ``,
          `执行结果:`,
          `--------`,
        ];

        task.results.forEach((r) => {
          lines.push(`[${r.nodeIndex + 1}] ${r.nodeName} (${r.nodeIpv4})`);
          lines.push(`    状态: ${r.status} | 退出码: ${r.exitCode}`);
          if (r.stdout) {
            lines.push(`    标准输出:`);
            r.stdout.split("\n").forEach((line) => lines.push(`      ${line}`));
          }
          if (r.stderr || r.error) {
            lines.push(`    错误输出:`);
            (r.stderr || r.error || "")
              .split("\n")
              .forEach((line) => lines.push(`      ${line}`));
          }
          lines.push(``);
        });

        content = lines.join("\n");
        filename = `batch-task-${task.id}.txt`;
        mimeType = "text/plain";
        break;

      default:
        return;
    }

    const blob = new Blob([content], { type: mimeType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    toast.success(`已导出为 ${format.toUpperCase()} 格式`);
  };

  // Copy output to clipboard
  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success("已复制到剪贴板");
  };

  // Open output in modal
  const openOutputModal = (
    title: string,
    content: string,
    type: "stdout" | "stderr"
  ) => {
    setSelectedOutput({ title, content, type });
    setOutputDialogOpen(true);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[50vh]">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!task) {
    return (
      <div className="text-center py-12">
        <h3 className="text-lg font-semibold mb-2">加载失败</h3>
        <Button onClick={() => router.back()}>返回</Button>
      </div>
    );
  }

  const canRetry =
    (task.status === "completed" || task.status === "failed") && failedCount > 0;

  return (
    <div>
      <div className="mb-6">
        <Button variant="ghost" onClick={() => router.back()} className="gap-2 mb-4">
          <ArrowLeft className="h-4 w-4" />
          返回
        </Button>
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold">任务详情</h1>
          <div className="flex gap-2">
            {canRetry && (
              <Button
                variant="default"
                onClick={() => {
                  selectAllFailed();
                  setRetryDialogOpen(true);
                }}
                className="gap-2"
              >
                <RefreshCcw className="h-4 w-4" />
                重试失败节点
              </Button>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" className="gap-2">
                  <Download className="h-4 w-4" />
                  导出
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem onClick={() => exportResults("json")}>
                  导出为 JSON
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => exportResults("csv")}>
                  导出为 CSV
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => exportResults("text")}>
                  导出为文本报告
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </div>

      <Card className="p-6 mb-6">
        <h2 className="text-xl font-semibold mb-4">{task.scriptName}</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
          <div>
            <p className="text-sm text-muted-foreground">状态</p>
            <div className="mt-1">{getStatusBadge(task.status)}</div>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">进度</p>
            <p className="text-lg font-semibold">
              {task.currentIndex} / {task.totalNodes}
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">执行结果</p>
            <p className="text-lg font-semibold">
              <span className="text-green-600">{successCount} 成功</span>
              {" / "}
              <span className="text-red-600">{failedCount} 失败</span>
            </p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">创建时间</p>
            <p className="text-sm">{formatDate(task.createdAt)}</p>
          </div>
          {task.parentTaskId && (
            <div>
              <p className="text-sm text-muted-foreground">重试自</p>
              <Button
                variant="link"
                size="sm"
                className="p-0 h-auto gap-1"
                onClick={() =>
                  router.push(`/manager/nodes/batch/tasks/${task.parentTaskId}`)
                }
              >
                <LinkIcon className="h-3 w-3" />
                任务 #{task.parentTaskId}
              </Button>
            </div>
          )}
        </div>
      </Card>

      <Card className="p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-semibold">执行结果</h2>
          <div className="flex items-center gap-2">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索节点、IP或输出..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-9 w-64"
              />
            </div>
          </div>
        </div>

        {!task.results || task.results.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground">暂无执行结果</div>
        ) : (
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList>
              <TabsTrigger value="all">全部 ({filteredResults.length})</TabsTrigger>
              <TabsTrigger value="success">
                成功 ({getTabResults("success").length})
              </TabsTrigger>
              <TabsTrigger value="failed">
                失败 ({getTabResults("failed").length})
              </TabsTrigger>
            </TabsList>

            <TabsContent value="all">
              <ResultsTable
                results={getTabResults("all")}
                formatDuration={formatDuration}
                getStatusBadge={getStatusBadge}
                selectedNodes={selectedNodes}
                onToggleSelection={toggleNodeSelection}
                showCheckbox={canRetry}
                onCopy={copyToClipboard}
                onOpenModal={openOutputModal}
                searchQuery={searchQuery}
              />
            </TabsContent>

            <TabsContent value="success">
              <ResultsTable
                results={getTabResults("success")}
                formatDuration={formatDuration}
                getStatusBadge={getStatusBadge}
                selectedNodes={selectedNodes}
                onToggleSelection={toggleNodeSelection}
                showCheckbox={false}
                onCopy={copyToClipboard}
                onOpenModal={openOutputModal}
                searchQuery={searchQuery}
              />
            </TabsContent>

            <TabsContent value="failed">
              <ResultsTable
                results={getTabResults("failed")}
                formatDuration={formatDuration}
                getStatusBadge={getStatusBadge}
                selectedNodes={selectedNodes}
                onToggleSelection={toggleNodeSelection}
                showCheckbox={canRetry}
                onCopy={copyToClipboard}
                onOpenModal={openOutputModal}
                searchQuery={searchQuery}
              />
            </TabsContent>
          </Tabs>
        )}

        {canRetry && selectedNodes.size > 0 && (
          <div className="mt-4 flex items-center justify-between p-3 bg-muted rounded-lg">
            <span className="text-sm">已选择 {selectedNodes.size} 个节点</span>
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" onClick={() => setSelectedNodes(new Set())}>
                清除选择
              </Button>
              <Button size="sm" onClick={() => setRetryDialogOpen(true)}>
                重试选中节点
              </Button>
            </div>
          </div>
        )}
      </Card>

      {/* Retry Confirmation Dialog */}
      <Dialog open={retryDialogOpen} onOpenChange={setRetryDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认重试</DialogTitle>
            <DialogDescription>
              将为以下 {selectedNodes.size || failedCount} 个节点创建新的重试任务：
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-48 overflow-y-auto">
            <ul className="text-sm space-y-1">
              {(selectedNodes.size > 0
                ? task.results?.filter((r) => selectedNodes.has(r.nodeId))
                : task.results?.filter(
                    (r) => r.status === "failed" || r.status === "skipped"
                  )
              )?.map((r) => (
                <li key={r.nodeId} className="flex items-center gap-2">
                  <XCircle className="h-4 w-4 text-red-500" />
                  {r.nodeName} ({r.nodeIpv4})
                </li>
              ))}
            </ul>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRetryDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleRetry} disabled={retrying}>
              {retrying && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              确认重试
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Output Modal */}
      <Dialog open={outputDialogOpen} onOpenChange={setOutputDialogOpen}>
        <DialogContent className="max-w-4xl max-h-[80vh]">
          <DialogHeader>
            <DialogTitle>{selectedOutput?.title}</DialogTitle>
          </DialogHeader>
          <div className="overflow-auto max-h-[60vh]">
            <pre
              className={`text-sm p-4 bg-muted rounded-lg whitespace-pre-wrap ${
                selectedOutput?.type === "stderr" ? "text-red-600" : ""
              }`}
            >
              {selectedOutput?.content || "无内容"}
            </pre>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => copyToClipboard(selectedOutput?.content || "")}
            >
              <Copy className="mr-2 h-4 w-4" />
              复制
            </Button>
            <Button onClick={() => setOutputDialogOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// Highlight search matches in text
function HighlightMatch({
  text,
  query,
  className,
}: {
  text: string;
  query: string;
  className?: string;
}) {
  if (!query || !text.toLowerCase().includes(query.toLowerCase())) {
    return <span className={className}>{text}</span>;
  }

  const parts = text.split(new RegExp(`(${query})`, "gi"));
  return (
    <span className={className}>
      {parts.map((part, i) =>
        part.toLowerCase() === query.toLowerCase() ? (
          <mark key={i} className="bg-yellow-200 dark:bg-yellow-800">
            {part}
          </mark>
        ) : (
          part
        )
      )}
    </span>
  );
}

function ResultsTable({
  results,
  formatDuration,
  getStatusBadge,
  selectedNodes,
  onToggleSelection,
  showCheckbox,
  onCopy,
  onOpenModal,
  searchQuery,
}: {
  results: TaskResultItem[];
  formatDuration: (ms: number | null | undefined) => string;
  getStatusBadge: (status: string) => React.ReactElement;
  selectedNodes: Set<number>;
  onToggleSelection: (nodeId: number) => void;
  showCheckbox: boolean;
  onCopy: (text: string) => void;
  onOpenModal: (title: string, content: string, type: "stdout" | "stderr") => void;
  searchQuery: string;
}) {
  const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set());

  const toggleRow = (nodeId: number) => {
    const newSet = new Set(expandedRows);
    if (newSet.has(nodeId)) {
      newSet.delete(nodeId);
    } else {
      newSet.add(nodeId);
    }
    setExpandedRows(newSet);
  };

  return (
    <Table>
      <TableHeader>
        <TableRow>
          {showCheckbox && <TableHead className="w-10"></TableHead>}
          <TableHead className="w-10"></TableHead>
          <TableHead>序号</TableHead>
          <TableHead>节点名称</TableHead>
          <TableHead>IP地址</TableHead>
          <TableHead>状态</TableHead>
          <TableHead>退出码</TableHead>
          <TableHead>执行时长</TableHead>
          <TableHead>操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {results.map((result) => (
          <React.Fragment key={result.nodeId}>
            <TableRow
              className={selectedNodes.has(result.nodeId) ? "bg-muted/50" : ""}
            >
              {showCheckbox && (
                <TableCell>
                  <Checkbox
                    checked={selectedNodes.has(result.nodeId)}
                    onCheckedChange={() => onToggleSelection(result.nodeId)}
                  />
                </TableCell>
              )}
              <TableCell>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 w-6 p-0"
                  onClick={() => toggleRow(result.nodeId)}
                >
                  {expandedRows.has(result.nodeId) ? (
                    <ChevronDown className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                </Button>
              </TableCell>
              <TableCell>{result.nodeIndex + 1}</TableCell>
              <TableCell>
                <HighlightMatch text={result.nodeName} query={searchQuery} />
              </TableCell>
              <TableCell className="font-mono text-sm">
                <HighlightMatch text={result.nodeIpv4} query={searchQuery} />
              </TableCell>
              <TableCell>{getStatusBadge(result.status)}</TableCell>
              <TableCell>
                <span
                  className={`font-mono ${
                    result.exitCode === 0 ? "text-green-600" : "text-red-600"
                  }`}
                >
                  {result.exitCode}
                </span>
              </TableCell>
              <TableCell>{formatDuration(result.duration)}</TableCell>
              <TableCell>
                <div className="flex gap-1">
                  {result.stdout && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2"
                      onClick={() =>
                        onOpenModal(
                          `${result.nodeName} - 标准输出`,
                          result.stdout,
                          "stdout"
                        )
                      }
                    >
                      <Maximize2 className="h-3 w-3" />
                    </Button>
                  )}
                </div>
              </TableCell>
            </TableRow>
            {expandedRows.has(result.nodeId) && (
              <TableRow>
                <TableCell
                  colSpan={showCheckbox ? 9 : 8}
                  className="bg-muted/30 p-4"
                >
                  <div className="space-y-4">
                    {result.stdout && (
                      <div>
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-sm font-medium">标准输出</span>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => onCopy(result.stdout)}
                          >
                            <Copy className="h-3 w-3 mr-1" />
                            复制
                          </Button>
                        </div>
                        <pre className="text-xs p-3 bg-background rounded border max-h-48 overflow-auto whitespace-pre-wrap">
                          <HighlightMatch text={result.stdout} query={searchQuery} />
                        </pre>
                      </div>
                    )}
                    {(result.stderr || result.error) && (
                      <div>
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-sm font-medium text-red-600">
                            错误输出
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => onCopy(result.stderr || result.error || "")}
                          >
                            <Copy className="h-3 w-3 mr-1" />
                            复制
                          </Button>
                        </div>
                        <pre className="text-xs p-3 bg-background rounded border max-h-48 overflow-auto whitespace-pre-wrap text-red-600">
                          <HighlightMatch
                            text={result.stderr || result.error || ""}
                            query={searchQuery}
                          />
                        </pre>
                      </div>
                    )}
                    {!result.stdout && !result.stderr && !result.error && (
                      <div className="text-sm text-muted-foreground">无输出内容</div>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            )}
          </React.Fragment>
        ))}
      </TableBody>
    </Table>
  );
}
