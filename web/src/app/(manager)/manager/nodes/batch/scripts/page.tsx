"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useRef } from "react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { api, BatchScriptResponse, BatchScriptDetailResponse, BatchScriptVersionResponse, SlaveNode, TestBatchScriptResponse } from "@/lib/api";
import { Plus, Pencil, Trash2, FileText, Loader2, ArrowLeft, Shield, Upload, Download, History, Play, RotateCcw, CheckCircle, XCircle, Clock } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import Link from "next/link";

export default function BatchScriptsPage() {
  const [scripts, setScripts] = useState<BatchScriptResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedScript, setSelectedScript] = useState<BatchScriptDetailResponse | null>(null);
  const [scriptToDelete, setScriptToDelete] = useState<number | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [formData, setFormData] = useState({
    name: "",
    description: "",
    content: "",
    fileName: "",
    executeWithSudo: false,
  });

  // Version history state
  const [versionDialogOpen, setVersionDialogOpen] = useState(false);
  const [versions, setVersions] = useState<BatchScriptVersionResponse[]>([]);
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [selectedVersionScript, setSelectedVersionScript] = useState<BatchScriptResponse | null>(null);
  const [versionContent, setVersionContent] = useState<string | null>(null);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(null);
  const [restoringVersion, setRestoringVersion] = useState(false);

  // Test script state
  const [testDialogOpen, setTestDialogOpen] = useState(false);
  const [testScript, setTestScript] = useState<BatchScriptResponse | null>(null);
  const [nodes, setNodes] = useState<SlaveNode[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<number | null>(null);
  const [testResult, setTestResult] = useState<TestBatchScriptResponse | null>(null);
  const [testing, setTesting] = useState(false);

  const createFileInputRef = useRef<HTMLInputElement>(null);
  const editFileInputRef = useRef<HTMLInputElement>(null);

  const loadScripts = async () => {
    try {
      setLoading(true);
      const response = await api.listBatchScripts({ page: 1, pageSize: 100 });
      setScripts(response.items || []);
    } catch (error) {
      toast.error("加载脚本列表失败");
      console.error("Failed to load scripts:", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadScripts();
    loadNodes();
  }, []);

  const loadNodes = async () => {
    try {
      const response = await api.listSlaveNodes({ page: 1, pageSize: 100 });
      setNodes(response.items || []);
    } catch (error) {
      console.error("Failed to load nodes:", error);
    }
  };

  const handleShowVersions = async (script: BatchScriptResponse) => {
    setSelectedVersionScript(script);
    setVersionDialogOpen(true);
    setVersionsLoading(true);
    setVersionContent(null);
    setSelectedVersion(null);
    try {
      const response = await api.getBatchScriptVersions(script.id);
      setVersions(response.items || []);
    } catch (error) {
      toast.error("加载版本历史失败");
      console.error("Failed to load versions:", error);
    } finally {
      setVersionsLoading(false);
    }
  };

  const handleViewVersion = async (version: number) => {
    if (!selectedVersionScript) return;
    setSelectedVersion(version);
    try {
      const detail = await api.getBatchScriptVersionDetail(selectedVersionScript.id, version);
      setVersionContent(detail.content);
    } catch (error) {
      toast.error("加载版本内容失败");
      console.error("Failed to load version content:", error);
    }
  };

  const handleRestoreVersion = async () => {
    if (!selectedVersionScript || !selectedVersion) return;
    try {
      setRestoringVersion(true);
      await api.restoreBatchScriptVersion(selectedVersionScript.id, selectedVersion);
      toast.success("版本恢复成功");
      setVersionDialogOpen(false);
      loadScripts();
    } catch (error) {
      toast.error("版本恢复失败");
      console.error("Failed to restore version:", error);
    } finally {
      setRestoringVersion(false);
    }
  };

  const handleShowTest = (script: BatchScriptResponse) => {
    setTestScript(script);
    setTestDialogOpen(true);
    setTestResult(null);
    setSelectedNodeId(null);
  };

  const handleRunTest = async () => {
    if (!testScript || !selectedNodeId) return;
    try {
      setTesting(true);
      setTestResult(null);
      const result = await api.testBatchScript(testScript.id, { nodeId: selectedNodeId });
      setTestResult(result);
    } catch (error) {
      toast.error("测试执行失败");
      console.error("Failed to run test:", error);
    } finally {
      setTesting(false);
    }
  };

  const handleCreate = async () => {
    if (!formData.name.trim()) {
      toast.error("请输入脚本名称");
      return;
    }
    if (!formData.content.trim()) {
      toast.error("请上传脚本文件");
      return;
    }

    try {
      setSubmitting(true);
      await api.createBatchScript({
        name: formData.name,
        description: formData.description,
        content: formData.content,
        executeWithSudo: formData.executeWithSudo,
      });
      toast.success("脚本创建成功");
      setCreateDialogOpen(false);
      setFormData({ name: "", description: "", content: "", fileName: "", executeWithSudo: false });
      if (createFileInputRef.current) {
        createFileInputRef.current.value = "";
      }
      loadScripts();
    } catch (error) {
      toast.error("脚本创建失败");
      console.error("Failed to create script:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const [editFormData, setEditFormData] = useState({
    name: "",
    description: "",
    content: "",
    fileName: "",
    executeWithSudo: false,
  });

  const handleEdit = async (id: number) => {
    try {
      const script = await api.getBatchScript(id);
      setSelectedScript(script);
      setEditFormData({
        name: script.name,
        description: script.description,
        content: script.content,
        fileName: "",
        executeWithSudo: script.executeWithSudo,
      });
      setEditDialogOpen(true);
    } catch (error) {
      toast.error("加载脚本详情失败");
      console.error("Failed to load script:", error);
    }
  };

  const handleUpdate = async () => {
    if (!selectedScript) return;
    if (!editFormData.name.trim()) {
      toast.error("请输入脚本名称");
      return;
    }
    if (!editFormData.content.trim()) {
      toast.error("请上传脚本文件");
      return;
    }

    try {
      setSubmitting(true);
      await api.updateBatchScript(selectedScript.id, {
        name: editFormData.name,
        description: editFormData.description,
        content: editFormData.content,
        executeWithSudo: editFormData.executeWithSudo,
      });
      toast.success("脚本更新成功");
      setEditDialogOpen(false);
      setSelectedScript(null);
      if (editFileInputRef.current) {
        editFileInputRef.current.value = "";
      }
      loadScripts();
    } catch (error) {
      toast.error("脚本更新失败");
      console.error("Failed to update script:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!scriptToDelete) return;

    try {
      setSubmitting(true);
      await api.deleteBatchScript(scriptToDelete);
      toast.success("脚本删除成功");
      setDeleteDialogOpen(false);
      setScriptToDelete(null);
      loadScripts();
    } catch (error) {
      toast.error("脚本删除失败");
      console.error("Failed to delete script:", error);
    } finally {
      setSubmitting(false);
    }
  };

  const handleFileChange = (
    e: React.ChangeEvent<HTMLInputElement>,
    setData: typeof setFormData | typeof setEditFormData
  ) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (event) => {
      const content = event.target?.result as string;
      setData((prev) => ({
        ...prev,
        content,
        fileName: file.name,
      }));
    };
    reader.onerror = () => {
      toast.error("文件读取失败");
    };
    reader.readAsText(file);
  };

  const handleDownload = (content: string, name: string) => {
    const blob = new Blob([content], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name}.sh`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const formatDate = (timestamp: number) => {
    return new Date(timestamp).toLocaleString("zh-CN");
  };

  return (
    <TooltipProvider>
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
            <h1 className="text-3xl font-bold">批量脚本管理</h1>
            <p className="text-muted-foreground mt-1">管理批量执行脚本</p>
          </div>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          创建脚本
        </Button>
      </div>

      <Card className="p-6">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        ) : scripts.length === 0 ? (
          <div className="text-center py-12">
            <FileText className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="text-lg font-semibold mb-2">暂无脚本</h3>
            <p className="text-muted-foreground mb-4">您还没有创建任何脚本</p>
            <Button onClick={() => setCreateDialogOpen(true)} variant="outline">
              创建第一个脚本
            </Button>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>描述</TableHead>
                <TableHead>Sudo</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {scripts.map((script) => (
                <TableRow key={script.id}>
                  <TableCell className="font-medium">{script.name}</TableCell>
                  <TableCell className="max-w-md truncate">{script.description || "-"}</TableCell>
                  <TableCell>
                    {script.executeWithSudo && (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Shield className="h-4 w-4 text-amber-500" />
                        </TooltipTrigger>
                        <TooltipContent>使用 sudo 执行</TooltipContent>
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell>{formatDate(script.createdAt)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => handleShowTest(script)}
                          >
                            <Play className="h-4 w-4" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>测试脚本</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => handleShowVersions(script)}
                          >
                            <History className="h-4 w-4" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>版本历史</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => handleEdit(script.id)}
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>编辑</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => {
                              setScriptToDelete(script.id);
                              setDeleteDialogOpen(true);
                            }}
                            className="text-destructive hover:text-destructive"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>删除</TooltipContent>
                      </Tooltip>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>创建脚本</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div>
              <Label htmlFor="name">脚本名称</Label>
              <Input
                id="name"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="请输入脚本名称"
              />
            </div>
            <div>
              <Label htmlFor="description">描述（可选）</Label>
              <Input
                id="description"
                value={formData.description}
                onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                placeholder="请输入脚本描述"
              />
            </div>
            <div>
              <Label htmlFor="content">脚本文件</Label>
              <div className="mt-2 flex items-center gap-4">
                <input
                  ref={createFileInputRef}
                  type="file"
                  id="content"
                  accept=".sh,.bash"
                  onChange={(e) => handleFileChange(e, setFormData)}
                  className="hidden"
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => createFileInputRef.current?.click()}
                  className="gap-2"
                >
                  <Upload className="h-4 w-4" />
                  选择文件
                </Button>
                {formData.fileName && (
                  <span className="text-sm text-muted-foreground">
                    {formData.fileName}
                  </span>
                )}
              </div>
              {formData.content && (
                <div className="mt-3 flex items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDownload(formData.content, formData.name || "script")}
                    className="gap-2 text-muted-foreground"
                  >
                    <Download className="h-4 w-4" />
                    下载验证
                  </Button>
                  <span className="text-xs text-muted-foreground">
                    ({formData.content.length} 字节)
                  </span>
                </div>
              )}
              <p className="text-xs text-muted-foreground mt-2">
                支持 .sh 和 .bash 文件
              </p>
            </div>
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <Label htmlFor="executeWithSudo">使用 sudo 执行</Label>
                <p className="text-xs text-muted-foreground">
                  以特权用户身份执行脚本
                </p>
              </div>
              <Switch
                id="executeWithSudo"
                checked={formData.executeWithSudo}
                onCheckedChange={(checked) => setFormData({ ...formData, executeWithSudo: checked })}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleCreate} disabled={submitting || !formData.content}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>编辑脚本</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div>
              <Label htmlFor="edit-name">脚本名称</Label>
              <Input
                id="edit-name"
                value={editFormData.name}
                onChange={(e) => setEditFormData({ ...editFormData, name: e.target.value })}
                placeholder="请输入脚本名称"
              />
            </div>
            <div>
              <Label htmlFor="edit-description">描述（可选）</Label>
              <Input
                id="edit-description"
                value={editFormData.description}
                onChange={(e) => setEditFormData({ ...editFormData, description: e.target.value })}
                placeholder="请输入脚本描述"
              />
            </div>
            <div>
              <Label htmlFor="edit-content">脚本文件</Label>
              <div className="mt-2 flex items-center gap-4">
                <input
                  ref={editFileInputRef}
                  type="file"
                  id="edit-content"
                  accept=".sh,.bash"
                  onChange={(e) => handleFileChange(e, setEditFormData)}
                  className="hidden"
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => editFileInputRef.current?.click()}
                  className="gap-2"
                >
                  <Upload className="h-4 w-4" />
                  {editFormData.content ? "替换文件" : "选择文件"}
                </Button>
                {editFormData.fileName && (
                  <span className="text-sm text-muted-foreground">
                    {editFormData.fileName}
                  </span>
                )}
              </div>
              {editFormData.content && (
                <div className="mt-3 flex items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDownload(editFormData.content, editFormData.name || "script")}
                    className="gap-2 text-muted-foreground"
                  >
                    <Download className="h-4 w-4" />
                    下载验证
                  </Button>
                  <span className="text-xs text-muted-foreground">
                    ({editFormData.content.length} 字节)
                  </span>
                </div>
              )}
              <p className="text-xs text-muted-foreground mt-2">
                支持 .sh 和 .bash 文件
              </p>
            </div>
            <div className="flex items-center justify-between">
              <div className="space-y-0.5">
                <Label htmlFor="edit-executeWithSudo">使用 sudo 执行</Label>
                <p className="text-xs text-muted-foreground">
                  以特权用户身份执行脚本
                </p>
              </div>
              <Switch
                id="edit-executeWithSudo"
                checked={editFormData.executeWithSudo}
                onCheckedChange={(checked) => setEditFormData({ ...editFormData, executeWithSudo: checked })}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              取消
            </Button>
            <Button onClick={handleUpdate} disabled={submitting || !editFormData.content}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除脚本</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除该脚本吗？删除后将无法恢复。
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

      {/* Version History Dialog */}
      <Dialog open={versionDialogOpen} onOpenChange={setVersionDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh]">
          <DialogHeader>
            <DialogTitle>版本历史 - {selectedVersionScript?.name}</DialogTitle>
          </DialogHeader>
          {versionsLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : versions.length === 0 ? (
            <div className="text-center py-12">
              <History className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
              <h3 className="text-lg font-semibold mb-2">暂无版本历史</h3>
              <p className="text-muted-foreground">脚本尚未更新过</p>
            </div>
          ) : (
            <div className="grid grid-cols-3 gap-4">
              <div className="col-span-1 border-r pr-4">
                <ScrollArea className="h-[400px]">
                  <div className="space-y-2">
                    {versions.map((v) => (
                      <div
                        key={v.version}
                        onClick={() => handleViewVersion(v.version)}
                        className={`p-3 rounded-lg cursor-pointer transition-colors ${
                          selectedVersion === v.version
                            ? "bg-primary text-primary-foreground"
                            : "hover:bg-muted"
                        }`}
                      >
                        <div className="flex items-center justify-between">
                          <span className="font-medium">v{v.version}</span>
                          {selectedVersion === v.version && (
                            <Badge variant="secondary" className="text-xs">
                              已选
                            </Badge>
                          )}
                        </div>
                        <p className="text-xs opacity-70 mt-1">
                          {formatDate(v.createdAt)}
                        </p>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </div>
              <div className="col-span-2">
                {versionContent ? (
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-muted-foreground">
                        版本 {selectedVersion} 内容预览
                      </span>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            handleDownload(
                              versionContent,
                              `${selectedVersionScript?.name}_v${selectedVersion}`
                            )
                          }
                          className="gap-1"
                        >
                          <Download className="h-3 w-3" />
                          下载
                        </Button>
                        <Button
                          size="sm"
                          onClick={handleRestoreVersion}
                          disabled={restoringVersion}
                          className="gap-1"
                        >
                          {restoringVersion ? (
                            <Loader2 className="h-3 w-3 animate-spin" />
                          ) : (
                            <RotateCcw className="h-3 w-3" />
                          )}
                          恢复此版本
                        </Button>
                      </div>
                    </div>
                    <ScrollArea className="h-[360px] border rounded-lg">
                      <pre className="p-4 text-xs font-mono whitespace-pre-wrap">
                        {versionContent}
                      </pre>
                    </ScrollArea>
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-[400px] text-muted-foreground">
                    选择左侧版本查看内容
                  </div>
                )}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Test Script Dialog */}
      <Dialog open={testDialogOpen} onOpenChange={setTestDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>测试脚本 - {testScript?.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>选择测试节点</Label>
              <Select
                value={selectedNodeId?.toString() || ""}
                onValueChange={(value) => setSelectedNodeId(parseInt(value))}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择一个节点进行测试" />
                </SelectTrigger>
                <SelectContent>
                  {nodes.map((node) => (
                    <SelectItem key={node.id} value={node.id.toString()}>
                      {node.name} ({node.ipv4})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                脚本将在选定节点上同步执行，用于验证脚本正确性
              </p>
            </div>

            {testResult && (
              <div className="space-y-3 border rounded-lg p-4 bg-muted/30">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {testResult.exitCode === 0 ? (
                      <CheckCircle className="h-5 w-5 text-green-500" />
                    ) : (
                      <XCircle className="h-5 w-5 text-red-500" />
                    )}
                    <span className="font-medium">
                      {testResult.exitCode === 0 ? "执行成功" : "执行失败"}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Clock className="h-4 w-4" />
                    {(testResult.duration / 1000).toFixed(2)}s
                  </div>
                </div>

                {testResult.error && (
                  <div className="text-sm text-red-500 bg-red-50 dark:bg-red-950/30 p-2 rounded">
                    {testResult.error}
                  </div>
                )}

                {testResult.stdout && (
                  <div>
                    <Label className="text-xs text-muted-foreground">标准输出</Label>
                    <ScrollArea className="h-[120px] mt-1 border rounded bg-background">
                      <pre className="p-2 text-xs font-mono whitespace-pre-wrap">
                        {testResult.stdout}
                      </pre>
                    </ScrollArea>
                  </div>
                )}

                {testResult.stderr && (
                  <div>
                    <Label className="text-xs text-muted-foreground">错误输出</Label>
                    <ScrollArea className="h-[120px] mt-1 border rounded bg-background">
                      <pre className="p-2 text-xs font-mono whitespace-pre-wrap text-red-500">
                        {testResult.stderr}
                      </pre>
                    </ScrollArea>
                  </div>
                )}

                <div className="text-xs text-muted-foreground">
                  退出码: {testResult.exitCode}
                </div>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setTestDialogOpen(false)}>
              关闭
            </Button>
            <Button
              onClick={handleRunTest}
              disabled={!selectedNodeId || testing}
              className="gap-2"
            >
              {testing ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Play className="h-4 w-4" />
              )}
              {testing ? "执行中..." : "执行测试"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
    </TooltipProvider>
  );
}
