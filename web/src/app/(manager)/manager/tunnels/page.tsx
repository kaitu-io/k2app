"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState, useCallback } from "react";
import { api, UnauthorizedError } from "@/lib/api";
import { useAuth } from "@/contexts/AuthContext";
import { toast } from "sonner";
import { RefreshCw, AlertCircle } from "lucide-react";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Pagination } from "@/components/Pagination";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

interface SlaveNode {
  name: string;
  country: string;
  ipv4: string;
  ipv6: string;
  isAlive: boolean;
  load: number;
}

interface SlaveTunnel {
  id: number;
  domain: string;
  name: string;
  protocol: string;
  port: number;  // 隧道端口，用于 addrs 中 node_ip:tunnel_port
  serverUrl: string;
  node: SlaveNode;
}

interface PaginationInfo {
  page: number;
  pageSize: number;
  total: number;
}

export default function TunnelsPage() {
  const [tunnels, setTunnels] = useState<SlaveTunnel[]>([]);
  const [activeProtocol, setActiveProtocol] = useState<string>("all");
  const [pagination, setPagination] = useState<PaginationInfo>({
    page: 0, // Backend starts from 0
    pageSize: 50,
    total: 0
  });
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const { navigateToLogin } = useAuth();

  const fetchTunnels = useCallback(async (protocol?: string, page: number = 0) => {
    setIsLoading(true);
    setLoadError(null);
    try {
      let url = `/app/tunnels?page=${page}&pageSize=${pagination.pageSize}`;
      if (protocol && protocol !== "all") {
        url += `&protocol=${protocol}`;
      }
      const data = await api.request<{
        items: SlaveTunnel[];
        pagination: PaginationInfo;
      }>(url, {
        method: "GET",
      });
      setTunnels(data?.items || []);
      if (data?.pagination) {
        setPagination(data.pagination);
      }
    } catch (error) {
      if (error instanceof UnauthorizedError) {
        toast.error("会话已过期，请重新登录。");
        navigateToLogin();
      } else {
        // Set error state for UI display
        setLoadError(error instanceof Error ? error.message : "加载隧道列表失败");
      }
    } finally {
      setIsLoading(false);
    }
  }, [pagination.pageSize, navigateToLogin]);

  useEffect(() => {
    fetchTunnels(activeProtocol, 0); // Reset to first page when protocol changes
  }, [activeProtocol, fetchTunnels]);

  const handleRetry = () => {
    fetchTunnels(activeProtocol, pagination.page);
  };

  const handlePageChange = (newPage: number) => {
    fetchTunnels(activeProtocol, newPage - 1); // UI 显示从 1 开始，后端从 0 开始
  };

  const handleDeleteTunnel = async (tunnelId: number) => {
    if (!confirm("确定要删除这个隧道吗？")) return;
    const originalTunnels = [...tunnels];
    setTunnels(tunnels.filter((t) => t.id !== tunnelId));

    try {
      await api.request(`/app/tunnels/${tunnelId}`, { method: "DELETE" });
      toast.success("隧道删除成功");
      // 刷新当前页面
      fetchTunnels(activeProtocol, pagination.page);
    } catch (error) {
      setTunnels(originalTunnels);
      if (error instanceof UnauthorizedError) {
        toast.error("会话已过期，请重新登录。");
        navigateToLogin();
      }
    }
  };

  const totalPages = Math.ceil(pagination.total / pagination.pageSize);

  // Loading state component
  const renderLoading = () => (
    <div className="flex items-center justify-center py-20">
      <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      <span className="ml-3 text-muted-foreground">{"加载中..."}</span>
    </div>
  );

  // Error state component with retry button
  const renderError = () => (
    <Alert variant="destructive" className="my-6">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{"加载失败"}</AlertTitle>
      <AlertDescription className="flex items-center justify-between">
        <span>{loadError}</span>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRetry}
          className="ml-4"
        >
          <RefreshCw className="h-4 w-4 mr-2" />
          {"重试"}
        </Button>
      </AlertDescription>
    </Alert>
  );

  // Empty state component
  const renderEmpty = () => (
    <div className="text-center py-20 text-muted-foreground">
      <p>{"暂无隧道数据"}</p>
    </div>
  );

  const renderTable = () => (
    <>
    <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{"隧道ID"}</TableHead>
            <TableHead>{"隧道域名"}</TableHead>
            <TableHead>{"隧道名称"}</TableHead>
            <TableHead>{"协议"}</TableHead>
            <TableHead>{"端口"}</TableHead>
            <TableHead>{"连接URL"}</TableHead>
            <TableHead>{"节点名称"}</TableHead>
            <TableHead>{"国家"}</TableHead>
            <TableHead>{"IPv4"}</TableHead>
            <TableHead>{"IPv6"}</TableHead>
            <TableHead>{"节点状态"}</TableHead>
            <TableHead>{"负载"}</TableHead>
            <TableHead>{"操作"}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {tunnels.map((tunnel) => (
            <TableRow key={tunnel.id}>
              <TableCell>{tunnel.id}</TableCell>
              <TableCell>
                <span className="font-mono text-sm bg-blue-100 dark:bg-blue-900 dark:text-blue-100 px-2 py-1 rounded">
                  {tunnel.domain}
                </span>
              </TableCell>
              <TableCell>{tunnel.name}</TableCell>
              <TableCell>
                <span className="font-mono text-sm bg-gray-100 dark:bg-gray-800 dark:text-gray-100 px-2 py-1 rounded">
                  {tunnel.protocol}
                </span>
              </TableCell>
              <TableCell>{tunnel.port}</TableCell>
              <TableCell>
                {tunnel.serverUrl ? (
                  <span className="font-mono text-xs bg-gray-100 dark:bg-gray-800 dark:text-gray-100 px-2 py-1 rounded break-all max-w-xs inline-block">
                    {tunnel.serverUrl}
                  </span>
                ) : (
                  <span className="text-muted-foreground">-</span>
                )}
              </TableCell>
              <TableCell>{tunnel.node.name}</TableCell>
              <TableCell>{tunnel.node.country}</TableCell>
              <TableCell>
                <span className="font-mono text-sm">
                  {tunnel.node.ipv4 || "-"}
                </span>
              </TableCell>
              <TableCell>
                <span className="font-mono text-sm">
                  {tunnel.node.ipv6 || "-"}
                </span>
              </TableCell>
              <TableCell>
                <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                  tunnel.node.isAlive
                    ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-100'
                    : 'bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-100'
                }`}>
                  {tunnel.node.isAlive ? "在线" : "离线"}
                </span>
              </TableCell>
              <TableCell>
                <div className="flex items-center">
                  <div className="flex-1 bg-gray-200 dark:bg-gray-700 rounded-full h-2 mr-2">
                    <div
                      className="bg-blue-600 dark:bg-blue-400 h-2 rounded-full"
                      style={{ width: `${Math.min(tunnel.node.load, 100)}%` }}
                    ></div>
                  </div>
                  <span className="text-sm">{tunnel.node.load}{"%"}</span>
                </div>
              </TableCell>
              <TableCell>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleDeleteTunnel(tunnel.id)}
                >
                  {"删除"}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {/* 分页组件 */}
      <Pagination
        currentPage={pagination.page + 1} // UI 显示从 1 开始
        totalPages={totalPages}
        onPageChange={handlePageChange}
        className="mt-4"
      />
    </>
  );

  // Render content based on state
  const renderContent = () => {
    if (isLoading) {
      return renderLoading();
    }
    if (loadError) {
      return renderError();
    }
    if (tunnels.length === 0) {
      return renderEmpty();
    }
    return renderTable();
  };

  return (
    <div className="container mx-auto py-10">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-3xl font-bold">{"隧道管理"}</h1>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRetry}
          disabled={isLoading}
        >
          <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? 'animate-spin' : ''}`} />
          {"刷新"}
        </Button>
      </div>

      <Tabs value={activeProtocol} onValueChange={setActiveProtocol} className="w-full">
        <TabsList className="grid w-full max-w-md grid-cols-4 mb-6">
          <TabsTrigger value="all">{"全部"}</TabsTrigger>
          <TabsTrigger value="k2v5">{"k2v5"}</TabsTrigger>
          <TabsTrigger value="k2v4">{"k2v4"}</TabsTrigger>
          <TabsTrigger value="k2oc">{"k2oc"}</TabsTrigger>
        </TabsList>

        <TabsContent value="all" className="mt-0">
          {renderContent()}
        </TabsContent>

        <TabsContent value="k2v5" className="mt-0">
          {renderContent()}
        </TabsContent>

        <TabsContent value="k2v4" className="mt-0">
          {renderContent()}
        </TabsContent>

        <TabsContent value="k2oc" className="mt-0">
          {renderContent()}
        </TabsContent>
      </Tabs>
    </div>
  );
}
