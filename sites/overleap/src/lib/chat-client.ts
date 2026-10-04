/**
 * 访客会话客户端（官网挂件用）：HTTP + WebSocket，断线重连、游标补齐、发送重试。
 *
 * 约束（服务端事实，见 api/api_chat.go、api/logic_chat_realtime.go）：
 * - HTTP 一律走同源相对路径 `/api/chat/...` 并带 cookie —— 访客身份 cookie 是 HttpOnly，
 *   由服务端种在同站 `/api` 路径上，换成跨域 API 主机就拿不到。
 * - WebSocket 连 `session.ws.url`（另一个主机），只靠 `?token=` 鉴权；令牌 5 分钟有效，
 *   每次重连前换新。服务端忽略客户端发来的帧，所以这里只收不发。
 * - 消息先落库再广播：推送丢了不丢消息，每次（重）连成功后按最大 id 拉一次补齐。
 * - 会话状态（status / handler）以服务端为准，四个来源：`session`、发消息的响应、
 *   `GET /messages` 的响应、WebSocket 的 `state` 帧。客户端不从消息里推状态。
 * - 推送频道按访客分，不按会话分：帧带着所属会话的 uuid，不是当前会话的帧不收（改为拉一次补齐）。
 * - 服务端凭 WebSocket 在线标记决定要不要发离线邮件：标签页在后台超过 1 分钟就主动断开，
 *   回到前台再连。轮询只是连不上时的顶班，会在联网、回前台与每 5 分钟各试一次回到 WebSocket。
 *
 * 设计：docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md §5 / §6
 */
import { SITE } from '@/lib/site';

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
  /** 服务端配了图片存储：挂件才画"发图片"按钮。 */
  images: boolean;
}

/** 与 api chatImageMaxBytes 一致。 */
export const CHAT_IMAGE_MAX_BYTES = 5 * 1024 * 1024;
/** 与 api chatImageTypes 一致（服务端按内容再嗅探一次，这里只是提前拦住明显不行的）。 */
export const CHAT_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const;

export interface ChatClient {
  /**
   * 建立 / 恢复会话。只有响应里已经有会话时才会立刻建实时通道；否则等 `activate()`
   * —— 从没点开过挂件的访客不占任何连接。重复调用会先关掉上一次的连接与定时器。
   */
  start(path: string, opts?: { preview?: boolean; resume?: string }): Promise<SessionState>;
  /** 访客第一次展开面板时调用：开始实时通道（WebSocket，或回落轮询）。幂等。 */
  activate(): void;
  /** 自动生成 clientId；失败重试复用同一 clientId。 */
  send(kind: ChatSendKind, content: string): Promise<void>;
  /** 上传一张图片作为访客消息（不做乐观气泡：成功后消息随响应进入列表）。主体丢失时重建会话重试一次。 */
  sendImage(file: Blob): Promise<void>;
  leaveEmail(email: string): Promise<void>;
  /** 回调拿到的是全量列表：已确认的按 id 去重、升序，其后是未确认的乐观气泡。 */
  onMessages(cb: (msgs: ChatMessage[]) => void): () => void;
  /** 会话状态变化（服务端下发）时回调；`null` = 当前没有会话。 */
  onConversation(cb: (conv: ChatConversation | null) => void): () => void;
  stop(): void;
}

/** 挂件按 kind 选文案；`code` 是信封错误码或 HTTP 状态，仅供排查，不给访客看。 */
export type ChatErrorKind = 'rate_limited' | 'invalid' | 'network';

