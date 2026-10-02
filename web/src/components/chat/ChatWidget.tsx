'use client';

import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslations } from 'next-intl';
import { MessageCircle, Send, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { siteBrand } from '@/lib/brands';
import { isEmbeddedPage } from '@/lib/funnel';
import {
  CHAT_CONTENT_MAX,
  ChatError,
  createChatClient,
  deriveConversation,
  type ChatClient,
  type ChatMessage,
  type SessionState,
} from '@/lib/chat-client';
import EmailForm from './EmailForm';
import MessageList from './MessageList';
import {
  CHAT_EMAIL_FLAG,
  CHAT_KNOWN_FLAG,
  onOpenChatRequest,
  parseChatParam,
  readFlag,
  shouldProbeSession,
  writeFlag,
  type ChatParam,
} from './gate';

/** 转人工后这么久没有客服消息，就请访客留邮箱。 */
const EMAIL_PROMPT_AFTER_MS = 30_000;

type SendError = 'rateLimited' | 'sendFailed' | 'tooLong';

type ChatwootWindow = { $chatwoot?: { toggleBubbleVisibility?: (v: 'hide' | 'show') => void } };

function setChatwootBubble(v: 'hide' | 'show') {
  try {
    (window as unknown as ChatwootWindow).$chatwoot?.toggleBubbleVisibility?.(v);
  } catch {
    // 第三方 SDK 出错不该影响本挂件
  }
}

/** 把继续对话令牌从地址栏拿掉，免得留在地址栏、历史记录和后续请求的 Referer 里。 */
function stripChatParam() {
  const url = new URL(window.location.href);
  url.searchParams.delete('chat');
  window.history.replaceState(window.history.state, '', `${url.pathname}${url.search}${url.hash}`);
}

/**
 * 官网访客会话挂件（入口按钮 + 对话面板）。只挂在售前页面，由 ChatWidgetLazy 按需加载。
 *
 * 出现条件（全部满足才渲染，否则返回 null）：
 * 1. 品牌注册表 `chatEnabled`；
 * 2. 不是 App 内嵌页（`?embed=true` / `#embed`，与旧客服挂件同一判定）；
 * 3. `shouldProbeSession()`——暗发布阶段只有 `?chat=` 或建过会话的浏览器才去问服务端，
 *    其余访客不渲染、不发请求、不种 cookie；
 * 4. 服务端 `session` 返回 `enabled: true`。
 *
 * `?chat=preview` 以预览身份建会话（参数保留）；`?chat=<其他值>` 是邮件回链的继续对话令牌：
 * 传给服务端、自动展开面板，并立刻从地址栏移除。
 *
 * 本挂件渲染期间隐藏旧客服气泡，避免两个入口叠在一起；卸载时恢复。
 */
export default function ChatWidget({ createClient = createChatClient }: { createClient?: () => ChatClient }) {
  const t = useTranslations('chat');
  // 只取首次传入的工厂：父组件重渲染换了函数身份不该导致重连
  const [create] = useState(() => createClient);
  const [session, setSession] = useState<SessionState | null>(null); // 非 null = 服务端已确认 enabled
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<SendError | null>(null);
  const [emailState, setEmailState] = useState<'idle' | 'saved' | 'done'>(() =>
    readFlag(CHAT_EMAIL_FLAG) ? 'done' : 'idle',
  );
  const [emailDue, setEmailDue] = useState(false);

  const clientRef = useRef<ChatClient | null>(null);
  // URL 参数只读一次并存在 ref 里：令牌读完即从地址栏移除，严格模式的第二次挂载要靠它拿回来
  const paramRef = useRef<ChatParam | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const launcherRef = useRef<HTMLButtonElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const wasOpenRef = useRef(false);

  useEffect(() => {
    if (!siteBrand().chatEnabled || isEmbeddedPage()) return;
    if (paramRef.current === null) {
      if (!shouldProbeSession()) return;
      paramRef.current = parseChatParam(window.location.search);
      if (paramRef.current.kind === 'resume') stripChatParam();
    }
    const param = paramRef.current;
    const client = create();
    clientRef.current = client;
    let cancelled = false;
    const off = client.onMessages((list) => {
      if (!cancelled) setMessages(list);
    });
    client
      .start(
        window.location.pathname,
        param.kind === 'preview' ? { preview: true } : param.kind === 'resume' ? { resume: param.token } : {},
      )
      .then((state) => {
        if (cancelled || !state.enabled) return;
        writeFlag(CHAT_KNOWN_FLAG);
        setSession(state);
        if (param.kind === 'resume') setOpen(true);
      })
      .catch(() => {
        // 会话建不起来：不出现入口，旧客服入口照常可用
      });
    return () => {
      cancelled = true;
      off();
      client.stop();
      clientRef.current = null;
    };
  }, [create]);

  const visible = session !== null;

  useEffect(() => {
    if (!visible) return;
    const hide = () => setChatwootBubble('hide');
    hide();
    // 旧客服 SDK 可能比我们晚就绪
    window.addEventListener('chatwoot:ready', hide);
    const offOpen = onOpenChatRequest(() => setOpen(true));
    return () => {
      window.removeEventListener('chatwoot:ready', hide);
      offOpen();
      setChatwootBubble('show');
    };
  }, [visible]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
    else if (wasOpenRef.current) launcherRef.current?.focus();
    wasOpenRef.current = open;
  }, [open]);

  useEffect(() => {
    if (open) endRef.current?.scrollIntoView?.({ block: 'end' });
  }, [open, messages, emailDue]);

  const conversation = session ? deriveConversation(session.conversation, messages) : null;
  // 最近一次转人工之后有没有客服消息（没有转人工事件时，看整段历史）
  const lastTransfer = messages.findLastIndex((m) => m.kind === 'event' && m.meta?.event === 'transfer_human');
  const staffReplied = messages.some((m, i) => i > lastTransfer && m.senderType === 'staff');
  const waitingHuman = conversation?.status === 'open' && conversation.handler === 'human' && !staffReplied;
  const wantsEmail = open && waitingHuman && emailState === 'idle';

  // 计时从"面板开着且在等人工"这一刻算起，即转人工与打开面板两者中较晚的那个
  useEffect(() => {
    if (!wantsEmail) return;
    const timer = setTimeout(() => setEmailDue(true), EMAIL_PROMPT_AFTER_MS);
    return () => {
      clearTimeout(timer);
      setEmailDue(false);
    };
  }, [wantsEmail]);

  if (!session) return null;

  const fail = (err: unknown) =>
    setError(err instanceof ChatError && err.kind === 'rate_limited' ? 'rateLimited' : 'sendFailed');

  const submit = async () => {
    const text = draft.trim();
    if (!text) return;
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
    await clientRef.current?.leaveEmail(email);
    writeFlag(CHAT_EMAIL_FLAG);
    setEmailState('saved');
  };

  if (!open) {
    return (
      <button
        ref={launcherRef}
        type="button"
        aria-label={t('launcher')}
        onClick={() => setOpen(true)}
        className="fixed bottom-4 right-4 z-50 flex size-14 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <MessageCircle className="size-6" aria-hidden />
      </button>
    );
  }

  const welcome = session.welcome;
  const showWelcome = welcome !== null && messages.length === 0 && session.conversation === null;

  return (
    <div
      role="dialog"
      aria-label={t('title')}
      onKeyDown={(e) => {
        if (e.key === 'Escape') setOpen(false);
      }}
      className="fixed bottom-0 right-0 z-50 flex h-[32rem] max-h-[85dvh] w-full flex-col overflow-hidden rounded-t-2xl border border-border bg-background shadow-2xl min-[480px]:bottom-4 min-[480px]:right-4 min-[480px]:w-[22rem] min-[480px]:rounded-2xl"
    >
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-sm font-semibold text-foreground">{t('title')}</span>
        <Button type="button" variant="ghost" size="icon" aria-label={t('close')} onClick={() => setOpen(false)}>
          <X aria-hidden />
        </Button>
      </div>

      <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3" aria-live="polite">
        {showWelcome && (
          <div className="space-y-2">
            <p className="max-w-[85%] rounded-2xl rounded-bl-sm bg-muted px-3 py-2 text-sm whitespace-pre-wrap text-foreground">
              {welcome.text}
            </p>
            <div className="flex flex-wrap gap-2">
              {welcome.options.map((o) => (
                <Button key={o.value} type="button" variant="outline" size="sm" onClick={() => pickOption(o.value)}>
                  {o.label}
                </Button>
              ))}
            </div>
          </div>
        )}
        <MessageList
          messages={messages}
          welcome={welcome}
          labels={{ ai: t('senderAi'), staff: t('senderStaff'), image: t('imagePlaceholder') }}
        />
        {conversation?.status === 'closed' && (
          <p className="text-center text-xs text-muted-foreground">{t('closed')}</p>
        )}
        {emailDue && wantsEmail && (
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
        {emailState === 'saved' && <p className="text-center text-xs text-muted-foreground">{t('emailSaved')}</p>}
        <div ref={endRef} />
      </div>

      <div className="border-t border-border p-3">
        {error && (
          <p role="alert" className="mb-2 text-xs text-destructive">
            {t(error)}
          </p>
        )}
        <div className="flex items-end gap-2">
          <Textarea
            ref={inputRef}
            rows={1}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onInputKeyDown}
            placeholder={t('placeholder')}
            aria-label={t('placeholder')}
            className="max-h-32 min-h-9 resize-none"
          />
          <Button type="button" size="icon" aria-label={t('send')} onClick={() => void submit()}>
            <Send aria-hidden />
          </Button>
        </div>
      </div>
    </div>
  );
}
