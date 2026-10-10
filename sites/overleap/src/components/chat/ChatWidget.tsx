'use client';

import { useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent } from 'react';
import { useTranslations } from 'next-intl';
import { ImagePlus, MessageCircle, Send, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { isEmbeddedPage } from '@/lib/embed';
import {
  CHAT_CONTENT_MAX,
  CHAT_IMAGE_MAX_BYTES,
  CHAT_IMAGE_TYPES,
  ChatError,
  createChatClient,
  type ChatClient,
  type ChatConversation,
  type ChatMessage,
  type SessionState,
} from '@/lib/chat-client';
import EmailForm from './EmailForm';
import MessageList from './MessageList';
import { CHAT_KNOWN_FLAG, isPreview, onOpenChatRequest, shouldStartSession, takeResumeToken, writeFlag } from './gate';

type SendError = 'rateLimited' | 'sendFailed' | 'tooLong' | 'imageTooLarge' | 'imageInvalid' | 'imageFailed';

/**
 * 官网访客会话挂件（入口按钮 + 对话面板）。由 ChatWidgetLazy 按需加载。
 *
 * 出现条件（全部满足才渲染，否则返回 null）：
 * 1. 不是 App 内嵌页（`?embed=true` / `#embed`）；
 * 2. 两种进入方式之一：
 *    - `deferStart`（ChatWidgetLazy 已探测到会话开着）：先只画入口，访客第一次展开面板才调
 *      `start()`——路过的访客不建主体、不种 cookie；
 *    - 否则必须 `shouldStartSession()`（预览身份、带继续对话令牌或建过会话），挂载即调 `start()`；
 * 3. 服务端 `session` 返回 `enabled: true`（返回 false 则整个挂件消失）。
 *
 * `?chat=preview` 以预览身份建会话（本标签页内跟着站内跳转走）。邮件回链是 `#chat=<令牌>`：
 * 令牌由根布局的内联脚本在统计脚本之前移出地址栏，这里取走、传给服务端并自动展开面板。
 *
 * 留邮箱是硬门槛：服务端说 `emailRequired`（游客且没留过邮箱；已登录的直接用账号邮箱）时，
 * 先画邮箱表单、锁住输入与欢迎选项，留完才能发。发送时服务端以 `email_required` 拒绝（例如 cookie
 * 丢了、重建出的新游客没有邮箱）也会把表单重新画出来。
 *
 * 实时通道（WebSocket / 轮询）在访客第一次展开面板、或会话已存在时才建立。
 * 会话状态（处理方、是否关闭）以服务端下发为准。
 *
 * 位置用逻辑方向（end-*）：阿拉伯语 / 波斯语页面里入口在左下角。
 */
export default function ChatWidget({
  createClient = createChatClient,
  deferStart = false,
}: {
  createClient?: () => ChatClient;
  deferStart?: boolean;
}) {
  const t = useTranslations('chat');
  // 只取首次传入的工厂：父组件重渲染换了函数身份不该导致重连
  const [create] = useState(() => createClient);
  const [session, setSession] = useState<SessionState | null>(null); // 非 null = 服务端已确认 enabled
  // 要不要调 start()：非延迟模式挂载即调；延迟模式等访客第一次展开面板
  const [started, setStarted] = useState(!deferStart);
  // 服务端 session 说没开：整个挂件消失（延迟模式下入口也收起）
  const [unavailable, setUnavailable] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [conversation, setConversation] = useState<ChatConversation | null>(null);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<SendError | null>(null);
  const [uploading, setUploading] = useState(false);
  // 必须先留邮箱：以服务端为准（session 下发、发送被拒时置上），留成功后放开
  const [emailRequired, setEmailRequired] = useState(false);
  const [emailSaved, setEmailSaved] = useState(false);

  const clientRef = useRef<ChatClient | null>(null);
  // 入口参数只读一次并存在 ref 里：令牌只能取走一次，严格模式的第二次挂载要靠它拿回来
  const entryRef = useRef<{ preview: boolean; resume: string | null } | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const launcherRef = useRef<HTMLButtonElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const wasOpenRef = useRef(false);

  useEffect(() => {
    if (!started || isEmbeddedPage()) return;
    if (entryRef.current === null) {
      if (!deferStart && !shouldStartSession()) return;
      entryRef.current = { preview: isPreview(), resume: takeResumeToken() };
    }
    const entry = entryRef.current;
    const client = create();
    clientRef.current = client;
    let cancelled = false;
    const off = client.onMessages((list) => {
      if (cancelled) return;
      setMessages(list);
    });
    const offConv = client.onConversation((conv) => {
      if (cancelled) return;
      setConversation(conv);
    });
    const opts: { preview?: boolean; resume?: string } = {};
    if (entry.preview) opts.preview = true;
    if (entry.resume) opts.resume = entry.resume;
    client
      .start(window.location.pathname, opts)
      .then((state) => {
        if (cancelled) return;
        if (!state.enabled) {
          setUnavailable(true);
          return;
        }
        writeFlag(CHAT_KNOWN_FLAG);
        setConversation(state.conversation);
        setEmailRequired(state.emailRequired);
        setSession(state);
        if (entry.resume) setOpen(true);
      })
      .catch(() => {
        if (cancelled) return;
        // 会话建不起来：延迟模式收起面板、保留入口，访客可以再点一次重试；非延迟模式不出现入口
        if (deferStart) {
          setStarted(false);
          setOpen(false);
        }
      });
    return () => {
      cancelled = true;
      off();
      offConv();
      client.stop();
      clientRef.current = null;
    };
  }, [create, started, deferStart]);

  const visible = session !== null;
  // 能画入口：服务端已确认，或延迟模式下探测说开着（且 session 没否认）
  const available = !unavailable && (visible || deferStart);

  // 展开面板；延迟模式下第一次展开才建会话
  const openPanel = () => {
    setStarted(true);
    setOpen(true);
  };

  useEffect(() => {
    if (!available) return;
    return onOpenChatRequest(() => {
      setStarted(true);
      setOpen(true);
    });
  }, [available]);

  // 第一次展开才建实时通道：从没点开过的访客不占连接（已有会话的由客户端自己连）
  useEffect(() => {
    if (open && visible) clientRef.current?.activate();
  }, [open, visible]);

  useEffect(() => {
    // 展开时焦点给输入框（要先留邮箱时输入框锁着，留完邮箱解锁的那一刻再给）
    if (open) {
      if (!emailRequired) inputRef.current?.focus();
    } else if (wasOpenRef.current) launcherRef.current?.focus();
    wasOpenRef.current = open;
  }, [open, emailRequired]);

  useEffect(() => {
    if (open) endRef.current?.scrollIntoView?.({ block: 'end' });
  }, [open, messages, emailRequired]);

  if (!available) return null;

  /** 服务端要求先留邮箱：重新画表单（不另报错，表单本身就是说明）。 */
  const needEmail = (err: unknown) => {
    if (!(err instanceof ChatError && err.kind === 'email_required')) return false;
    setEmailSaved(false);
    setEmailRequired(true);
    return true;
  };

  /** 发一张图片：先在本地拦住明显不行的（类型、大小），服务端会按内容再判一次。 */
  const uploadImage = async (file: File) => {
    if (!session?.images || uploading || emailRequired) return;
    if (!(CHAT_IMAGE_TYPES as readonly string[]).includes(file.type)) {
      setError('imageInvalid');
      return;
    }
    if (file.size > CHAT_IMAGE_MAX_BYTES) {
      setError('imageTooLarge');
      return;
    }
    setError(null);
    setUploading(true);
    try {
      await clientRef.current?.sendImage(file);
    } catch (err) {
      if (needEmail(err)) return;
      setError(err instanceof ChatError && err.kind === 'rate_limited' ? 'rateLimited' : 'imageFailed');
    } finally {
      setUploading(false);
    }
  };

  // 粘贴截图直接发送（剪贴板里有文字时照常粘贴文字）
  const onInputPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!session?.images) return;
    const file = [...e.clipboardData.files].find((f) => f.type.startsWith('image/'));
    if (!file) return;
    e.preventDefault();
    void uploadImage(file);
  };

  const fail = (err: unknown) => {
    if (needEmail(err)) return;
    setError(err instanceof ChatError && err.kind === 'rate_limited' ? 'rateLimited' : 'sendFailed');
  };

  const submit = async () => {
    const text = draft.trim();
    if (!text || !session || emailRequired) return; // 会话还没建好 / 还没留邮箱：输入框里的字留着
    if ([...text].length > CHAT_CONTENT_MAX) {
      setError('tooLong');
      return;
    }
    setError(null);
    setDraft('');
    try {
      await clientRef.current?.send('text', text);
    } catch (err) {
      // 没发出去：把文字还给输入框（期间访客已经另打了字就不覆盖）
      setDraft((current) => current || text);
      fail(err);
    }
  };

  const pickOption = (value: string) => {
    if (emailRequired) return;
    setError(null);
    clientRef.current?.send('option_reply', value).catch(fail);
  };

  const onInputKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter' || e.shiftKey) return;
    // 输入法组字中的回车是"上屏"，不是发送（Safari 用 keyCode 229 表示）
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    void submit();
  };

  const submitEmail = async (email: string) => {
    const client = clientRef.current;
    if (!client) throw new Error('chat not ready');
    await client.leaveEmail(email);
    setEmailRequired(false);
    setEmailSaved(true);
    setError(null);
  };

  if (!open) {
    return (
      <button
        ref={launcherRef}
        type="button"
        aria-label={t('launcher')}
        onClick={openPanel}
        className="fixed end-4 bottom-4 z-50 flex size-14 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <MessageCircle className="size-6" aria-hidden />
      </button>
    );
  }

  const welcome = session?.welcome ?? null;
  const showWelcome = welcome !== null && messages.length === 0 && conversation === null;

  return (
    <div
      role="dialog"
      aria-label={t('title')}
      onKeyDown={(e) => {
        // 输入法组字中的 Esc 是取消候选，不是关面板
        if (e.key === 'Escape' && !e.nativeEvent.isComposing && e.keyCode !== 229) setOpen(false);
      }}
      className="fixed end-0 bottom-0 z-50 flex h-[32rem] max-h-[85dvh] w-full flex-col overflow-hidden rounded-t-2xl border border-border bg-background shadow-2xl min-[480px]:end-4 min-[480px]:bottom-4 min-[480px]:w-[22rem] min-[480px]:rounded-2xl"
    >
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-sm font-semibold text-foreground">{t('title')}</span>
        <Button type="button" variant="ghost" size="icon" aria-label={t('close')} onClick={() => setOpen(false)}>
          <X aria-hidden />
        </Button>
      </div>

      {/* data-sentry-mask：对话内容不进会话回放 */}
      <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3" aria-live="polite" data-sentry-mask>
        {!session && <p className="text-center text-xs text-muted-foreground">{t('connecting')}</p>}
        {showWelcome && (
          <div className="space-y-2">
            <p className="max-w-[85%] rounded-2xl rounded-bl-sm bg-muted px-3 py-2 text-sm whitespace-pre-wrap text-foreground">
              {welcome.text}
            </p>
            {!emailRequired && (
            <div className="flex flex-wrap gap-2">
              {welcome.options.map((o) => (
                <Button key={o.value} type="button" variant="outline" size="sm" onClick={() => pickOption(o.value)}>
                  {o.label}
                </Button>
              ))}
            </div>
            )}
          </div>
        )}
        <MessageList
          messages={messages}
          welcome={welcome}
          labels={{
            ai: t('senderAi'),
            staff: t('senderStaff'),
            image: t('imagePlaceholder'),
            events: {
              transfer_human: t('eventTransferHuman'),
              handed_to_ai: t('eventHandedToAi'),
              closed: t('eventClosed'),
              auto_closed: t('eventAutoClosed'),
            },
          }}
        />
        {conversation?.status === 'closed' && (
          <p className="text-center text-xs text-muted-foreground">{t('closed')}</p>
        )}
        {emailRequired && (
          <EmailForm
            labels={{
              prompt: t('emailPrompt'),
              placeholder: t('emailPlaceholder'),
              submit: t('emailSubmit'),
              invalid: t('emailInvalid'),
              failed: t('emailFailed'),
            }}
            onSubmit={submitEmail}
          />
        )}
        {emailSaved && <p className="text-center text-xs text-muted-foreground">{t('emailSaved')}</p>}
        <div ref={endRef} />
      </div>

      <div className="border-t border-border p-3">
        {error && (
          <p role="alert" className="mb-2 text-xs text-destructive">
            {t(error)}
          </p>
        )}
        {uploading && <p className="mb-2 text-xs text-muted-foreground">{t('uploading')}</p>}
        <div className="flex items-end gap-2">
          {session?.images && (
            <>
              <input
                ref={fileRef}
                type="file"
                accept={CHAT_IMAGE_TYPES.join(',')}
                className="hidden"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  e.target.value = ''; // 同一张图可以再选一次
                  if (file) void uploadImage(file);
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('attachImage')}
                disabled={uploading || emailRequired}
                onClick={() => fileRef.current?.click()}
              >
                <ImagePlus aria-hidden />
              </Button>
            </>
          )}
          <Textarea
            ref={inputRef}
            rows={1}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onInputKeyDown}
            onPaste={onInputPaste}
            placeholder={t('placeholder')}
            aria-label={t('placeholder')}
            disabled={emailRequired}
            className="max-h-32 min-h-9 resize-none"
          />
          <Button type="button" size="icon" aria-label={t('send')} disabled={!session || emailRequired} onClick={() => void submit()}>
            <Send aria-hidden />
          </Button>
        </div>
      </div>
    </div>
  );
}
