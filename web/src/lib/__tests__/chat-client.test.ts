import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ChatError, createChatClient, newClientId, type ChatConversation, type ChatMessage } from '../chat-client';
import { FakeWS, fakeFetch, msg, session, setHidden } from './chat-test-fakes';

const tick = (ms = 0) => vi.advanceTimersByTimeAsync(ms);

function make(routes: Parameters<typeof fakeFetch>[0]) {
  const f = fakeFetch(routes);
  let n = 0;
  const client = createChatClient({ fetch: f.fn, WebSocket: FakeWS, randomId: () => `cid-${++n}`, random: () => 0.5 });
  const seen: ChatMessage[][] = [];
  client.onMessages((m) => seen.push(m));
  const last = () => seen[seen.length - 1] ?? [];
  const convs: (ChatConversation | null)[] = [];
  client.onConversation((c) => convs.push(c));
  return { client, f, seen, last, convs };
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
    client.activate();
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
    client.activate();
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

  it('send never retries a rate limit (code 429)', async () => {
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw { code: 429 }; },
    });
    await client.start('/p');
    client.activate();
    const err = await client.send('text', 'hi').catch((e) => e);
    expect(err).toBeInstanceOf(ChatError);
    expect((err as ChatError).kind).toBe('rate_limited');
    await tick(3000);
    expect(f.of('POST /api/chat/messages')).toHaveLength(1);
    expect(f.of('POST /api/chat/session')).toHaveLength(1);
    expect(last()).toEqual([]);
    client.stop();
  });

  it('a 422 on send (visitor subject lost) re-runs the session once and resends once with the same clientId', async () => {
    let sends = 0;
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => {
        if (++sends === 1) throw { code: 422 };
        return { message: msg(7, { senderType: 'visitor', content: 'hi' }), conversation: { uuid: 'u', status: 'open', handler: 'ai' } };
      },
    });
    await client.start('/p', { preview: true, resume: 'r' });
    await client.send('text', 'hi');
    const sessions = f.of('POST /api/chat/session');
    expect(sessions).toHaveLength(2);
    expect(sessions[1].body).toEqual({ path: '/p', preview: true }); // 令牌不重放
    const posts = f.of('POST /api/chat/messages');
    expect(posts).toHaveLength(2);
    expect(posts[0].body!.clientId).toBe(posts[1].body!.clientId);
    expect(last().map((m) => m.id)).toEqual([7]);
    client.stop();
  });

  it('a 422 that persists after the re-session is reported as invalid, with no further attempts', async () => {
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw { code: 422 }; },
    });
    await client.start('/p');
    const err = await client.send('text', 'hi').catch((e) => e);
    expect((err as ChatError).kind).toBe('invalid');
    await tick(10000);
    expect(f.of('POST /api/chat/messages')).toHaveLength(2);
    expect(f.of('POST /api/chat/session')).toHaveLength(2);
    expect(last()).toEqual([]);
    client.stop();
  });

  it.each([
    [429, 'rate_limited'],
    [503, 'network'],
    [404, 'invalid'],
  ])('maps a non-2xx HTTP status %i to %s', async (status, kind) => {
    const { client } = make({ 'POST /api/chat/session': () => { throw { status }; } });
    const err = await client.start('/p').catch((e) => e);
    expect(err).toBeInstanceOf(ChatError);
    expect((err as ChatError).kind).toBe(kind);
    expect((err as ChatError).code).toBe(status);
    expect(vi.getTimerCount()).toBe(0);
    client.stop();
  });

  it('a request that hangs is aborted after 15s and reported as a network error', async () => {
    const hang = ((_: RequestInfo | URL, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init!.signal!.addEventListener('abort', () => reject(new Error('aborted')));
      })) as unknown as typeof fetch;
    const client = createChatClient({ fetch: hang, WebSocket: FakeWS });
    const p = client.start('/p').catch((e) => e);
    await tick(14999);
    let settled = false;
    void p.then(() => { settled = true; });
    await tick(0);
    expect(settled).toBe(false);
    await tick(1);
    const err = await p;
    expect(err).toBeInstanceOf(ChatError);
    expect((err as ChatError).kind).toBe('network');
    expect(vi.getTimerCount()).toBe(0);
    client.stop();
  });

  it('lost response: when every POST fails but a catch-up shows the message arrived, send resolves', async () => {
    let arrived = false;
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ ws: null }),
      'GET /api/chat/messages': () => ({ messages: arrived ? [msg(9, { senderType: 'visitor', content: 'hi' })] : [] }),
      'POST /api/chat/messages': () => { arrived = true; throw new Error('response lost'); },
    });
    await client.start('/p');
    let ok = false;
    const p = client.send('text', 'hi').then(() => { ok = true; });
    await tick(7000);
    await p;
    expect(ok).toBe(true);
    expect(f.of('POST /api/chat/messages')).toHaveLength(4);
    expect(last().map((m) => [m.id, m.pending ?? false])).toEqual([[9, false]]);
    client.stop();
  });

  it('lost response: stops retrying as soon as the push channel confirms the message', async () => {
    const { client, f, last } = make({
      'POST /api/chat/session': () => session({ conversation: { uuid: 'u', status: 'open', handler: 'ai' } }),
      'GET /api/chat/messages': () => ({ messages: [] }),
      'POST /api/chat/messages': () => { throw new Error('response lost'); },
    });
    await client.start('/p');
    FakeWS.last().open();
    const p = client.send('text', 'hi');
    await tick(500);
    FakeWS.last().recv({ type: 'message', message: msg(9, { senderType: 'visitor', content: 'hi' }) });
    await tick(500);
    await p;
    expect(f.of('POST /api/chat/messages')).toHaveLength(1);
    expect(last().map((m) => m.id)).toEqual([9]);
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
    client.activate();
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
    client.activate();
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
    client.activate();
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
    client.activate();
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
    client.activate();
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
    client.activate();
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
    client.activate();
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
    const client = createChatClient({ fetch: fetchFn, WebSocket: FakeWS, randomId: () => 'c1', random: () => 0.5 });
    const seen: ChatMessage[][] = [];
    client.onMessages((m) => seen.push(m));
    await client.start('/p');
    client.activate();
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
    client.activate();
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
    a.client.activate();
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
    client.activate();
    await client.leaveEmail('a@b.co');
    expect(f.of('POST /api/chat/email')[0].body).toEqual({ email: 'a@b.co' });
    fail = true;
    await expect(client.leaveEmail('a@b.co')).rejects.toBeInstanceOf(ChatError);
    client.stop();
  });

  describe('deferred transport', () => {
    it('no conversation in the session → no socket, no timer, no listener until activate(); activate is idempotent', async () => {
      const listen = vi.spyOn(document, 'addEventListener');
      const { client } = make({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/messages': () => ({ messages: [] }),
      });
      await client.start('/p');
      await tick(60000);
      expect(FakeWS.instances).toHaveLength(0);
      expect(vi.getTimerCount()).toBe(0);
      expect(listen).not.toHaveBeenCalled();
      client.activate();
      client.activate();
      expect(FakeWS.instances).toHaveLength(1);
      listen.mockRestore();
      client.stop();
    });

    it('an existing conversation connects right away; activate() before the session resolves waits for it', async () => {
      const a = make({ 'POST /api/chat/session': () => session({ conversation: { uuid: 'u', status: 'open', handler: 'ai' } }) });
      await a.client.start('/p');
      expect(FakeWS.instances).toHaveLength(1);
      a.client.stop();

      FakeWS.reset();
      const b = make({ 'POST /api/chat/session': () => session() });
      const p = b.client.start('/p');
      b.client.activate();
      expect(FakeWS.instances).toHaveLength(0);
      await p;
      expect(FakeWS.instances).toHaveLength(1);
      b.client.stop();
    });

    it('calling start() again closes the previous socket and timers first', async () => {
      const { client } = make({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/ws-token': () => ({ token: 't' }),
        'GET /api/chat/messages': () => ({ messages: [] }),
      });
      await client.start('/p');
      client.activate();
      const first = FakeWS.last();
      first.open();
      await tick(0);
      await client.start('/p');
      expect(first.closed).toBe(true);
      expect(FakeWS.live()).toHaveLength(1);
      await tick(60000);
      expect(FakeWS.instances).toHaveLength(2);

      // 轮询模式下重复 start 也只留一个轮询定时器
      const b = make({
        'POST /api/chat/session': () => session({ ws: null }),
        'GET /api/chat/messages': () => ({ messages: [] }),
      });
      await b.client.start('/p');
      b.client.activate();
      await b.client.start('/p');
      await tick(4000);
      expect(b.f.of('GET /api/chat/messages')).toHaveLength(1);
      b.client.stop();
      client.stop();
    });
  });

  describe('connection failures', () => {
    it('a WebSocket constructor that throws does not reject start(); three failures fall back to polling', async () => {
      let built = 0;
      class ThrowWS {
        constructor() { built++; throw new SyntaxError('bad url'); }
      }
      const f = fakeFetch({
        'POST /api/chat/session': () => session({ conversation: { uuid: 'u', status: 'open', handler: 'ai' } }),
        'GET /api/chat/ws-token': () => ({ token: 't' }),
        'GET /api/chat/messages': () => ({ messages: [] }),
      });
      const client = createChatClient({ fetch: f.fn, WebSocket: ThrowWS as never, random: () => 0.5 });
      const state = await client.start('/p');
      expect(state.enabled).toBe(true);
      expect(built).toBe(1);
      await tick(1000);
      expect(built).toBe(2);
      await tick(2000);
      expect(built).toBe(3);
      expect(f.of('GET /api/chat/messages')).toHaveLength(0);
      await tick(4000);
      expect(f.of('GET /api/chat/messages')).toHaveLength(1);
      await tick(60000);
      expect(built).toBe(3);
      client.stop();
    });

    it.each([
      [0, 750],
      [0.999999, 1250],
    ])('reconnect delay carries ±25%% jitter (random=%f → %ims)', async (rnd, expected) => {
      const f = fakeFetch({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/ws-token': () => ({ token: 't' }),
        'GET /api/chat/messages': () => ({ messages: [] }),
      });
      const client = createChatClient({ fetch: f.fn, WebSocket: FakeWS, random: () => rnd });
      await client.start('/p');
      client.activate();
      FakeWS.last().drop();
      await tick(expected - 1);
      expect(FakeWS.instances).toHaveLength(1);
      await tick(1);
      expect(FakeWS.instances).toHaveLength(2);
      client.stop();
    });
  });

  describe('conversation state comes from the server', () => {
    const open = { uuid: 'u1', status: 'open', handler: 'ai' };
    const human = { uuid: 'u1', status: 'open', handler: 'human' };

    it('session → POST response → ws state frame (bare and enveloped); unchanged states do not notify', async () => {
      const { client, convs } = make({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/messages': () => ({ messages: [], conversation: open }),
        'POST /api/chat/messages': () => ({ message: msg(1, { senderType: 'visitor', content: 'hi' }), conversation: open }),
      });
      await client.start('/p');
      expect(convs).toEqual([]); // 还没有会话：null → null 不通知
      client.activate();
      await client.send('text', 'hi');
      expect(convs).toEqual([open]);
      const ws = FakeWS.last();
      ws.recv({ type: 'state', conversation: human });
      ws.recv({ type: 'state', conversation: human });
      expect(convs).toEqual([open, human]);
      ws.recv({ channel: 'c', timestamp: 1, payload: { type: 'state', conversation: { ...human, status: 'closed' } } });
      expect(convs).toHaveLength(3);
      expect(convs[2]).toEqual({ uuid: 'u1', status: 'closed', handler: 'human' });
      ws.recv({ type: 'state', conversation: 'garbage' });
      expect(convs).toHaveLength(3);
      client.stop();
    });

    it('a catch-up with zero new messages still delivers a state change; null clears it', async () => {
      let conv: unknown = human;
      const { client, convs, seen } = make({
        'POST /api/chat/session': () => session({ ws: null, conversation: open }),
        'GET /api/chat/messages': () => ({ messages: [], conversation: conv }),
      });
      await client.start('/p');
      expect(convs).toEqual([open]);
      const emitted = seen.length;
      await tick(4000);
      expect(convs).toEqual([open, human]);
      expect(seen.length).toBe(emitted);
      conv = null;
      await tick(4000);
      expect(convs).toEqual([open, human, null]);
      client.stop();
    });

    it('an older server that omits `conversation` on GET /messages leaves the last known state alone', async () => {
      const { client, convs } = make({
        'POST /api/chat/session': () => session({ ws: null, conversation: human }),
        'GET /api/chat/messages': () => ({ messages: [msg(3)] }),
      });
      await client.start('/p');
      await tick(8000);
      expect(convs).toEqual([human]);
      client.stop();
    });

    it('polling that hits 422 (subject lost) re-runs the session exactly once until a poll succeeds again', async () => {
      let broken = true;
      const { client, f } = make({
        'POST /api/chat/session': () => session({ ws: null, conversation: open }),
        'GET /api/chat/messages': () => { if (broken) throw { code: 422 }; return { messages: [] }; },
      });
      await client.start('/p');
      await tick(4000);
      expect(f.of('POST /api/chat/session')).toHaveLength(2);
      await tick(12000);
      expect(f.of('POST /api/chat/session')).toHaveLength(2);
      broken = false;
      await tick(4000);
      broken = true;
      await tick(4000);
      expect(f.of('POST /api/chat/session')).toHaveLength(3);
      client.stop();
    });
  });

  describe('newClientId', () => {
    afterEach(() => vi.unstubAllGlobals());
    const V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

    it('falls back to getRandomValues when crypto.randomUUID is missing (old Safari / Chrome)', () => {
      const getRandomValues = vi.fn((a: Uint8Array) => { a.forEach((_, i) => { a[i] = (i * 37 + 11) & 0xff; }); return a; });
      vi.stubGlobal('crypto', { getRandomValues });
      const id = newClientId();
      expect(getRandomValues).toHaveBeenCalledTimes(1);
      expect(id).toMatch(V4);
      expect(id.length).toBeLessThanOrEqual(36);
    });

    it('still produces distinct valid ids with no crypto at all', () => {
      vi.stubGlobal('crypto', undefined);
      const a = newClientId();
      expect(a).toMatch(V4);
      expect(newClientId()).not.toBe(a);
    });

    it('send works end to end without crypto.randomUUID', async () => {
      vi.stubGlobal('crypto', { getRandomValues: (a: Uint8Array) => a.fill(7) });
      const f = fakeFetch({
        'POST /api/chat/session': () => session({ ws: null }),
        'GET /api/chat/messages': () => ({ messages: [] }),
        'POST /api/chat/messages': (body) => ({ message: msg(5, { senderType: 'visitor', content: String(body!.content) }), conversation: null }),
      });
      const client = createChatClient({ fetch: f.fn, WebSocket: FakeWS });
      await client.start('/p');
      await client.send('text', 'hi');
      expect(String(f.of('POST /api/chat/messages')[0].body!.clientId)).toMatch(V4);
      client.stop();
    });
  });
});