export class ChatError extends Error {
  constructor(
    public readonly kind: ChatErrorKind,
    public readonly code: number,
    /**
     * 服务端不认这个访客了（cookie 被清 / 过期）。只有它才值得重建会话——
     * 同一个 422 也用于内容、邮箱等校验错误，那些重建了也没用。
     */
    public readonly subjectLost = false,
  ) {
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
  /** [0,1) 随机数，用于重连抖动；测试注入定值。 */
  random?: () => number;
}

/** 图片上传的单次超时。 */
const IMAGE_TIMEOUT_MS = 60_000;
export const CHAT_CONTENT_MAX = 2000; // 与 api chatContentMaxRunes 一致（按字符数）

const SEND_RETRY_DELAYS = [1000, 2000, 4000];
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30_000;
/** 重连抖动 ±25%：服务端重启时不让所有访客在同一毫秒涌回来。 */
const RECONNECT_JITTER = 0.25;
/** 连上后撑过这么久才算"稳定"，退避归零；连上即断的抖动连接继续退避。 */
const STABLE_AFTER_MS = 10_000;
const MAX_CONNECT_FAILURES = 3;
const POLL_INTERVAL_MS = 4000;
/** 标签页在后台这么久就主动断开 WebSocket：服务端不再把这个访客算作在线，客服回复会走离线邮件。 */
const HIDDEN_RELEASE_MS = 60_000;
/** 回落轮询后每隔这么久试一次回到 WebSocket（另有联网、回前台两个时机）。 */
const WS_RESTORE_INTERVAL_MS = 5 * 60_000;
const FETCH_TIMEOUT_MS = 15_000;

const CODE_RATE_LIMITED = 429;
const CODE_SERVER_ERROR = 500;
const CODE_INVALID_ARGUMENT = 422;
/**
 * api/api_chat.go 在没有访客主体时返回的信封：code 422 + 这条 message。422 同时用于各种校验错误，
 * 只有 message 能区分，所以这里与服务端文案绑定（仅用于判断，不展示）。服务端改了这句话，
 * 后果是不再自动重建会话（退化为报"发送失败"），不会误重建。
 */
const MSG_NO_SUBJECT = 'no chat session';
/**
 * 重建会话失败后的后台重试：1s 起翻倍、上限 30s，一轮最多这么多次。一轮用尽后本轮结束、通道照旧；
 * 之后只要再收到一次"没有访客主体"（下一次轮询 / 补齐 / 发消息），就会开启新的一轮。
 */
const RESESSION_RETRY_BASE_MS = 1000;
const RESESSION_RETRY_MAX_MS = 30_000;
const RESESSION_MAX_RETRIES = 8;

/**
 * 消息去重键（服务端上限 36 字符）。`crypto.randomUUID` 在 Chrome < 92 / Safari < 15.4 不存在，
 * 退回用 `getRandomValues` 拼 v4；连它也没有时用时间 + Math.random（只求会话内唯一）。
 */
export function newClientId(): string {
  const c = typeof crypto !== 'undefined' ? crypto : undefined;
  if (c && typeof c.randomUUID === 'function') return c.randomUUID();
  const bytes = new Uint8Array(16);
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(bytes);
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => (b + 0x100).toString(16).slice(1));
  return `${hex.slice(0, 4).join('')}-${hex.slice(4, 6).join('')}-${hex.slice(6, 8).join('')}-${hex.slice(8, 10).join('')}-${hex.slice(10).join('')}`;
}

function isChatMessage(v: unknown): v is ChatMessage {
  if (!v || typeof v !== 'object') return false;
  const m = v as Record<string, unknown>;
  return typeof m.id === 'number' && m.id > 0 && typeof m.kind === 'string' && typeof m.senderType === 'string';
}

function asConversation(v: unknown): ChatConversation | null {
  if (!v || typeof v !== 'object') return null;
  const c = v as Record<string, unknown>;
  if (typeof c.uuid !== 'string' || typeof c.status !== 'string' || typeof c.handler !== 'string') return null;
  return { uuid: c.uuid, status: c.status as ChatConversation['status'], handler: c.handler as ChatConversation['handler'] };
}

