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
import {
  api,
  AdminNodeItem,
  ListResult,
} from "@/lib/api";
import { toast } from "sonner";
import { RefreshCw, Copy } from "lucide-react";

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

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).then(() => {
    toast.success("已复制");
  });
}

export default function NodesPage() {
  const [data, setData] = useState<ListResult<AdminNodeItem> | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const fetchData = async () => {
    setIsLoading(true);
    try {
      const response = await api.listSlaveNodes({ page: 1, pageSize: 200 });
      setData(response);
    } catch (error) {
      console.error("Failed to fetch nodes data:", error);
      toast.error("获取节点数据失败");
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  return (
    <div className="container mx-auto py-10">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-3xl font-bold">节点运维</h1>
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

      {isLoading ? (
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
        </div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[120px]">节点</TableHead>
                <TableHead className="w-[160px]">网络</TableHead>
                <TableHead>隧道</TableHead>
                <TableHead className="w-[90px]">更新时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.items && data.items.length > 0 ? (
                data.items.map((node) => (
                  <TableRow key={node.id} className="align-top">
                    {/* Node: name + country/region */}
                    <TableCell>
                      <div className="font-medium">{node.name}</div>
                      <div className="text-xs text-muted-foreground">
                        {node.country}
                        {node.region && ` · ${node.region}`}
                      </div>
                    </TableCell>

                    {/* Network: IPv4 + IPv6 */}
                    <TableCell>
                      <div className="font-mono text-sm">{node.ipv4}</div>
                      {node.ipv6 && (
                        <div className="font-mono text-xs text-muted-foreground">
                          {node.ipv6}
                        </div>
                      )}
                    </TableCell>

                    {/* Tunnels: name + truncated URL with copy */}
                    <TableCell>
                      {node.tunnels && node.tunnels.length > 0 ? (
                        <div className="space-y-2">
                          {node.tunnels.map((tunnel) => (
                            <div key={tunnel.id} className="flex items-start gap-2 group">
                              <div className="min-w-0 flex-1">
                                <div className="text-xs text-muted-foreground">{tunnel.name}</div>
                                <div
                                  className="font-mono text-xs truncate max-w-[500px] cursor-pointer hover:text-primary"
                                  title={tunnel.serverUrl ?? ''}
                                  onClick={() => tunnel.serverUrl && copyToClipboard(tunnel.serverUrl)}
                                >
                                  {tunnel.serverUrl ?? `${tunnel.domain}:${tunnel.port}`}
                                </div>
                              </div>
                              {tunnel.serverUrl && (
                                <button
                                  className="shrink-0 mt-3 opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-primary"
                                  onClick={() => copyToClipboard(tunnel.serverUrl!)}
                                >
                                  <Copy className="h-3.5 w-3.5" />
                                </button>
                              )}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </TableCell>

                    {/* Last updated */}
                    <TableCell>
                      <span className="text-sm text-muted-foreground">
                        {formatRelativeTime(node.updatedAt)}
                      </span>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={4} className="h-24 text-center">
                    暂无节点数据
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
