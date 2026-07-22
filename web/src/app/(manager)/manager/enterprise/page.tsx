"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  ApiError,
  AdminNodeItem,
  EnterpriseCustomerItem,
  EnterpriseLineItem,
  EnterpriseBindingItem,
} from "@/lib/api";
import { getApiErrorMessageZh } from "@/lib/api-errors";
import { toast } from "sonner";
import { RefreshCw, Loader2, Plus, Trash2 } from "lucide-react";

const SLOTS = [1, 2, 3, 4, 5, 6, 7, 8];

export default function EnterpriseRouterPage() {
  const [customers, setCustomers] = useState<EnterpriseCustomerItem[]>([]);
  const [isLoadingCustomers, setIsLoadingCustomers] = useState(true);
  const [selectedCustomer, setSelectedCustomer] = useState<EnterpriseCustomerItem | null>(null);

  const [lines, setLines] = useState<EnterpriseLineItem[]>([]);
  const [isLoadingLines, setIsLoadingLines] = useState(false);

  const [bindings, setBindings] = useState<EnterpriseBindingItem[]>([]);
  const [isLoadingBindings, setIsLoadingBindings] = useState(false);

  const [privateNodes, setPrivateNodes] = useState<AdminNodeItem[]>([]);

  // Create customer dialog
  const [createCustOpen, setCreateCustOpen] = useState(false);
  const [custCompany, setCustCompany] = useState("");
  const [custContact, setCustContact] = useState("");
  const [custUserId, setCustUserId] = useState("");
  const [isCreatingCust, setIsCreatingCust] = useState(false);

  // Create line dialog
  const [createLineOpen, setCreateLineOpen] = useState(false);
  const [lineNodeId, setLineNodeId] = useState("");
  const [lineCountry, setLineCountry] = useState("");
  const [lineNo, setLineNo] = useState("1");
  const [isCreatingLine, setIsCreatingLine] = useState(false);

  // Binding matrix — deviceId + slot -> lineId picker
  const [bindDeviceId, setBindDeviceId] = useState("");
  const [busySlot, setBusySlot] = useState<number | null>(null);

  const fetchCustomers = async () => {
    setIsLoadingCustomers(true);
    try {
      const res = await api.listEnterpriseCustomers({ page: 1, pageSize: 200 });
      setCustomers(res.items || []);
    } catch (error) {
      console.error("Failed to fetch enterprise customers:", error);
      toast.error("获取企业客户列表失败");
    } finally {
      setIsLoadingCustomers(false);
    }
  };

  const fetchPrivateNodes = async () => {
    try {
      const res = await api.listSlaveNodes({ page: 1, pageSize: 500 });
      setPrivateNodes((res.items || []).filter((n) => n.class === "private"));
    } catch (error) {
      console.error("Failed to fetch private nodes:", error);
    }
  };

  useEffect(() => {
    fetchCustomers();
    fetchPrivateNodes();
  }, []);

  const fetchLines = async (customerId: number) => {
    setIsLoadingLines(true);
    try {
      const res = await api.listEnterpriseLines(customerId);
      setLines(res || []);
    } catch (error) {
      console.error("Failed to fetch enterprise lines:", error);
      toast.error("获取线路列表失败");
    } finally {
      setIsLoadingLines(false);
    }
  };

  const fetchBindings = async (customerId: number) => {
    setIsLoadingBindings(true);
    try {
      const res = await api.listEnterpriseBindings({ customerId });
      setBindings(res || []);
    } catch (error) {
      console.error("Failed to fetch enterprise bindings:", error);
      toast.error("获取绑定矩阵失败");
    } finally {
      setIsLoadingBindings(false);
    }
  };

  const selectCustomer = (cust: EnterpriseCustomerItem) => {
    setSelectedCustomer(cust);
    setBindDeviceId("");
    fetchLines(cust.id);
    fetchBindings(cust.id);
  };

  const handleCreateCustomer = async () => {
    const userId = parseInt(custUserId, 10);
    if (!custCompany.trim() || !userId) {
      toast.error("请填写公司名称和账号 ID");
      return;
    }
    setIsCreatingCust(true);
    try {
      await api.createEnterpriseCustomer({
        company: custCompany.trim(),
        contact: custContact.trim() || undefined,
        userId,
      });
      toast.success("企业客户已创建");
      setCreateCustOpen(false);
      setCustCompany("");
      setCustContact("");
      setCustUserId("");
      await fetchCustomers();
    } catch (error) {
      console.error("Failed to create enterprise customer:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "创建企业客户失败");
    } finally {
      setIsCreatingCust(false);
    }
  };

  const toggleCustomerStatus = async (cust: EnterpriseCustomerItem) => {
    const next = cust.status === "active" ? "suspended" : "active";
    try {
      await api.updateEnterpriseCustomer(cust.id, { status: next });
      toast.success(`客户状态已切换为「${next === "active" ? "正常" : "暂停"}」`);
      await fetchCustomers();
      if (selectedCustomer?.id === cust.id) {
        setSelectedCustomer({ ...cust, status: next });
      }
    } catch (error) {
      console.error("Failed to update enterprise customer status:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "更新客户状态失败");
    }
  };

  const handleCreateLine = async () => {
    if (!selectedCustomer) return;
    const nodeId = parseInt(lineNodeId, 10);
    const no = parseInt(lineNo, 10);
    if (!nodeId || !lineCountry.trim() || !no || no < 1) {
      toast.error("请选择节点、填写国家代码（小写两位）和序号");
      return;
    }
    setIsCreatingLine(true);
    try {
      await api.createEnterpriseLine({
        customerId: selectedCustomer.id,
        nodeId,
        countryCode: lineCountry.trim().toLowerCase(),
        lineNo: no,
      });
      toast.success("线路已创建");
      setCreateLineOpen(false);
      setLineNodeId("");
      setLineCountry("");
      setLineNo("1");
      await fetchLines(selectedCustomer.id);
    } catch (error) {
      console.error("Failed to create enterprise line:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "创建线路失败");
    } finally {
      setIsCreatingLine(false);
    }
  };

  const handleDeleteLine = async (line: EnterpriseLineItem) => {
    if (!selectedCustomer) return;
    try {
      await api.deleteEnterpriseLine(line.id);
      toast.success("线路已删除");
      await fetchLines(selectedCustomer.id);
    } catch (error) {
      console.error("Failed to delete enterprise line:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "删除线路失败（可能仍绑定在某槽位上）");
    }
  };

  const bindingForSlot = (slot: number) =>
    bindings.find((b) => b.gatewayDeviceId === parseInt(bindDeviceId, 10) && b.slot === slot);

  const handleBindSlot = async (slot: number, lineId: string) => {
    const deviceId = parseInt(bindDeviceId, 10);
    const lid = parseInt(lineId, 10);
    if (!deviceId || !lid) return;
    setBusySlot(slot);
    try {
      await api.upsertEnterpriseBinding({ gatewayDeviceId: deviceId, slot, lineId: lid });
      toast.success(`槽位 ${slot} 已绑定`);
      if (selectedCustomer) await fetchBindings(selectedCustomer.id);
    } catch (error) {
      console.error("Failed to bind enterprise slot:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "绑定失败");
    } finally {
      setBusySlot(null);
    }
  };

  const handleUnbindSlot = async (binding: EnterpriseBindingItem) => {
    setBusySlot(binding.slot);
    try {
      await api.deleteEnterpriseBinding(binding.id);
      toast.success(`槽位 ${binding.slot} 已解绑`);
      if (selectedCustomer) await fetchBindings(selectedCustomer.id);
    } catch (error) {
      console.error("Failed to unbind enterprise slot:", error);
      const code = error instanceof ApiError ? error.code : undefined;
      toast.error(code ? getApiErrorMessageZh(code) : "解绑失败");
    } finally {
      setBusySlot(null);
    }
  };

  return (
    <div className="container mx-auto py-10 space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">企业路由器</h1>
          <p className="text-muted-foreground">企业客户 · 专属线路 · 8 槽位绑定矩阵</p>
        </div>
        <Button variant="outline" onClick={fetchCustomers} disabled={isLoadingCustomers}>
          <RefreshCw className={`h-4 w-4 mr-2 ${isLoadingCustomers ? "animate-spin" : ""}`} />
          刷新
        </Button>
      </div>

      {/* 客户列表 */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-xl font-semibold">企业客户</h2>
          <Button size="sm" onClick={() => setCreateCustOpen(true)}>
            <Plus className="h-4 w-4 mr-2" />
            新建客户
          </Button>
        </div>
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>公司</TableHead>
                <TableHead>联系人</TableHead>
                <TableHead>账号 ID</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoadingCustomers ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-24 text-center">
                    加载中...
                  </TableCell>
                </TableRow>
              ) : customers.length > 0 ? (
                customers.map((cust) => (
                  <TableRow
                    key={cust.id}
                    className={`cursor-pointer ${selectedCustomer?.id === cust.id ? "bg-muted" : ""}`}
                    onClick={() => selectCustomer(cust)}
                  >
                    <TableCell className="font-medium">{cust.company}</TableCell>
                    <TableCell>{cust.contact || "-"}</TableCell>
                    <TableCell className="font-mono text-sm">{cust.userId}</TableCell>
                    <TableCell>
                      <Badge variant={cust.status === "active" ? "default" : "destructive"}>
                        {cust.status === "active" ? "正常" : "暂停"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          toggleCustomerStatus(cust);
                        }}
                      >
                        {cust.status === "active" ? "暂停" : "恢复"}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={5} className="h-24 text-center">
                    暂无企业客户
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      {selectedCustomer && (
        <>
          {/* 线路列表 */}
          <div>
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-xl font-semibold">
                线路 — {selectedCustomer.company}
              </h2>
              <Button size="sm" onClick={() => setCreateLineOpen(true)}>
                <Plus className="h-4 w-4 mr-2" />
                新建线路
              </Button>
            </div>
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>线路</TableHead>
                    <TableHead>节点</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {isLoadingLines ? (
                    <TableRow>
                      <TableCell colSpan={4} className="h-24 text-center">
                        加载中...
                      </TableCell>
                    </TableRow>
                  ) : lines.length > 0 ? (
                    lines.map((line) => (
                      <TableRow key={line.id}>
                        <TableCell className="font-mono">
                          {line.countryCode.toUpperCase()}-{line.lineNo}
                        </TableCell>
                        <TableCell>
                          {line.node ? (
                            <>
                              <div className="text-sm">{line.node.name}</div>
                              <div className="font-mono text-xs text-muted-foreground">
                                {line.node.ipv4}
                              </div>
                            </>
                          ) : (
                            <span className="text-muted-foreground">-</span>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant={line.status === "active" ? "default" : "destructive"}>
                            {line.status === "active" ? "正常" : "暂停"}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleDeleteLine(line)}
                          >
                            <Trash2 className="h-4 w-4 mr-1" />
                            删除
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={4} className="h-24 text-center">
                        暂无线路
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
          </div>

          {/* 绑定矩阵 */}
          <div>
            <h2 className="text-xl font-semibold mb-3">绑定矩阵</h2>
            <div className="flex items-center gap-2 mb-3">
              <Label htmlFor="bind-device-id" className="whitespace-nowrap">
                网关设备 ID
              </Label>
              <Input
                id="bind-device-id"
                className="w-40"
                value={bindDeviceId}
                onChange={(e) => setBindDeviceId(e.target.value)}
                placeholder="例如 12345"
              />
              <Button
                variant="outline"
                size="sm"
                disabled={!bindDeviceId}
                onClick={() => selectedCustomer && fetchBindings(selectedCustomer.id)}
              >
                查询
              </Button>
            </div>

            {isLoadingBindings ? (
              <div className="h-24 flex items-center justify-center text-muted-foreground">
                加载中...
              </div>
            ) : (
              <div className="rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-20">槽位</TableHead>
                      <TableHead>线路</TableHead>
                      <TableHead>操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {SLOTS.map((slot) => {
                      const binding = bindingForSlot(slot);
                      const busy = busySlot === slot;
                      return (
                        <TableRow key={slot}>
                          <TableCell className="font-mono">{slot}</TableCell>
                          <TableCell>
                            {binding?.line ? (
                              <span className="font-mono">
                                {binding.line.countryCode.toUpperCase()}-{binding.line.lineNo}
                              </span>
                            ) : (
                              <Select
                                disabled={!bindDeviceId || busy}
                                onValueChange={(v) => handleBindSlot(slot, v)}
                              >
                                <SelectTrigger className="w-56">
                                  <SelectValue placeholder="选择线路绑定" />
                                </SelectTrigger>
                                <SelectContent>
                                  {lines
                                    .filter((l) => l.status === "active")
                                    .map((l) => (
                                      <SelectItem key={l.id} value={String(l.id)}>
                                        {l.countryCode.toUpperCase()}-{l.lineNo}（{l.node?.name || l.nodeId}）
                                      </SelectItem>
                                    ))}
                                </SelectContent>
                              </Select>
                            )}
                          </TableCell>
                          <TableCell>
                            {binding && (
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={busy}
                                onClick={() => handleUnbindSlot(binding)}
                              >
                                {busy ? (
                                  <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                  "解绑"
                                )}
                              </Button>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        </>
      )}

      {/* 新建客户 Dialog */}
      <Dialog open={createCustOpen} onOpenChange={(open) => !isCreatingCust && setCreateCustOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建企业客户</DialogTitle>
            <DialogDescription>绑定到一个已存在的用户账号。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label htmlFor="cust-company">公司名称</Label>
              <Input id="cust-company" value={custCompany} onChange={(e) => setCustCompany(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="cust-contact">联系人（可选）</Label>
              <Input id="cust-contact" value={custContact} onChange={(e) => setCustContact(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="cust-user-id">账号 ID</Label>
              <Input
                id="cust-user-id"
                type="number"
                value={custUserId}
                onChange={(e) => setCustUserId(e.target.value)}
                placeholder="例如 12345"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateCustOpen(false)} disabled={isCreatingCust}>
              取消
            </Button>
            <Button onClick={handleCreateCustomer} disabled={isCreatingCust}>
              {isCreatingCust ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : null}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 新建线路 Dialog */}
      <Dialog open={createLineOpen} onOpenChange={(open) => !isCreatingLine && setCreateLineOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建线路</DialogTitle>
            <DialogDescription>
              为 {selectedCustomer?.company} 挂接一个专属节点作为线路。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label>专属节点</Label>
              <Select value={lineNodeId} onValueChange={setLineNodeId}>
                <SelectTrigger>
                  <SelectValue placeholder="选择节点" />
                </SelectTrigger>
                <SelectContent>
                  {privateNodes.map((n) => (
                    <SelectItem key={n.id} value={String(n.id)}>
                      {n.name}（{n.ipv4}）
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="line-country">国家代码（ISO alpha-2 小写）</Label>
              <Input
                id="line-country"
                value={lineCountry}
                onChange={(e) => setLineCountry(e.target.value)}
                placeholder="例如 ae"
                maxLength={2}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="line-no">序号</Label>
              <Input
                id="line-no"
                type="number"
                min={1}
                value={lineNo}
                onChange={(e) => setLineNo(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateLineOpen(false)} disabled={isCreatingLine}>
              取消
            </Button>
            <Button onClick={handleCreateLine} disabled={isCreatingLine}>
              {isCreatingLine ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : null}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
