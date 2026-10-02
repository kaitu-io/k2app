/**
 * 访客会话客户端（官网挂件用）：HTTP + WebSocket，断线重连、游标补齐、发送重试。
 *
 * 约束（服务端事实，见 api/api_chat.go、api/logic_chat_realtime.go）：
 * - HTTP 一律走同源相对路径 `/api/chat/...` 并带 cookie —— 访客身份 cookie 是 HttpOnly，
 *   由服务端种在同站 `/api` 路径上，换成跨域 API 主机就拿不到。
 * - WebSocket 连 `session.ws.url`（另一个主机），只靠 `?token=` 鉴权；令牌 5 分钟有效，
 *   每次重连前换新。服务端忽略客户端发来的帧，所以这里只收不发。
 * - 消息先落库再广播：推送丢了不丢消息，每次（重）连成功后按最大 id 拉一次补齐。
 *
 * 设计：docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md §5 / §6
 */
import { siteBrand } from '@/lib/brands';

export type ChatSenderType = 'visitor' | 'ai' | 'staff' | 'system';
export type ChatMessageKind = 'text' | 'image' | 'options' | 'option_reply' | 'event';
export type ChatSendKind = 'text' | 'option_reply';

export interface ChatMessage {
  /** 服务端消息 id；本地乐观气泡为 0（用 `clientId` 区分）。 */
  id: number;
  senderType: ChatSenderType;
  senderName: string;
  kind: ChatMessageKind;
  content: string;
  meta: Record<string, unknown> | null;
  createdAt: string;
  /** 仅本地乐观气泡带。 */
  clientId?: string;
  /** true = 还没得到服务端确认。 */
  pending?: boolean;
}

export interface ChatConversation {
  uuid: string;
  status: 'open' | 'closed';
  handler: 'ai' | 'human';
}

export interface ChatWelcome {
  text: string;
  options: { label: string; value: string }[];
}

export interface SessionState {
  enabled: boolean;
  conversation: ChatConversation | null;
  messages: ChatMessage[];
  welcome: ChatWelcome | null;
  ws: { url: string; token: string } | null;
}

export interface ChatClient {
  start(path: string, opts?: { preview?: boolean; resume?: string }): Promise<SessionState>;
  /** 自动生成 clientId；失败重试复用同一 clientId。 */
  send(kind: ChatSendKind, content: string): Promise<void>;
  leaveEmail(email: string): Promise<void>;
  /** 回调拿到的是全量列表：已确认的按 id 去重、升序，其后是未确认的乐观气泡。 */
  onMessages(cb: (msgs: ChatMessage[]) => void): () => void;
  stop(): void;
}

/** 挂件按 kind 选文案；`code` 是信封错误码或 HTTP 状态，仅供排查，不给访客看。 */
export type ChatErrorKind = 'rate_limited' | 'invalid' | 'network';

export class ChatError extends Error {
  constructor(public readonly kind: ChatErrorKind, public readonly code: number) {
    super(`chat ${kind} (${code})`);
    this.name = 'ChatError';
  }
  /** 只有网络失败与 5xx 值得用同一 clientId 重试；校验与限流错误重试只会更糟。 */
  get retryable(): boolean {
    return this.kind === 'network';
  }
}

/** 客户端用到的 WebSocket 最小面，测试经构造参数注入替身。 */
export interface ChatSocket {
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: unknown) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  close(): void;
}

export interface ChatClientOptions {
  fetch?: typeof fetch;
  WebSocket?: new (url: string) => ChatSocket;
  randomId?: () => string;
}

export const CHAT_CONTENT_MAX = 2000; // 与 api chatContentMaxRunes 一致（按字符数）

const SEND_RETRY_DELAYS = [1000, 2000, 4000];
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30_000;
/** 连上后撑过这么久才算"稳定"，退避归零；连上即断的抖动连接继续退避。 */
const STABLE_AFTER_MS = 10_000;
const MAX_CONNECT_FAILURES = 3;
const POLL_INTERVAL_MS = 4000;

const CODE_RATE_LIMITED = 429;
const CODE_SERVER_ERROR = 500;

/**
 * 由会话初值 + 消息流推出访客看到的会话状态。
 * 推送通道只下发消息、不下发状态，但每次状态变化服务端都会写一条 system 事件消息
 * （meta.event），所以按 id 顺序回放事件即可；关闭后访客再发消息，服务端自动开新会话（ai 接待）。
 */
