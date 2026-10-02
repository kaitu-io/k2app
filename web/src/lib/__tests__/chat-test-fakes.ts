// 会话挂件测试共用的替身：假 WebSocket（经构造参数注入，不碰全局）与假 fetch 路由。
import { vi } from 'vitest';
import type { ChatMessage, ChatSocket } from '../chat-client';

export class FakeWS implements ChatSocket {
  static instances: FakeWS[] = [];
  static reset() { FakeWS.instances = []; }
  static live() { return FakeWS.instances.filter((w) => !w.closed); }
  static last() { return FakeWS.instances[FakeWS.instances.length - 1]; }

  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    FakeWS.instances.push(this);
  }
  /** 客户端主动关闭：真实浏览器随后会异步触发 onclose，这里同样触发，验证 stop() 不会因此重连。 */
  close() {
    if (this.closed) return;
    this.closed = true;
    this.onclose?.({});
  }
  open() { this.onopen?.({}); }
  recv(frame: unknown) { this.onmessage?.({ data: JSON.stringify(frame) }); }
  /** 服务端/网络断开。 */
  drop() {
    this.closed = true;
    this.onclose?.({});
  }
}

export function msg(id: number, over: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id,
    senderType: 'ai',
    senderName: '',
    kind: 'text',
    content: `m${id}`,
    meta: null,
    createdAt: '2026-10-02T00:00:00Z',
    ...over,
  };
}

type Handler = (body: Record<string, unknown> | undefined, url: string) => unknown;

export interface FakeCall {
  method: string;
  url: string;
  body?: Record<string, unknown>;
  init: RequestInit;
}

/**
 * 假 fetch：按 "METHOD /path"（不含 query）路由。handler 返回值作为信封里的 data；
 * 抛 `{ code, message? }` 表示业务错误信封；抛 `{ status }` 表示非 2xx；抛 Error 表示网络失败。
 */
export function fakeFetch(routes: Record<string, Handler>) {
  const calls: FakeCall[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input);
    const method = (init.method ?? 'GET').toUpperCase();
    const body = typeof init.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined;
    calls.push({ method, url, body, init });
    const key = `${method} ${url.split('?')[0]}`;
    const handler = routes[key];
    if (!handler) throw new Error(`unrouted ${key}`);
    let data: unknown;
    try {
      data = handler(body, url);
    } catch (e) {
      if (e instanceof Error) throw e;
      const err = e as { code?: number; status?: number; message?: string };
      if (err.status) return { ok: false, status: err.status, json: async () => ({}) } as Response;
      return { ok: true, status: 200, json: async () => ({ code: err.code, message: err.message ?? 'debug text' }) } as Response;
    }
    return { ok: true, status: 200, json: async () => ({ code: 0, data }) } as Response;
  });
  const of = (key: string) => calls.filter((c) => `${c.method} ${c.url.split('?')[0]}` === key);
  return { fn: fn as unknown as typeof fetch, calls, of };
}

export function session(over: Record<string, unknown> = {}) {
  return {
    enabled: true,
    conversation: null,
    messages: [],
    welcome: {
      text: 'hello',
      options: [
        { label: 'Install', value: 'install' },
        { label: 'Buy', value: 'purchase' },
      ],
    },
    ws: { url: 'wss://ws.example.test', token: 'tok-0' },
    ...over,
  };
}

let hidden = false;
/** 切换 document.hidden 并派发 visibilitychange。 */
export function setHidden(v: boolean) {
  hidden = v;
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
  document.dispatchEvent(new Event('visibilitychange'));
}

/** 服务端"没有访客主体"的信封（api/api_chat.go）。 */
export const NO_SUBJECT = { code: 422, message: 'no chat session' };
