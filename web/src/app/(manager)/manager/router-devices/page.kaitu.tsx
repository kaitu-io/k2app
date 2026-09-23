"use client";

export const dynamic = "force-dynamic";

import { useCallback, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { toast } from "sonner";
import { Loader2, RefreshCw } from "lucide-react";
import { api, type AdminRouterDeviceItem } from "@/lib/api";
import { Pagination } from "@/components/Pagination";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const PAGE_SIZE = 50;
const UDID_TRUNCATE_LEN = 12;

function formatDate(ts: number | undefined): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

// 相对时间：与 node-operations 页 formatAge 同粒度（秒/分钟/小时/天）。
function formatRelative(ts: number): string {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - ts));
  if (seconds < 60) return `${seconds} 秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  return `${days} 天前`;
}

function truncateUdid(udid: string): string {
  if (udid.length <= UDID_TRUNCATE_LEN) return udid;
  return `${udid.slice(0, UDID_TRUNCATE_LEN)}…`;
}

export default function RouterDevicesPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const page = Math.max(1, Number(searchParams.get("page") || "1"));
  const userIdParam = Number(searchParams.get("userId") || "0") || undefined;

  const [items, setItems] = useState<AdminRouterDeviceItem[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);

  const [userIdInput, setUserIdInput] = useState(userIdParam ? String(userIdParam) : "");
  useEffect(() => {
    setUserIdInput(userIdParam ? String(userIdParam) : "");
  }, [userIdParam]);

  const setQuery = useCallback(
    (patch: Record<string, string | number | undefined>) => {
      const next = new URLSearchParams(searchParams.toString());
      for (const [k, v] of Object.entries(patch)) {
        if (v === undefined || v === "" || v === "all") next.delete(k);
        else next.set(k, String(v));
      }
      router.push(`/manager/router-devices?${next.toString()}`);
    },
    [router, searchParams]
  );

  const load = useCallback(async () => {
    setIsLoading(true);
    try {
      const res = await api.listRouterDevices({ page, pageSize: PAGE_SIZE, userId: userIdParam });
      setItems(res.items || []);
      setTotal(res.pagination?.total ?? (res.items || []).length);
    } catch {
      toast.error("获取路由器设备失败");
    } finally {
      setIsLoading(false);
    }
  }, [page, userIdParam]);

  useEffect(() => {
    load();
  }, [load]);

  const commitUserId = () => {
    const trimmed = userIdInput.trim();
    const num = trimmed ? Number(trimmed) : undefined;
    setQuery({ userId: num && num > 0 ? num : undefined, page: 1 });
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <TooltipProvider>
      <div className="container mx-auto py-10">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-3xl font-bold">路由器设备</h1>
            <p className="text-muted-foreground">
              吊销并重铸凭证请在路由器订单台账中使用「代铸凭证」，新凭证会自动替换旧设备
            </p>
          </div>
          <Button onClick={load} disabled={isLoading}>
            {isLoading ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <RefreshCw className="h-4 w-4 mr-2" />}
            刷新
          </Button>
        </div>

        {/* 筛选 */}
        <div className="flex items-center gap-2 mb-4">
          <Label htmlFor="filter-user-id" className="text-sm text-muted-foreground whitespace-nowrap">
            用户 ID
          </Label>
          <Input
            id="filter-user-id"
            className="w-[140px]"
            value={userIdInput}
            onChange={(e) => setUserIdInput(e.target.value)}
            onBlur={commitUserId}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitUserId();
            }}
            placeholder="例如 1234"
          />
        </div>

        {/* 表格 */}
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>设备 ID</TableHead>
                <TableHead>用户</TableHead>
                <TableHead>UDID</TableHead>
                <TableHead>k2r 版本</TableHead>
                <TableHead>架构</TableHead>
                <TableHead>最近在线</TableHead>
                <TableHead>在线状态</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length > 0 ? (
                items.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="font-mono">{item.id}</TableCell>
                    <TableCell>
                      <div>{item.email}</div>
                      <div className="text-xs text-muted-foreground">{item.userId}</div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="cursor-default">{truncateUdid(item.udid)}</span>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p className="max-w-xs break-all">{item.udid}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell className="text-sm">{item.appVersion || "—"}</TableCell>
                    <TableCell className="text-sm">{item.appArch || "—"}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {item.lastSeenAt ? (
                        <>
                          <div>{formatDate(item.lastSeenAt)}</div>
                          <div className="text-xs">{formatRelative(item.lastSeenAt)}</div>
                        </>
                      ) : (
                        <span>从未连接</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5">
                        <span
                          className={`inline-block h-2 w-2 rounded-full ${
                            item.online ? "bg-green-500" : "bg-gray-400"
                          }`}
                        />
                        {item.online ? "在线" : "离线"}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Button variant="outline" size="sm" asChild>
                        <Link href={`/manager/router-fulfillments?userId=${item.userId}`}>查看台账</Link>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={8} className="h-24 text-center">
                    暂无路由器设备
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>

        {totalPages > 1 && (
          <Pagination
            currentPage={page}
            totalPages={totalPages}
            onPageChange={(p) => setQuery({ page: p })}
            className="mt-4"
          />
        )}
      </div>
    </TooltipProvider>
  );
}