export function deriveConversation(
  initial: Pick<ChatConversation, 'status' | 'handler'> | null,
  messages: readonly ChatMessage[],
): Pick<ChatConversation, 'status' | 'handler'> | null {
  let state = initial ? { status: initial.status, handler: initial.handler } : null;
  for (const m of messages) {
    if (m.pending) continue;
    if (m.kind === 'event') {
      const event = m.meta?.event;
      if (!state) state = { status: 'open', handler: 'ai' };
      if (event === 'transfer_human') state = { status: 'open', handler: 'human' };
      else if (event === 'handed_to_ai') state = { status: 'open', handler: 'ai' };
      else if (event === 'closed' || event === 'auto_closed') state = { ...state, status: 'closed' };
    } else if (m.senderType === 'visitor' && (!state || state.status === 'closed')) {
      state = { status: 'open', handler: 'ai' };
    }
  }
  return state;
}

function isChatMessage(v: unknown): v is ChatMessage {
  if (!v || typeof v !== 'object') return false;
  const m = v as Record<string, unknown>;
  return typeof m.id === 'number' && m.id > 0 && typeof m.kind === 'string' && typeof m.senderType === 'string';
}

export function createChatClient(options: ChatClientOptions = {}): ChatClient {
  const doFetch: typeof fetch = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
  const SocketCtor = options.WebSocket ?? (globalThis.WebSocket as unknown as new (url: string) => ChatSocket);
  const randomId = options.randomId ?? (() => crypto.randomUUID());

  let stopped = false;
  const listeners = new Set<(msgs: ChatMessage[]) => void>();
  const confirmed = new Map<number, ChatMessage>();
  let pending: ChatMessage[] = [];
  let cursor = 0; // 见过的最大 id

  let wsUrl = '';
  let socket: ChatSocket | null = null;
  let connectFailures = 0; // 连续"没连上就断"的次数
  let backoffAttempt = 0;
  let polling = false;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let watchingVisibility = false;
  /** 所有一次性定时器及其唤醒函数：stop() 清掉定时器并唤醒等待者，让它们自行发现已停止。 */
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();

  function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const id = setTimeout(() => {
        timers.delete(id);
        resolve();
      }, ms);
      timers.set(id, resolve);
    });
  }

  async function call<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
    let res: Response;
    try {
      res = await doFetch(`/api/chat${path}`, {
        method,
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', 'X-K2-Brand': siteBrand().id },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      throw new ChatError('network', 0);
    }
    if (!res.ok) {
      if (res.status === CODE_RATE_LIMITED) throw new ChatError('rate_limited', res.status);
      throw new ChatError(res.status >= 500 ? 'network' : 'invalid', res.status);
    }
    let envelope: { code?: number; data?: T };
    try {
      envelope = await res.json();
    } catch {
      throw new ChatError('network', res.status);
    }
    const code = envelope.code ?? CODE_SERVER_ERROR;
    if (code === 0) return envelope.data as T;
    if (code === CODE_RATE_LIMITED) throw new ChatError('rate_limited', code);
    throw new ChatError(code >= CODE_SERVER_ERROR && code < 600 ? 'network' : 'invalid', code);
  }

  function emit() {
    if (stopped) return;
    const list = [...confirmed.values()].sort((a, b) => a.id - b.id).concat(pending);
    for (const cb of [...listeners]) cb(list);
  }

  /** 收下服务端消息：按 id 去重、推进游标、顺手对掉匹配的乐观气泡。有变化才通知。 */
  function ingest(incoming: unknown) {
    if (stopped || !Array.isArray(incoming)) return;
    let changed = false;
    for (const m of incoming) {
      // note 是内部备注，服务端不会下发；防御性丢弃
      if (!isChatMessage(m) || (m.kind as string) === 'note' || confirmed.has(m.id)) continue;
      confirmed.set(m.id, m);
      if (m.id > cursor) cursor = m.id;
      if (m.senderType === 'visitor') {
        // 推送可能先于 POST 响应到达，且帧里没有 clientId：按 kind + 内容对掉最早的那个气泡
        const i = pending.findIndex((p) => p.kind === m.kind && p.content === m.content);
        if (i >= 0) pending = pending.filter((_, j) => j !== i);
      }
      changed = true;
    }
    if (changed) emit();
  }

  async function catchUp() {
    if (stopped) return;
    try {
      const data = await call<{ messages: ChatMessage[] }>('GET', `/messages?after=${cursor}`);
      ingest(data?.messages);
    } catch {
      // 补齐失败不影响已有内容：下一次轮询 / 重连 / 回到前台会再拉
    }
  }

  function startPollTimer() {
    if (stopped || pollTimer !== null || document.hidden) return;
    pollTimer = setInterval(() => void catchUp(), POLL_INTERVAL_MS);
  }

  function stopPollTimer() {
    if (pollTimer !== null) clearInterval(pollTimer);
    pollTimer = null;
  }

  function startPolling() {
    polling = true;
    startPollTimer();
  }

  function onVisibility() {
    if (stopped) return;
    if (document.hidden) {
      stopPollTimer(); // 后台标签页不轮询
      return;
    }
    void catchUp(); // 回到前台：两种模式都立刻补一次
    if (polling) startPollTimer();
  }

  function connect(token: string) {
    if (stopped) return;
    let opened = false;
    let openedAt = 0;
    const ws = new SocketCtor(`${wsUrl}/api/chat/ws?token=${encodeURIComponent(token)}`);
    socket = ws;
    ws.onopen = () => {
      if (stopped || socket !== ws) return;
      opened = true;
      openedAt = Date.now();
      connectFailures = 0;
      void catchUp();
    };
    ws.onmessage = (ev) => {
      if (stopped || socket !== ws || typeof ev.data !== 'string') return;
      let frame: unknown;
      try {
        frame = JSON.parse(ev.data);
      } catch {
        return;
      }
      // 广播实现在外面包了一层 {channel,timestamp,payload}；两种形态都认
      const outer = frame as { payload?: unknown } | null;
      const payload = (outer && typeof outer === 'object' && outer.payload ? outer.payload : frame) as
        | { type?: unknown; message?: unknown }
        | null;
      if (payload && payload.type === 'message') ingest([payload.message]);
    };
    ws.onerror = () => {
      // 浏览器在 error 之后必然触发 close，统一在 onclose 里处理
    };
    ws.onclose = () => {
      if (stopped || socket !== ws) return;
      socket = null;
      if (opened && Date.now() - openedAt >= STABLE_AFTER_MS) backoffAttempt = 0;
      if (!opened) connectFailed();
      else scheduleReconnect();
    };
  }

  function connectFailed() {
    connectFailures++;
    if (connectFailures >= MAX_CONNECT_FAILURES) startPolling();
    else scheduleReconnect();
  }

  function scheduleReconnect() {
    const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** backoffAttempt);
    backoffAttempt++;
    void (async () => {
      await sleep(delay);
      if (stopped) return;
      let token: string;
      try {
        token = (await call<{ token: string }>('GET', '/ws-token')).token;
      } catch {
        if (!stopped) connectFailed();
        return;
      }
      connect(token);
    })();
  }

  return {
    async start(path, opts = {}) {
      const body: Record<string, unknown> = { path };
      if (opts.preview) body.preview = true;
      if (opts.resume) body.resume = opts.resume;
      const data = await call<Partial<SessionState>>('POST', '/session', body);
      const state: SessionState = {
        enabled: data?.enabled === true,
        conversation: data?.conversation ?? null,
        messages: Array.isArray(data?.messages) ? data.messages : [],
        welcome: data?.welcome ?? null,
        ws: data?.ws ?? null,
      };
      if (stopped || !state.enabled) return state;
      ingest(state.messages);
      if (!watchingVisibility) {
        watchingVisibility = true;
        document.addEventListener('visibilitychange', onVisibility);
      }
      if (state.ws && SocketCtor) {
        wsUrl = state.ws.url.replace(/\/+$/, '');
        connect(state.ws.token);
      } else {
        startPolling();
      }
      return state;
    },

    async send(kind, content) {
      const clientId = randomId();
      pending = [
        ...pending,
        {
          id: 0,
          senderType: 'visitor',
          senderName: '',
          kind,
          content,
          meta: null,
          createdAt: new Date().toISOString(),
          clientId,
          pending: true,
        },
      ];
      emit();
      const dropBubble = () => {
        const before = pending.length;
        pending = pending.filter((p) => p.clientId !== clientId);
        return pending.length !== before;
      };
      for (let attempt = 0; ; attempt++) {
        try {
          const data = await call<{ message: ChatMessage }>('POST', '/messages', { kind, content, clientId });
          const removed = dropBubble();
          const before = confirmed.size;
          ingest([data?.message]);
          // ingest 没发通知（消息已经由推送先到）但气泡刚被这里摘掉时，补一次通知
          if (removed && confirmed.size === before) emit();
          return;
        } catch (err) {
          const retry = err instanceof ChatError && err.retryable && attempt < SEND_RETRY_DELAYS.length && !stopped;
          if (!retry) {
            if (dropBubble()) emit();
            throw err;
          }
          await sleep(SEND_RETRY_DELAYS[attempt]);
          if (stopped) throw err;
        }
      }
    },

    async leaveEmail(email) {
      await call<unknown>('POST', '/email', { email });
    },

    onMessages(cb) {
      listeners.add(cb);
      return () => {
        listeners.delete(cb);
      };
    },

    stop() {
      if (stopped) return;
      stopped = true;
      listeners.clear();
      stopPollTimer();
      const wake = [...timers];
      timers.clear();
      for (const [id, resolve] of wake) {
        clearTimeout(id);
        resolve();
      }
      if (watchingVisibility) document.removeEventListener('visibilitychange', onVisibility);
      const ws = socket;
      socket = null;
      if (ws) {
        // 先摘回调再关：主动关闭不该触发重连
        ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
        ws.close();
      }
    },
  };
}
