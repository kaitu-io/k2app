import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ChatError, createChatClient, deriveConversation, type ChatMessage } from '../chat-client';
import { FakeWS, fakeFetch, msg, session, setHidden } from './chat-test-fakes';

const tick = (ms = 0) => vi.advanceTimersByTimeAsync(ms);

function make(routes: Parameters<typeof fakeFetch>[0]) {
  const f = fakeFetch(routes);
  let n = 0;
  const client = createChatClient({ fetch: f.fn, WebSocket: FakeWS, randomId: () => `cid-${++n}` });
  const seen: ChatMessage[][] = [];
  client.onMessages((m) => seen.push(m));
  const last = () => seen[seen.length - 1] ?? [];
  return { client, f, seen, last };
}

describe('chat-client', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWS.reset();
    setHidden(false);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('talks to same-origin /api/chat with credentials and passes path/preview/resume', async () => {
    const { client, f } = make({ 'POST /api/chat/session': () => session({ ws: null }) });
    const s = await client.start('/zh-CN/pricing', { preview: true, resume: 'r-tok' });
    expect(s.enabled).toBe(true);
    const call = f.of('POST /api/chat/session')[0];
    expect(call.url).toBe('/api/chat/session');
    expect(call.init.credentials).toBe('include');
    expect(call.body).toEqual({ path: '/zh-CN/pricing', preview: true, resume: 'r-tok' });
    client.stop();
  });

  it('enabled:false opens no socket and starts no polling', async () => {
    const { client } = make({ 'POST /api/chat/session': () => ({ enabled: false, messages: [] }) });
    const s = await client.start('/p');
    expect(s.enabled).toBe(false);
    expect(FakeWS.instances).toHaveLength(0);
    expect(vi.getTimerCount()).toBe(0);
    client.stop();
  });

  it('send retries network failures with the SAME clientId (backoff 1s/2s)', async () => {
    let attempt = 0;
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': (body) => {
        attempt++;
        if (attempt < 3) throw new Error('offline');
        return {
          message: msg(11, { senderType: 'visitor', content: String(body!.content) }),
          conversation: { uuid: 'u', status: 'open', handler: 'ai' },
        };
      },
    });
    await client.start('/p');
    let done = false;
    const p = client.send('text', 'hi').then(() => { done = true; });
    await tick(0);
    expect(f.of('POST /api/chat/messages')).toHaveLength(1);
    expect(last().map((m) => [m.content, m.pending])).toEqual([['hi', true]]);
    await tick(999);
    expect(f.of('POST /api/chat/messages')).toHaveLength(1);
    await tick(1);
    expect(f.of('POST /api/chat/messages')).toHaveLength(2);
    await tick(2000);
    await p;
    expect(done).toBe(true);
    const sends = f.of('POST /api/chat/messages');
    expect(sends).toHaveLength(3);
    const ids = sends.map((c) => c.body!.clientId);
    expect(ids[0]).toBe('cid-1');
    expect(new Set(ids).size).toBe(1);
    expect(sends[0].body).toEqual({ kind: 'text', content: 'hi', clientId: 'cid-1' });
    expect(last().map((m) => [m.id, m.pending ?? false])).toEqual([[11, false]]);
    client.stop();
  });

  it('send also retries a 5xx envelope, then gives up after the 1s/2s/4s backoffs and drops the bubble', async () => {
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw { code: 500 }; },
    });
    await client.start('/p');
    const p = client.send('text', 'hi').catch((e) => e);
    await tick(7000);
    const err = await p;
    expect(err).toBeInstanceOf(ChatError);
    expect((err as ChatError).kind).toBe('network');
    expect(f.of('POST /api/chat/messages')).toHaveLength(4);
    expect(new Set(f.of('POST /api/chat/messages').map((c) => c.body!.clientId)).size).toBe(1);
    expect(last()).toEqual([]);
    client.stop();
  });

  it.each([
    [429, 'rate_limited'],
    [422, 'invalid'],
  ])('send never retries code %i (%s)', async (code, kind) => {
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw { code }; },
    });
    await client.start('/p');
    const err = await client.send('text', 'hi').catch((e) => e);
    expect(err).toBeInstanceOf(ChatError);
    expect((err as ChatError).kind).toBe(kind);
    await tick(10000);
    expect(f.of('POST /api/chat/messages')).toHaveLength(1);
    expect(last()).toEqual([]);
    client.stop();
  });

  it('connects to ws.url with the session token; after a drop refreshes the token and catches up from the MAX id', async () => {
    let tok = 0;
    const { client, f } = make({
      'POST /api/chat/session': () => session({ messages: [msg(3)] }),
      'GET /api/chat/ws-token': () => ({ token: `tok-${++tok}` }),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await client.start('/p');
    expect(FakeWS.instances).toHaveLength(1);
    expect(FakeWS.last().url).toBe('wss://ws.example.test/api/chat/ws?token=tok-0');
    FakeWS.last().open();
    await tick(0);
    expect(f.of('GET /api/chat/messages').map((c) => c.url)).toEqual(['/api/chat/messages?after=3']);

    // 乱序到达：游标必须是最大 id，而不是最后收到的那条
    FakeWS.last().recv({ type: 'message', message: msg(5) });
    FakeWS.last().recv({ type: 'message', message: msg(9) });
    FakeWS.last().recv({ type: 'message', message: msg(7) });
    FakeWS.last().drop();
    await tick(999);
    expect(FakeWS.instances).toHaveLength(1);
    await tick(1);
    expect(f.of('GET /api/chat/ws-token')).toHaveLength(1);
    expect(FakeWS.instances).toHaveLength(2);
    expect(FakeWS.last().url).toBe('wss://ws.example.test/api/chat/ws?token=tok-1');
    FakeWS.last().open();
    await tick(0);
    expect(f.of('GET /api/chat/messages').map((c) => c.url)).toEqual([
      '/api/chat/messages?after=3',
      '/api/chat/messages?after=9',
    ]);
    client.stop();
  });

  it('delivers each id once, ascending, whatever the transport (ws twice + catch-up + broadcast envelope); notes never surface', async () => {
    const { client, last } = make({
      'POST /api/chat/session': () => session({ messages: [msg(2)] }),
      'GET /api/chat/messages': () => ({ messages: [msg(4), msg(6)] }),
    });
    await client.start('/p');
    const ws = FakeWS.last();
    ws.recv({ type: 'message', message: msg(6) });
    ws.recv({ type: 'message', message: msg(6) });
    // 广播实现的外层信封 {channel,timestamp,payload}
    ws.recv({ channel: 'c', timestamp: 1, payload: { type: 'message', message: msg(5) } });
    ws.recv({ type: 'message', message: msg(8, { kind: 'note' as ChatMessage['kind'] }) });
    ws.recv({ type: 'other' });
    ws.onmessage?.({ data: 'not json' });
    ws.open();
    await tick(0);
    expect(last().map((m) => m.id)).toEqual([2, 4, 5, 6]);
    client.stop();
  });

  it('ws:null → polls GET /messages?after= every 4s', async () => {
    let round = 0;
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null, messages: [msg(1)] }),
      'GET /api/chat/messages': () => ({ messages: ++round === 1 ? [msg(2)] : [] }),
    });
    await client.start('/p');
    expect(FakeWS.instances).toHaveLength(0);
    await tick(3999);
    expect(f.of('GET /api/chat/messages')).toHaveLength(0);
    await tick(1);
    expect(f.of('GET /api/chat/messages').map((c) => c.url)).toEqual(['/api/chat/messages?after=1']);
    await tick(4000);
    expect(f.of('GET /api/chat/messages').map((c) => c.url)).toEqual([
      '/api/chat/messages?after=1',
      '/api/chat/messages?after=2',
    ]);
    expect(last().map((m) => m.id)).toEqual([1, 2]);
    client.stop();
  });

  it('polling pauses while the tab is hidden and resumes with an immediate poll', async () => {
    const { client, f } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await client.start('/p');
    setHidden(true);
    await tick(20000);
    expect(f.of('GET /api/chat/messages')).toHaveLength(0);
    setHidden(false);
    await tick(0);
    expect(f.of('GET /api/chat/messages')).toHaveLength(1);
    await tick(4000);
    expect(f.of('GET /api/chat/messages')).toHaveLength(2);
    client.stop();
  });

  it('ws mode: becoming visible triggers exactly one catch-up fetch', async () => {
    const { client, f } = make({
      'POST /api/chat/session': () => session(),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await client.start('/p');
    FakeWS.last().open();
    await tick(0);
    expect(f.of('GET /api/chat/messages')).toHaveLength(1);
    setHidden(true);
    await tick(60000);
    expect(f.of('GET /api/chat/messages')).toHaveLength(1);
    setHidden(false);
    await tick(0);
    expect(f.of('GET /api/chat/messages')).toHaveLength(2);
    client.stop();
  });

  it('three consecutive failed connects (1s, 2s apart) fall back to polling for good', async () => {
    const { client, f } = make({
      'POST /api/chat/session': () => session(),
      'GET /api/chat/ws-token': () => ({ token: 't' }),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await client.start('/p');
    FakeWS.last().drop(); // 1
    await tick(1000);
    expect(FakeWS.instances).toHaveLength(2);
    FakeWS.last().drop(); // 2
    await tick(1999);
    expect(FakeWS.instances).toHaveLength(2);
    await tick(1);
    expect(FakeWS.instances).toHaveLength(3);
    FakeWS.last().drop(); // 3 → 轮询
    expect(f.of('GET /api/chat/messages')).toHaveLength(0);
    await tick(4000);
    expect(f.of('GET /api/chat/messages')).toHaveLength(1);
    await tick(60000);
    expect(FakeWS.instances).toHaveLength(3);
    client.stop();
  });

  it('a flapping connection backs off exponentially and caps at 30s', async () => {
    const { client } = make({
      'POST /api/chat/session': () => session(),
      'GET /api/chat/ws-token': () => ({ token: 't' }),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await client.start('/p');
    const delays = [1000, 2000, 4000, 8000, 16000, 30000, 30000];
    for (const [i, d] of delays.entries()) {
      FakeWS.last().open(); // 连上即断：不算"连不上"，但也不重置退避
      FakeWS.last().drop();
      await tick(d - 1);
      expect(FakeWS.instances, `delay #${i}`).toHaveLength(i + 1);
      await tick(1);
      expect(FakeWS.instances, `delay #${i}`).toHaveLength(i + 2);
    }
    // 稳定连接 10s 以上后再断：退避从 1s 重新开始
    FakeWS.last().open();
    await tick(10000);
    FakeWS.last().drop();
    const n = FakeWS.instances.length;
    await tick(1000);
    expect(FakeWS.instances).toHaveLength(n + 1);
    client.stop();
  });

  it('optimistic bubble is reconciled without duplicates when the same message also arrives over ws first', async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => { release = r; });
    const f = fakeFetch({
      'POST /api/chat/session': () => session(),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    const sent = msg(42, { senderType: 'visitor', content: 'hi' });
    const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === '/api/chat/messages' && init?.method === 'POST') {
        await gate;
        return { ok: true, status: 200, json: async () => ({ code: 0, data: { message: sent, conversation: null } }) } as Response;
      }
      return f.fn(input, init);
    }) as typeof fetch;
    const client = createChatClient({ fetch: fetchFn, WebSocket: FakeWS, randomId: () => 'c1' });
    const seen: ChatMessage[][] = [];
    client.onMessages((m) => seen.push(m));
    await client.start('/p');
    FakeWS.last().open();
    await tick(0);

    const p = client.send('text', 'hi');
    await tick(0);
    expect(seen[seen.length - 1].map((m) => [m.id, m.pending ?? false, m.clientId])).toEqual([[0, true, 'c1']]);

    FakeWS.last().recv({ type: 'message', message: sent });
    expect(seen[seen.length - 1].map((m) => [m.id, m.pending ?? false])).toEqual([[42, false]]);

    release();
    await p;
    // 之后同一条再经补齐/广播到达也不重复
    FakeWS.last().recv({ type: 'message', message: sent });
    expect(seen[seen.length - 1].map((m) => m.id)).toEqual([42]);
    client.stop();
  });

  it('stop() closes the socket without reconnecting, clears every timer, and silences callbacks', async () => {
    const { client, seen, f } = make({
      'POST /api/chat/session': () => session(),
      'GET /api/chat/ws-token': () => ({ token: 't' }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw new Error('offline'); },
    });
    await client.start('/p');
    const ws = FakeWS.last();
    ws.open();
    await tick(0);
    void client.send('text', 'x').catch(() => {}); // 留一个重试定时器
    await tick(0);
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    const before = seen.length;
    const fetches = f.calls.length;

    client.stop();
    expect(ws.closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    ws.onmessage?.({ data: JSON.stringify({ type: 'message', message: msg(99) }) });
    setHidden(true);
    setHidden(false);
    await tick(120000);
    expect(FakeWS.instances).toHaveLength(1);
    expect(seen.length).toBe(before);
    expect(f.calls.length).toBe(fetches);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('stop() while polling leaves no timers; stop() before start resolves opens nothing', async () => {
    const a = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
    });
    await a.client.start('/p');
    expect(vi.getTimerCount()).toBe(1);
    a.client.stop();
    expect(vi.getTimerCount()).toBe(0);

    const b = make({ 'POST /api/chat/session': () => session() });
    const listen = vi.spyOn(document, 'addEventListener');
    const p = b.client.start('/p');
    b.client.stop();
    await p;
    expect(listen).not.toHaveBeenCalled(); // 停止后不再挂 visibilitychange 监听
    listen.mockRestore();
    await tick(60000);
    expect(FakeWS.instances).toHaveLength(0);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('leaveEmail posts the address and surfaces failures as ChatError', async () => {
    let fail = false;
    const { client, f } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'POST /api/chat/email': () => { if (fail) throw { code: 422 }; return undefined; },
    });
    await client.start('/p');
    await client.leaveEmail('a@b.co');
    expect(f.of('POST /api/chat/email')[0].body).toEqual({ email: 'a@b.co' });
    fail = true;
    await expect(client.leaveEmail('a@b.co')).rejects.toBeInstanceOf(ChatError);
    client.stop();
  });
});

describe('deriveConversation', () => {
  const ev = (id: number, event: string) => msg(id, { senderType: 'system', kind: 'event', meta: { event } });
  const visitor = (id: number) => msg(id, { senderType: 'visitor' });

  it('no conversation and no messages → null', () => {
    expect(deriveConversation(null, [])).toBeNull();
  });
  it('first visitor message opens an ai conversation', () => {
    expect(deriveConversation(null, [visitor(1)])).toEqual({ status: 'open', handler: 'ai' });
  });
  it('keeps the session state when there are no events', () => {
    expect(deriveConversation({ status: 'open', handler: 'human' }, [visitor(1)])).toEqual({ status: 'open', handler: 'human' });
  });
  it('follows transfer / hand-back / close events in id order', () => {
    expect(deriveConversation(null, [visitor(1), ev(2, 'transfer_human')])).toEqual({ status: 'open', handler: 'human' });
    expect(deriveConversation(null, [visitor(1), ev(2, 'transfer_human'), ev(3, 'handed_to_ai')])).toEqual({ status: 'open', handler: 'ai' });
    expect(deriveConversation(null, [visitor(1), ev(2, 'transfer_human'), ev(3, 'closed')])?.status).toBe('closed');
    expect(deriveConversation(null, [visitor(1), ev(2, 'auto_closed')])?.status).toBe('closed');
  });
  it('a visitor message after close starts a fresh ai conversation; optimistic bubbles do not count', () => {
    expect(deriveConversation(null, [visitor(1), ev(2, 'transfer_human'), ev(3, 'closed'), visitor(4)])).toEqual({ status: 'open', handler: 'ai' });
    const pending = { ...visitor(0), pending: true };
    expect(deriveConversation(null, [visitor(1), ev(3, 'closed'), pending])?.status).toBe('closed');
  });
});
