"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
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
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { api, ChatConversation, ChatMessage } from "@/lib/api";
import { BrandBadge, useManagerBrand } from "@/components/manager/brand";
import { toast } from "sonner";
import { Eye, ExternalLink } from "lucide-react";

const PAGE_SIZE = 50;
const BASE_PATH = "/manager/conversations";

/** 时间戳按 unix 秒处理（与工单等 admin 接口一致）；改格式只改这里。 */
const formatChatTime = (ts: number) => {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
};

type StatusVariant = "default" | "secondary" | "destructive" | "outline";

/** 与 Slack 总览一致的状态语义。 */
function statusOf(c: ChatConversation): { label: string; variant: StatusVariant } {
  if (c.status === "closed") return { label: "已关闭", variant: "secondary" };
  if (c.handler === "ai") return { label: "AI 接待中", variant: "outline" };
  if (c.lastMessageBy !== "staff") return { label: "等待人工", variant: "destructive" };
  return { label: "已回复待访客", variant: "default" };
}

function SlackLink({ url }: { url: string }) {
  if (!url.startsWith("https://")) return null;
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
    >
      <ExternalLink className="h-3 w-3" />
      {"在 Slack 打开"}
    </a>
  );
}

function MessageBubble({ m }: { m: ChatMessage }) {
  if (m.kind === "event" || m.senderType === "system") {
    return (
      <div data-kind={m.kind} className="text-center text-xs text-muted-foreground">
        {m.content}
        <span className="ml-2">{formatChatTime(m.createdAt)}</span>
      </div>
    );
  }
  const isNote = m.kind === "note";
  const fromVisitor = m.senderType === "visitor";
  const sender = fromVisitor ? "访客" : m.senderType === "ai" ? "AI" : m.senderName || "客服";
  return (
    <div data-kind={m.kind} className={`flex ${fromVisitor ? "justify-start" : "justify-end"}`}>
      <div
        className={`max-w-[80%] rounded-lg px-3 py-2 text-sm ${
          isNote
            ? "bg-amber-100 text-amber-950 border border-amber-300 dark:bg-amber-950/40 dark:text-amber-100"
            : fromVisitor
              ? "bg-muted"
              : "bg-primary/10"
        }`}
      >
        <div className="mb-1 flex items-center gap-2 text-xs text-muted-foreground">
          <span>{sender}</span>
          {isNote && <Badge variant="outline">{"内部备注"}</Badge>}
          <span>{formatChatTime(m.createdAt)}</span>
        </div>
        {m.kind === "image" ? (
          // 访客提供的 URL 不可信：不渲染图片（避免管理员浏览器被动请求），只以纯文本展示
          <div>
            <div className="text-xs text-muted-foreground">{"[图片]"}</div>
            <div className="break-all text-xs">{m.content}</div>
          </div>
        ) : (
          <div className="whitespace-pre-wrap break-words">{m.content}</div>
        )}
      </div>
    </div>
  );
}