export function createChatClient(options: ChatClientOptions = {}): ChatClient {
  const doFetch: typeof fetch = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
  const SocketCtor = options.WebSocket ?? (globalThis.WebSocket as unknown as new (url: string) => ChatSocket);
  const randomId = options.randomId ?? newClientId;
  const random = options.random ?? Math.random;

  let stopped = false;
  const listeners = new Set<(msgs: ChatMessage[]) => void>();
  const convListeners = new Set<(conv: ChatConversation | null) => void>();
  const confirmed = new Map<number, ChatMessage>();
  let pending: ChatMessage[] = [];
  let cursor = 0; // 见过的最大 id
  let conversation: ChatConversation | null = null;

  let lastStart: { path: string; preview?: boolean } | null = null;
  let wsInfo: SessionState['ws'] = null;
  let sessionReady = false; // session 返回 enabled:true 之后
  let activated = false; // 访客展开过面板，或会话已存在
  let transportUp = false;
  /** 每次重建通道 +1：旧通道的回调与定时器醒来后发现代数不符就作废。 */
  let generation = 0;
  let socket: ChatSocket | null = null;
  let connectFailures = 0; // 连续"没连上就断"的次数（只算握手 / 建连失败，不算换令牌的网络失败）
  let tokenFailures = 0; // 连续换令牌网络失败的次数
  /** 已排了一次重连（在等退避或在换令牌）：其他触发点不再另起一次。 */
  let reconnecting = false;
  /** 标签页在后台待久了，WebSocket 已主动断开；回到前台才恢复。 */
  let suspended = false;
  let hiddenTimer: ReturnType<typeof setTimeout> | null = null;
  let restoreTimer: ReturnType<typeof setInterval> | null = null;
  let backoffAttempt = 0;
  let polling = false;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let watchingVisibility = false;
  let resessionRun: Promise<boolean> | null = null; // 进行中的那次重建（并发调用共用）
  let resessionRetrying = false; // 后台重试循环在跑
  /** 重建成功后还没有任何带主体的请求成功过。此时再报"主体丢失"说明 cookie 根本存不住，不再重建，免得每次轮询都新建一个访客。 */
  let resessionUnproven = false;
  /** 所有一次性定时器及其唤醒函数：stop() 清掉定时器并唤醒等待者，让它们自行发现已停止。 */
  const timers = new Map<ReturnType<typeof setTimeout>, () => void>();
  /** 在途请求的超时器与中断器：stop() 一并清掉。 */
  const inflight = new Map<ReturnType<typeof setTimeout>, AbortController>();

  function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const id = setTimeout(() => {
        timers.delete(id);
        resolve();
      }, ms);
      timers.set(id, resolve);
    });
  }

  async function call<T>(method: 'GET' | 'POST', path: string, body?: unknown, timeoutMs = FETCH_TIMEOUT_MS): Promise<T> {
    const ctrl = new AbortController();
    const timeout = setTimeout(() => ctrl.abort(), timeoutMs);
    inflight.set(timeout, ctrl);
    try {
      let res: Response;
      try {
        res = await doFetch(`/api/chat${path}`, {
          method,
          credentials: 'include',
          // FormData 由浏览器自己写 multipart 边界，不能手设 Content-Type
          headers: body instanceof FormData
            ? { 'X-K2-Brand': SITE.brandId }
            : { 'Content-Type': 'application/json', 'X-K2-Brand': SITE.brandId },
          body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body),
          signal: ctrl.signal,
        });
      } catch {
        throw new ChatError('network', 0); // 断网、超时中断都走这里
      }
      if (!res.ok) {
        if (res.status === CODE_RATE_LIMITED) throw new ChatError('rate_limited', res.status);
        throw new ChatError(res.status >= 500 ? 'network' : 'invalid', res.status);
      }
      let envelope: { code?: number; data?: T; message?: string };
      try {
        envelope = await res.json();
      } catch {
        throw new ChatError('network', res.status);
      }
      const code = envelope.code ?? CODE_SERVER_ERROR;
      if (code === 0) return envelope.data as T;
      if (code === CODE_RATE_LIMITED) throw new ChatError('rate_limited', code);
      if (code >= CODE_SERVER_ERROR && code < 600) throw new ChatError('network', code);
      throw new ChatError('invalid', code, code === CODE_INVALID_ARGUMENT && envelope.message === MSG_NO_SUBJECT);
    } finally {
      clearTimeout(timeout);
      inflight.delete(timeout);
    }
  }

  function emit() {
    if (stopped) return;
    const list = [...confirmed.values()].sort((a, b) => a.id - b.id).concat(pending);
    for (const cb of [...listeners]) cb(list);
  }

  /** 服务端下发的会话状态；有变化才通知。 */
  function setConversation(next: ChatConversation | null) {
    if (stopped) return;
    const prev = conversation;
    if (prev?.uuid === next?.uuid && prev?.status === next?.status && prev?.handler === next?.handler) return;
    conversation = next;
    for (const cb of [...convListeners]) cb(next);
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

  /**
   * `keepTransport`（重建会话时为 true）：新会话拿到手之前不动现有通道——
   * 重建失败时访客还能继续靠原来的 WebSocket / 轮询收消息。
   */
  async function session(
    path: string,
    preview: boolean | undefined,
    resume: string | undefined,
    keepTransport = false,
  ): Promise<SessionState> {
    if (!keepTransport) {
      teardownTransport();
      sessionReady = false;
    }
    const body: Record<string, unknown> = { path };
    if (preview) body.preview = true;
    if (resume) body.resume = resume;
    const data = await call<Partial<SessionState>>('POST', '/session', body);
    const state: SessionState = {
      enabled: data?.enabled === true,
      conversation: asConversation(data?.conversation),
      messages: Array.isArray(data?.messages) ? data.messages : [],
      welcome: data?.welcome ?? null,
      ws: data?.ws ?? null,
      images: data?.images === true,
    };
    if (stopped || !state.enabled) return state;
    if (keepTransport) teardownTransport();
    sessionReady = true;
    wsInfo = state.ws;
    setConversation(state.conversation);
    ingest(state.messages);
    if (state.conversation) activated = true;
    if (activated) startTransport();
    return state;
  }

  async function attemptResession(): Promise<boolean> {
    if (stopped || !lastStart) return false;
    try {
      const state = await session(lastStart.path, lastStart.preview, undefined, true);
      if (stopped || !state.enabled) return false;
      resessionUnproven = true;
      return true;
    } catch {
      return false;
    }
  }

  async function retryResession() {
    if (resessionRetrying) return;
    resessionRetrying = true;
    for (let i = 0; i < RESESSION_MAX_RETRIES && !stopped; i++) {
      await sleep(Math.min(RESESSION_RETRY_MAX_MS, RESESSION_RETRY_BASE_MS * 2 ** i));
      if (stopped || (await attemptResession())) break;
    }
    resessionRetrying = false;
  }

  /**
   * 访客主体丢了：用上次的入口页重建会话（不重放继续对话令牌）。返回第一次尝试的结果；
   * 失败时现有通道原样保留，并在后台按退避继续重试。
   */
  function resession(): Promise<boolean> {
    if (stopped || !lastStart || resessionUnproven) return Promise.resolve(false);
    if (resessionRun) return resessionRun;
    if (resessionRetrying) return Promise.resolve(false);
    const run = attemptResession().then((ok) => {
      resessionRun = null;
      if (!ok) void retryResession();
      return ok;
    });
    resessionRun = run;
    return run;
  }

  async function catchUp() {
    if (stopped) return;
    try {
      const data = await call<{ messages?: ChatMessage[]; conversation?: unknown }>('GET', `/messages?after=${cursor}`);
      resessionUnproven = false;
      ingest(data?.messages);
      // 旧版服务端不带 conversation 字段：保持已知状态；带了（含 null）就以它为准
      if (data && 'conversation' in data) setConversation(asConversation(data.conversation));
    } catch (err) {
      // 补齐失败不影响已有内容：下一次轮询 / 重连 / 回到前台会再拉
      if (err instanceof ChatError && err.subjectLost) void resession();
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

  /** 本会话有 WebSocket 可用（服务端给了地址、环境支持）。没有的话轮询就是唯一通道，不存在"恢复"。 */
  const wsCapable = () => wsInfo !== null && Boolean(SocketCtor);

  function startPolling() {
    polling = true;
    startPollTimer();
    if (wsCapable() && restoreTimer === null) {
      restoreTimer = setInterval(() => {
        if (!document.hidden) reconnectNow();
      }, WS_RESTORE_INTERVAL_MS);
    }
  }

  /** WebSocket 连上了：顶班的轮询与定时恢复都停掉。 */
  function stopPolling() {
    polling = false;
    stopPollTimer();
    if (restoreTimer !== null) clearInterval(restoreTimer);
    restoreTimer = null;
  }

  function clearHiddenTimer() {
    if (hiddenTimer !== null) clearTimeout(hiddenTimer);
    hiddenTimer = null;
  }

  function armHiddenTimer() {
    if (hiddenTimer !== null || suspended || !wsCapable()) return;
    hiddenTimer = setTimeout(() => {
      hiddenTimer = null;
      if (stopped || !transportUp || !document.hidden) return;
      // 主动断开：不是连接失败，不动 connectFailures。排着的重连随代数作废，回前台时重新换令牌连
      suspended = true;
      generation++;
      reconnecting = false;
      closeSocket();
    }, HIDDEN_RELEASE_MS);
  }

  /**
   * 立刻试一次 WebSocket（换新令牌再连）。已经连着、正在重连、后台挂起中或本会话没有
   * WebSocket 时什么都不做，所以各个触发点可以放心重复调用。
   */
  function reconnectNow() {
    if (stopped || !transportUp || !wsCapable() || socket || reconnecting || suspended) return;
    reconnecting = true;
    void refreshAndConnect(generation);
  }

  function onVisibility() {
    if (stopped || !transportUp) return;
    if (document.hidden) {
      stopPollTimer(); // 后台标签页不轮询
      armHiddenTimer();
      return;
    }
    clearHiddenTimer();
    void catchUp(); // 回到前台：两种模式都立刻补一次
    if (polling) startPollTimer();
    suspended = false;
    reconnectNow(); // 挂起过 / 已回落轮询：试着回到 WebSocket；连着的不受影响
  }

  function onOnline() {
    if (stopped || !transportUp || document.hidden) return;
    reconnectNow();
  }

  function closeSocket() {
    const ws = socket;
    socket = null;
    if (!ws) return;
    // 先摘回调再关：主动关闭不该触发重连
    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
    try {
      ws.close();
    } catch {
      // 关一个已经坏掉的连接出错无关紧要
    }
  }

  function teardownTransport() {
    generation++;
    transportUp = false;
    connectFailures = 0;
    tokenFailures = 0;
    backoffAttempt = 0;
    reconnecting = false;
    suspended = false;
    clearHiddenTimer();
    stopPolling();
    closeSocket();
  }

  function startTransport() {
    if (stopped || transportUp || !sessionReady) return;
    transportUp = true;
    if (!watchingVisibility) {
      watchingVisibility = true;
      document.addEventListener('visibilitychange', onVisibility);
      window.addEventListener('online', onOnline);
    }
    if (wsCapable() && wsInfo) connect(wsInfo.token);
    else startPolling();
    if (document.hidden) armHiddenTimer(); // 在后台建起来的通道同样受 1 分钟限制
  }

  function connect(token: string) {
    if (stopped || !wsInfo) return;
    let opened = false;
    let openedAt = 0;
    let ws: ChatSocket;
    try {
      ws = new SocketCtor(`${wsInfo.url.replace(/\/+$/, '')}/api/chat/ws?token=${encodeURIComponent(token)}`);
    } catch {
      // 地址不合法 / 环境不支持：按"没连上"处理，三次后回落轮询
      connectFailed();
      return;
    }
    socket = ws;
    ws.onopen = () => {
      if (stopped || socket !== ws) return;
      opened = true;
      openedAt = Date.now();
      connectFailures = 0;
      tokenFailures = 0;
      if (polling) stopPolling(); // 回到 WebSocket：轮询只是连不上期间的顶班
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
        | { type?: unknown; message?: unknown; conversation?: unknown; conversationUuid?: unknown }
        | null;
      if (!payload || typeof payload !== 'object') return;
      if (payload.type !== 'message' && payload.type !== 'state') return;
      const conv = payload.type === 'state' ? asConversation(payload.conversation) : null;
      // 帧归属：频道按访客分，上一条会话的迟到帧也会送到这里。带了所属会话 uuid（顶层
      // conversationUuid，或 state 帧的 conversation.uuid）且不是当前会话的，一律不收；
      // 拉一次补齐，由它发现新会话。旧版服务端不带该字段、或当前还没有会话：照旧处理。
      const current = conversation?.uuid;
      const owners = [payload.conversationUuid, conv?.uuid].filter((u): u is string => typeof u === 'string' && u !== '');
      if (current !== undefined && owners.some((u) => u !== current)) {
        void catchUp();
        return;
      }
      if (payload.type === 'message') ingest([payload.message]);
      else if (conv) setConversation(conv);
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

  /** 握手 / 建连失败。连续三次回落轮询（不再排重连，由联网、回前台与定时恢复再试）。 */
  function connectFailed() {
    connectFailures++;
    if (connectFailures >= MAX_CONNECT_FAILURES) startPolling();
    else scheduleReconnect();
  }

  function scheduleReconnect() {
    const base = Math.min(RECONNECT_MAX_MS, RECONNECT_BASE_MS * 2 ** backoffAttempt);
    const delay = Math.round(base * (1 - RECONNECT_JITTER + random() * 2 * RECONNECT_JITTER));
    backoffAttempt++;
    const gen = generation;
    reconnecting = true;
    void (async () => {
      await sleep(delay);
      if (stopped || gen !== generation) return;
      await refreshAndConnect(gen);
    })();
  }

  /** 换新令牌再连。调用方已把 `reconnecting` 置位；这里在有结果时清掉。 */
  async function refreshAndConnect(gen: number) {
    let token: string;
    try {
      token = (await call<{ token: string }>('GET', '/ws-token')).token;
      resessionUnproven = false;
    } catch (err) {
      if (stopped || gen !== generation) return;
      reconnecting = false;
      if (err instanceof ChatError && err.kind === 'network') {
        // 断网 / 服务端重启：不是"WebSocket 连不上"，不计入回落轮询的次数，按退避一直试下去。
        // 连续几次都换不到时先让轮询顶班（HTTP 也许只是这一个接口有问题），连上后自动停。
        if (++tokenFailures >= MAX_CONNECT_FAILURES && !polling) startPolling();
        scheduleReconnect();
        return;
      }
      connectFailed();
      // 主体丢了换不到令牌：重建会话（成功后会用新令牌重连，这条旧通道的重试随代数作废）
      if (err instanceof ChatError && err.subjectLost) void resession();
      return;
    }
    if (stopped || gen !== generation) return;
    reconnecting = false;
    connect(token);
  }

  const hasBubble = (clientId: string) => pending.some((p) => p.clientId === clientId);

  return {
    async start(path, opts = {}) {
      lastStart = { path, preview: opts.preview };
      return session(path, opts.preview, opts.resume);
    },

    activate() {
      activated = true;
      startTransport();
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
        if (!hasBubble(clientId)) return;
        pending = pending.filter((p) => p.clientId !== clientId);
        emit();
      };
      let resessioned = false;
      for (let attempt = 0; ; ) {
        try {
          const data = await call<{ message: ChatMessage; conversation?: unknown }>('POST', '/messages', {
            kind,
            content,
            clientId,
          });
          // 先收消息（会顺手对掉气泡），再摘气泡兜底（消息已由推送先到时 ingest 不会动它）
          resessionUnproven = false;
          ingest([data?.message]);
          dropBubble();
          const conv = asConversation(data?.conversation);
          if (conv) setConversation(conv);
          // 发出了第一条消息 = 会话已存在，实时通道该起来了（正常路径下面板早已展开过）
          activated = true;
          startTransport();
          return;
        } catch (err) {
          if (stopped) throw err;
          const kindOf = err instanceof ChatError ? err.kind : 'network';
          if (err instanceof ChatError && err.subjectLost && !resessioned) {
            // 访客主体丢了：重建一次会话，再用同一 clientId 重发一次。校验类 422 不走这里
            resessioned = true;
            if (await resession()) continue;
          } else if (kindOf === 'network') {
            if (attempt < SEND_RETRY_DELAYS.length) {
              await sleep(SEND_RETRY_DELAYS[attempt++]);
              if (stopped) throw err;
              // 等待期间消息已经由推送确认（丢的只是响应）：不必再发
              if (!hasBubble(clientId)) return;
              continue;
            }
            // 重试用尽。可能请求其实到了、只是响应丢了：拉一次补齐，气泡被对掉就算发成功
            await catchUp();
            if (!hasBubble(clientId)) return;
          }
          dropBubble();
          throw err;
        }
      }
    },

    async sendImage(file) {
      const form = () => {
        const f = new FormData();
        f.append('clientId', randomId());
        f.append('file', file);
        return f;
      };
      let resessioned = false;
      for (;;) {
        try {
          // 上传比发文字慢：给足时间，且不做自动重发（重发会多传一份）
          const data = await call<{ message: ChatMessage; conversation?: unknown }>('POST', '/images', form(), IMAGE_TIMEOUT_MS);
          resessionUnproven = false;
          ingest([data?.message]);
          const conv = asConversation(data?.conversation);
          if (conv) setConversation(conv);
          activated = true;
          startTransport();
          return;
        } catch (err) {
          if (stopped) throw err;
          if (err instanceof ChatError && err.subjectLost && !resessioned) {
            resessioned = true;
            if (await resession()) continue;
          }
          throw err;
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

    onConversation(cb) {
      convListeners.add(cb);
      return () => {
        convListeners.delete(cb);
      };
    },

    stop() {
      if (stopped) return;
      stopped = true;
      listeners.clear();
      convListeners.clear();
      teardownTransport();
      const wake = [...timers];
      timers.clear();
      for (const [id, resolve] of wake) {
        clearTimeout(id);
        resolve();
      }
      for (const [id, ctrl] of [...inflight]) {
        clearTimeout(id);
        ctrl.abort();
      }
      inflight.clear();
      if (watchingVisibility) {
        document.removeEventListener('visibilitychange', onVisibility);
        window.removeEventListener('online', onOnline);
      }
    },
  };
}