export default function ConversationsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { brandParam } = useManagerBrand();

  const [data, setData] = useState<ChatConversation[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [refreshKey, setRefreshKey] = useState(0);

  const [page, setPage] = useState(0);
  const [status, setStatus] = useState("");
  const [handler, setHandler] = useState("");
  const [emailInput, setEmailInput] = useState("");
  const [email, setEmail] = useState("");
  const emailRef = useRef("");

  // 全局品牌切换后回到第一页（否则可能请求越界页）；在渲染期调整，避免多发一次越界请求
  const [prevBrand, setPrevBrand] = useState(brandParam);
  if (prevBrand !== brandParam) {
    setPrevBrand(brandParam);
    setPage(0);
  }

  // 详情：以 URL 的 ?c= 为准（Slack 深链），点行时本地先开、同步写回 URL
  const paramUuid = searchParams.get("c") || "";
  const [openUuid, setOpenUuid] = useState(paramUuid);
  const [detail, setDetail] = useState<{ conversation: ChatConversation; messages: ChatMessage[] } | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);
  const [isClosing, setIsClosing] = useState(false);
  const [detailError, setDetailError] = useState(false);
  const [detailTry, setDetailTry] = useState(0);

  useEffect(() => {
    setOpenUuid(paramUuid);
  }, [paramUuid]);

  // 邮箱输入防抖
  useEffect(() => {
    const t = setTimeout(() => {
      const next = emailInput.trim();
      if (next !== emailRef.current) {
        emailRef.current = next;
        setEmail(next);
        setPage(0);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [emailInput]);

  useEffect(() => {
    let cancelled = false;
    const fetchList = async () => {
      setIsLoading(true);
      try {
        const res = await api.getChatConversations({
          page,
          pageSize: PAGE_SIZE,
          status: (status || undefined) as "open" | "closed" | undefined,
          handler: (handler || undefined) as "ai" | "human" | undefined,
          email: email || undefined,
          brand: brandParam,
        });
        if (cancelled) return;
        setData(res.items || []);
        if (res.pagination) {
          setTotal(res.pagination.total);
          setPageCount(Math.ceil(res.pagination.total / res.pagination.pageSize));
        }
      } catch (error) {
        if (cancelled) return;
        console.error("Failed to fetch conversations:", error);
        toast.error("加载会话失败");
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    };
    fetchList();
    return () => {
      cancelled = true;
    };
  }, [page, status, handler, email, brandParam, refreshKey]);

  useEffect(() => {
    setDetail(null);
    setDetailError(false);
    if (!openUuid) return;
    let cancelled = false;
    api
      .getChatConversation(openUuid)
      .then((res) => {
        if (!cancelled) setDetail(res);
      })
      .catch((error) => {
        if (cancelled) return;
        console.error("Failed to fetch conversation:", error);
        setDetailError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [openUuid, refreshKey, detailTry]);

  const openDetail = useCallback(
    (uuid: string) => {
      setOpenUuid(uuid);
      const params = new URLSearchParams(searchParams.toString());
      params.set("c", uuid);
      router.replace(`${BASE_PATH}?${params.toString()}`);
    },
    [router, searchParams],
  );

  const closeDetail = useCallback(() => {
    setOpenUuid("");
    const params = new URLSearchParams(searchParams.toString());
    params.delete("c");
    const qs = params.toString();
    router.replace(qs ? `${BASE_PATH}?${qs}` : BASE_PATH);
  }, [router, searchParams]);

  const handleClose = async () => {
    if (!openUuid) return;
    setIsClosing(true);
    try {
      await api.closeChatConversation(openUuid);
      toast.success("会话已关闭");
      setConfirmClose(false);
      setRefreshKey((k) => k + 1);
    } catch (error) {
      console.error("Failed to close conversation:", error);
      toast.error("操作失败");
    } finally {
      setIsClosing(false);
    }
  };

  const selectClass = "w-full p-2 border border-border bg-muted text-foreground rounded-md";
  const conv = detail?.conversation;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">{"会话记录"}</h1>
        <p className="text-muted-foreground">{"访客在线会话的只读索引（处理请在 Slack 中进行）"}</p>
      </div>

      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1">
          <label htmlFor="chat-status" className="text-sm font-medium">{"状态"}</label>
          <select
            id="chat-status"
            className={selectClass}
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPage(0);
            }}
          >
            <option value="">{"全部"}</option>
            <option value="open">{"进行中"}</option>
            <option value="closed">{"已关闭"}</option>
          </select>
        </div>
        <div className="flex-1">
          <label htmlFor="chat-handler" className="text-sm font-medium">{"处理方"}</label>
          <select
            id="chat-handler"
            className={selectClass}
            value={handler}
            onChange={(e) => {
              setHandler(e.target.value);
              setPage(0);
            }}
          >
            <option value="">{"全部"}</option>
            <option value="ai">{"AI"}</option>
            <option value="human">{"人工"}</option>
          </select>
        </div>
        <div className="flex-1">
          <label htmlFor="chat-email" className="text-sm font-medium">{"邮箱"}</label>
          <Input
            id="chat-email"
            placeholder="搜索邮箱"
            value={emailInput}
            onChange={(e) => setEmailInput(e.target.value)}
          />
        </div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{"品牌"}</TableHead>
              <TableHead>{"访客"}</TableHead>
              <TableHead>{"状态"}</TableHead>
              <TableHead>{"入口页"}</TableHead>
              <TableHead>{"最后消息"}</TableHead>
              <TableHead>{"创建时间"}</TableHead>
              <TableHead>{"操作"}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={7} className="h-24 text-center">
                  <div className="flex items-center justify-center">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
                  </div>
                </TableCell>
              </TableRow>
            ) : data.length ? (
              data.map((c) => {
                const st = statusOf(c);
                return (
                  <TableRow key={c.uuid}>
                    <TableCell><BrandBadge brand={c.brand} /></TableCell>
                    <TableCell>
                      {c.email || (
                        <span className="text-muted-foreground">
                          {c.subjectKind === "guest" ? "匿名访客" : `用户 ${c.subjectId}`}
                        </span>
                      )}
                    </TableCell>
                    <TableCell><Badge variant={st.variant}>{st.label}</Badge></TableCell>
                    <TableCell><code className="text-xs bg-muted px-1 py-0.5 rounded">{c.entryPath || "-"}</code></TableCell>
                    <TableCell>{formatChatTime(c.lastMessageAt)}</TableCell>
                    <TableCell>{formatChatTime(c.createdAt)}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="ghost" size="sm" onClick={() => openDetail(c.uuid)} title="查看详情">
                          <Eye className="h-4 w-4" />
                        </Button>
                        <SlackLink url={c.slackPermalink} />
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={7} className="h-24 text-center">{"无会话数据"}</TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">{"总计: "}{total}</span>
        <Button variant="outline" size="sm" onClick={() => setPage((p) => p - 1)} disabled={page === 0}>
          {"上一页"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => setPage((p) => p + 1)} disabled={page >= pageCount - 1}>
          {"下一页"}
        </Button>
      </div>

      <Dialog open={!!openUuid} onOpenChange={(open) => { if (!open) closeDetail(); }}>
        <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{"会话详情"}</DialogTitle>
            <DialogDescription>
              {conv ? `${conv.email || (conv.subjectKind === "guest" ? "匿名访客" : `用户 ${conv.subjectId}`)} · 创建于 ${formatChatTime(conv.createdAt)}` : detailError ? "" : "加载中…"}
            </DialogDescription>
          </DialogHeader>
          {detailError && (
            <div className="space-y-2 text-sm">
              <p className="text-destructive">{"加载会话详情失败"}</p>
              <Button variant="outline" size="sm" onClick={() => setDetailTry((k) => k + 1)}>
                {"重试"}
              </Button>
            </div>
          )}
          {conv && detail && (
            <div className="space-y-4">
              <div className="flex flex-wrap items-center gap-3">
                <Badge variant={statusOf(conv).variant}>{statusOf(conv).label}</Badge>
                <SlackLink url={conv.slackPermalink} />
                {conv.status === "open" && (
                  <Button variant="outline" size="sm" className="ml-auto" onClick={() => setConfirmClose(true)}>
                    {"关闭会话"}
                  </Button>
                )}
              </div>
              <div className="space-y-3">
                {detail.messages.map((m) => (
                  <MessageBubble key={m.id} m={m} />
                ))}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{"关闭此会话？"}</AlertDialogTitle>
            <AlertDialogDescription>{"关闭后访客需重新发起会话，对应的 Slack 线程会被归档。"}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{"取消"}</AlertDialogCancel>
            <AlertDialogAction disabled={isClosing} onClick={(e) => { e.preventDefault(); handleClose(); }}>
              {"确认关闭"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
