import { StrictMode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import ChatWidget from '../chat/ChatWidget';
import { CHAT_EMAIL_FLAG, CHAT_KNOWN_FLAG, hasResumeToken, requestOpenChat, shouldProbeSession, takeResumeToken } from '../chat/gate';
import { CHAT_RESUME_GLOBAL, CHAT_RESUME_HASH_PREFIX, CHAT_RESUME_SCRIPT, stripChatResume } from '../chat/resume-script';
import {
  ChatError,
  createChatClient,
  type ChatClient,
  type ChatConversation,
  type ChatMessage,
  type SessionState,
} from '@/lib/chat-client';
import { COOKIE_BANNER_OFFSET_VAR } from '@/lib/cookie-banner';
import { FakeWS, fakeFetch, msg, session, setHidden } from '@/lib/__tests__/chat-test-fakes';

const flush = (ms = 0) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

function fakeClient(state: Partial<SessionState> = {}) {
  const listeners = new Set<(m: ChatMessage[]) => void>();
  const convListeners = new Set<(c: ChatConversation | null) => void>();
  const full = { ...(session() as unknown as SessionState), ...state };
  const client = {
    start: vi.fn(async () => {
      if (full.conversation) convListeners.forEach((cb) => cb(full.conversation));
      listeners.forEach((cb) => cb(full.messages));
      return full;
    }),
    activate: vi.fn(),
    onConversation: (cb: (c: ChatConversation | null) => void) => {
      convListeners.add(cb);
      return () => { convListeners.delete(cb); };
    },
    send: vi.fn(async () => {}),
    leaveEmail: vi.fn(async () => {}),
    onMessages: (cb: (m: ChatMessage[]) => void) => {
      listeners.add(cb);
      return () => { listeners.delete(cb); };
    },
    stop: vi.fn(() => { listeners.clear(); convListeners.clear(); }),
  } satisfies ChatClient;
  const push = (m: ChatMessage[]) => act(() => { listeners.forEach((cb) => cb(m)); });
  const pushConv = (c: ChatConversation | null) => act(() => { convListeners.forEach((cb) => cb(c)); });
  return { client, push, pushConv, create: vi.fn(() => client) };
}

const visit = (url: string) => window.history.pushState({}, '', url);
const launcher = () => screen.queryByRole('button', { name: 'launcher' });
const dialog = () => screen.queryByRole('dialog');
const input = () => screen.getByPlaceholderText('placeholder') as HTMLTextAreaElement;

async function mountOpen(fc: ReturnType<typeof fakeClient>) {
  const utils = render(<ChatWidget createClient={fc.create} />);
  await flush();
  fireEvent.click(launcher()!);
  await flush();
  return utils;
}

describe('ChatWidget', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    localStorage.clear();
    sessionStorage.clear();
    delete (window as unknown as Record<string, unknown>).__chatResume;
    FakeWS.reset();
    setHidden(false);
    visit('/zh-CN/pricing?chat=preview');
    delete (window as unknown as Record<string, unknown>).$chatwoot;
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
  });

  describe('gating', () => {
    it('renders nothing and creates no client when the brand has chat off', async () => {
      vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
      const fc = fakeClient();
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(container.innerHTML).toBe('');
      expect(fc.create).not.toHaveBeenCalled();
    });

    it.each(['/zh-CN/pricing?chat=preview&embed=true', '/zh-CN/pricing?chat=preview#embed'])(
      'renders nothing in embed mode: %s',
      async (url) => {
        visit(url);
        const fc = fakeClient();
        const { container } = render(<ChatWidget createClient={fc.create} />);
        await flush();
        expect(container.innerHTML).toBe('');
        expect(fc.create).not.toHaveBeenCalled();
      },
    );

    it('dark launch: without ?chat= and without the known-browser flag nothing renders and no request is made', async () => {
      visit('/zh-CN/pricing');
      const fc = fakeClient();
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(container.innerHTML).toBe('');
      expect(fc.create).not.toHaveBeenCalled();
    });

    it('renders nothing when the server says enabled:false, and does not mark the browser as known', async () => {
      const fc = fakeClient({ enabled: false });
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/pricing', { preview: true });
      expect(container.innerHTML).toBe('');
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBeNull();
    });

    it('renders nothing when the session call fails', async () => {
      const fc = fakeClient();
      fc.client.start.mockRejectedValue(new ChatError('network', 0));
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(container.innerHTML).toBe('');
    });

    it('?chat=preview → preview session, launcher shown (panel closed), param kept, browser marked known', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/pricing', { preview: true });
      expect(launcher()).not.toBeNull();
      expect(dialog()).toBeNull();
      expect(window.location.search).toBe('?chat=preview');
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBe('1');
    });

    it('a known browser probes the session without preview', async () => {
      visit('/zh-CN/purchase');
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/purchase', {});
      expect(launcher()).not.toBeNull();
    });

    it('preview survives navigation within the tab (sessionStorage), without the query param', async () => {
      const first = fakeClient();
      const { unmount } = render(<ChatWidget createClient={first.create} />);
      await flush();
      unmount();
      visit('/zh-CN/purchase');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/purchase', { preview: true });
    });

    it('#chat=<token> is a resume token: passed as resume, panel auto-opens, fragment removed, query kept', async () => {
      visit('/zh-CN/support?utm_source=mail#chat=abc.DEF-123');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      expect(window.location.hash).toBe('');
      expect(window.location.search).toBe('?utm_source=mail');
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/support', { resume: 'abc.DEF-123' });
      expect(dialog()).not.toBeNull();
      expect(window.location.href).not.toContain('abc.DEF-123');
    });

    it('picks up the token the inline script stashed on window, and removes it', async () => {
      visit('/zh-CN/support?chat=preview');
      (window as unknown as Record<string, unknown>).__chatResume = 'stashed.tok';
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/support', { preview: true, resume: 'stashed.tok' });
      expect('__chatResume' in window).toBe(false);
      expect(dialog()).not.toBeNull();
    });

    it('a non-preview ?chat= query value is ignored: not a resume token, not a reason to probe', async () => {
      visit('/zh-CN/support?chat=abc.DEF-123');
      const fc = fakeClient();
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.create).not.toHaveBeenCalled();
      expect(container.innerHTML).toBe('');

      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      const known = fakeClient();
      render(<ChatWidget createClient={known.create} />);
      await flush();
      expect(known.client.start).toHaveBeenCalledWith('/zh-CN/support', {});
      expect(dialog()).toBeNull();
    });

    it('strict mode still passes the resume token on the second mount after it was taken', async () => {
      visit('/zh-CN/support#chat=tok1');
      const fc = fakeClient();
      render(<StrictMode><ChatWidget createClient={fc.create} /></StrictMode>);
      await flush();
      expect(window.location.hash).toBe('');
      expect(fc.client.start).toHaveBeenLastCalledWith('/zh-CN/support', { resume: 'tok1' });
      expect(dialog()).not.toBeNull();
    });

    it('shouldProbeSession: preview, a pending resume token, or the known flag — nothing else', () => {
      visit('/a');
      expect(shouldProbeSession()).toBe(false);
      visit('/a?chat=');
      expect(shouldProbeSession()).toBe(false);
      visit('/a?chat=tok&x=1#contact');
      expect(shouldProbeSession()).toBe(false);
      visit('/a#chat=tok');
      expect(shouldProbeSession()).toBe(true);
      expect(hasResumeToken()).toBe(true); // 判断不取走
      expect(takeResumeToken()).toBe('tok');
      expect(takeResumeToken()).toBeNull();
      expect(shouldProbeSession()).toBe(false);
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      expect(shouldProbeSession()).toBe(true);
      localStorage.clear();
      visit('/a?chat=preview');
      expect(shouldProbeSession()).toBe(true);
    });

    describe('inline resume script (root layout, before analytics)', () => {
      const run = () => new Function(CHAT_RESUME_SCRIPT)();
      const stash = () => (window as unknown as Record<string, unknown>).__chatResume;

      it('moves #chat=<token> onto window and strips the fragment, keeping path and query', () => {
        visit('/zh-CN/support?utm_source=mail#chat=abc.DEF-123');
        run();
        expect(stash()).toBe('abc.DEF-123');
        expect(window.location.pathname + window.location.search).toBe('/zh-CN/support?utm_source=mail');
        expect(window.location.href).not.toContain('abc.DEF-123');
        expect(takeResumeToken()).toBe('abc.DEF-123');
        expect(stash()).toBeUndefined();
      });

      // 内联脚本不经转译，直接进 HTML：必须是老浏览器也能解析的写法，并与 TS 版行为一致。
      it('is hand-written ES5 (no const/let, arrow functions or optional catch binding)', () => {
        expect(CHAT_RESUME_SCRIPT).not.toMatch(/\b(const|let)\b|=>|catch\s*\{|`/);
        expect(CHAT_RESUME_SCRIPT).toContain(`"${CHAT_RESUME_HASH_PREFIX}"`);
        expect(CHAT_RESUME_SCRIPT).toContain(`.${CHAT_RESUME_GLOBAL}=`);
        expect(CHAT_RESUME_SCRIPT).toContain(`.slice(${CHAT_RESUME_HASH_PREFIX.length})`);
      });

      it.each([
        ['#chat=t.1', '/p', '?a=1', false],
        ['#chat=abc.DEF-123_x%2B', '/zh-CN/support', '', false],
        ['#chat=', '/p', '', false],
        ['#contact', '/p', '?chat=tok', false],
        ['', '/p', '', false],
        ['#x#chat=t', '/p', '', false],
        ['#chat=t', '/p', '?a=1', true],
      ])('behaves exactly like stripChatResume for hash %j (path %s%s, history throws: %s)', (hash, pathname, search, throws) => {
        const host = () => {
          const calls: unknown[][] = [];
          const h = {
            location: { hash, pathname, search },
            history: {
              replaceState: (...args: unknown[]) => {
                calls.push(args);
                if (throws) throw new Error('SecurityError');
              },
            },
          };
          return { h, calls };
        };
        const ts = host();
        stripChatResume(ts.h);
        const inline = host();
        expect(() => new Function('window', CHAT_RESUME_SCRIPT)(inline.h)).not.toThrow();
        expect(inline.calls).toEqual(ts.calls);
        expect((inline.h as unknown as Record<string, unknown>)[CHAT_RESUME_GLOBAL]).toEqual(
          (ts.h as unknown as Record<string, unknown>)[CHAT_RESUME_GLOBAL],
        );
        expect(Object.keys(inline.h).sort()).toEqual(Object.keys(ts.h).sort());
      });

      it('stripChatResume literals match the exported constants', () => {
        const calls: unknown[][] = [];
        const host = {
          location: { hash: `${CHAT_RESUME_HASH_PREFIX}t.1`, pathname: '/p', search: '?a=1' },
          history: { replaceState: (...args: unknown[]) => { calls.push(args); } },
        };
        stripChatResume(host);
        expect((host as unknown as Record<string, unknown>)[CHAT_RESUME_GLOBAL]).toBe('t.1');
        expect(calls).toEqual([[null, '', '/p?a=1']]);
      });

      it('stripChatResume never throws when the history API refuses', () => {
        const host = {
          location: { hash: '#chat=t', pathname: '/p', search: '' },
          history: { replaceState: () => { throw new Error('SecurityError'); } },
        };
        expect(() => stripChatResume(host)).not.toThrow();
      });

      it.each(['/zh-CN/support', '/zh-CN/support#contact', '/zh-CN/support?chat=tok#embed'])('leaves %s alone', (url) => {
        visit(url);
        const before = window.location.href;
        run();
        expect(stash()).toBeUndefined();
        expect(window.location.href).toBe(before);
      });
    });
  });

  // Cookie 同意横幅（z-[9999]）占着同一个角：入口与面板都以它公布的高度为底边，不被它盖住，也不盖它。
  describe('stays clear of the cookie-consent banner', () => {
    const lifted = (el: Element) => el.className.split(/\s+/).filter((c) => c.includes(`var(${COOKIE_BANNER_OFFSET_VAR},0px)`));

    it('the launcher is positioned above the banner offset, with no fixed bottom of its own', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      const classes = launcher()!.className.split(/\s+/);
      expect(lifted(launcher()!)).toEqual([`bottom-[calc(var(${COOKIE_BANNER_OFFSET_VAR},0px)+1rem)]`]);
      expect(classes.filter((c) => /^bottom-\d/.test(c))).toEqual([]);
      // 层级不动：不靠盖过横幅来解决
      expect(classes).toContain('z-50');
    });

    it('the open panel rests on the banner too and gives up that much height', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      const classes = dialog()!.className.split(/\s+/);
      expect(lifted(dialog()!).sort()).toEqual(
        [
          `bottom-[var(${COOKIE_BANNER_OFFSET_VAR},0px)]`,
          `max-h-[calc(85dvh-var(${COOKIE_BANNER_OFFSET_VAR},0px))]`,
          `min-[480px]:bottom-[calc(var(${COOKIE_BANNER_OFFSET_VAR},0px)+1rem)]`,
        ].sort(),
      );
      expect(classes.filter((c) => /(^|:)bottom-\d/.test(c))).toEqual([]);
      expect(classes).toContain('z-50');
    });
  });

  describe('lifecycle', () => {
    it('no connection until the panel is opened; strict-mode double mount then yields exactly one live socket; unmount leaves none', async () => {
      const f = fakeFetch({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/messages': () => ({ messages: [] }),
        'GET /api/chat/ws-token': () => ({ token: 't' }),
      });
      const create = () => createChatClient({ fetch: f.fn, WebSocket: FakeWS });
      const { unmount } = render(<StrictMode><ChatWidget createClient={create} /></StrictMode>);
      await flush(60000);
      expect(f.of('POST /api/chat/session')).toHaveLength(2); // 严格模式挂载两次
      expect(launcher()).not.toBeNull();
      expect(FakeWS.instances).toHaveLength(0);
      expect(vi.getTimerCount()).toBe(0);

      fireEvent.click(launcher()!);
      await flush();
      expect(FakeWS.instances).toHaveLength(1);
      FakeWS.last().open();
      await flush();
      fireEvent.keyDown(dialog()!, { key: 'Escape' });
      fireEvent.click(launcher()!);
      await flush();
      expect(FakeWS.live()).toHaveLength(1);
      expect(FakeWS.instances).toHaveLength(1);

      unmount();
      expect(FakeWS.live()).toHaveLength(0);
      await flush(60000);
      expect(FakeWS.instances).toHaveLength(1);
      expect(vi.getTimerCount()).toBe(0);
    });

    it('activates the client only once the panel is open', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush(60000);
      expect(fc.client.activate).not.toHaveBeenCalled();
      fireEvent.click(launcher()!);
      await flush();
      expect(fc.client.activate).toHaveBeenCalled();
    });

    it('stops the client on unmount', async () => {
      const fc = fakeClient();
      const { unmount } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      unmount();
      expect(fc.client.stop).toHaveBeenCalledTimes(1);
    });

    it('hides the Chatwoot bubble while rendered (also when its SDK becomes ready later) and restores it on unmount', async () => {
      const toggle = vi.fn();
      (window as unknown as Record<string, unknown>).$chatwoot = { toggleBubbleVisibility: toggle };
      const fc = fakeClient();
      const { unmount } = render(<ChatWidget createClient={fc.create} />);
      expect(toggle).not.toHaveBeenCalled(); // 还不知道 enabled，不动别人的气泡
      await flush();
      expect(toggle.mock.calls).toEqual([['hide']]);
      window.dispatchEvent(new Event('chatwoot:ready'));
      expect(toggle.mock.calls).toEqual([['hide'], ['hide']]);
      unmount();
      expect(toggle.mock.calls).toEqual([['hide'], ['hide'], ['show']]);
      window.dispatchEvent(new Event('chatwoot:ready'));
      expect(toggle).toHaveBeenCalledTimes(3);
    });

    it('never touches the Chatwoot bubble when our launcher is not rendered', async () => {
      const toggle = vi.fn();
      (window as unknown as Record<string, unknown>).$chatwoot = { toggleBubbleVisibility: toggle };
      const fc = fakeClient({ enabled: false });
      const { unmount } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      unmount();
      expect(toggle).not.toHaveBeenCalled();
    });

    it('requestOpenChat() opens the panel when the widget is live, and reports false when it is not', async () => {
      expect(requestOpenChat()).toBe(false);
      const fc = fakeClient();
      const { unmount } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      let handled = false;
      act(() => { handled = requestOpenChat(); });
      expect(handled).toBe(true);
      expect(dialog()).not.toBeNull();
      unmount();
      expect(requestOpenChat()).toBe(false);
    });
  });

  describe('panel', () => {
    it('opens as a dialog with focus in the input; Esc closes it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      expect(dialog()).not.toBeNull();
      expect(dialog()!.getAttribute('aria-label')).toBe('title');
      expect(document.activeElement).toBe(input());
      // 输入法组字中的 Esc 只是取消候选
      fireEvent.keyDown(input(), { key: 'Escape', isComposing: true });
      fireEvent.keyDown(input(), { key: 'Escape', keyCode: 229 });
      expect(dialog()).not.toBeNull();
      expect(dialog()!.querySelector('[data-sentry-mask]')).not.toBeNull();
      fireEvent.keyDown(dialog()!, { key: 'Escape' });
      expect(dialog()).toBeNull();
      expect(launcher()).not.toBeNull();
    });

    it('shows the welcome text and options while there is no conversation; clicking one sends option_reply with its value', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      expect(screen.getByText('hello')).toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Install' }));
      await flush();
      expect(fc.client.send).toHaveBeenCalledWith('option_reply', 'install');
    });

    it('an option_reply bubble shows the option label, and the welcome options disappear once there are messages', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push([msg(1, { senderType: 'visitor', kind: 'option_reply', content: 'install' })]);
      expect(screen.getByText('Install')).toBeInTheDocument();
      expect(screen.queryByText('install')).toBeNull();
      expect(screen.queryByRole('button', { name: 'Install' })).toBeNull();
      expect(screen.queryByRole('button', { name: 'Buy' })).toBeNull();
    });

    it('renders content as text only, linkifies http(s) URLs safely, labels ai/staff generically, hides notes', async () => {
      const fc = fakeClient({
        conversation: { uuid: 'u', status: 'open', handler: 'ai' },
        messages: [
          msg(1, { senderType: 'visitor', content: '<img src=x onerror=alert(1)> hi' }),
          msg(2, { senderType: 'ai', content: 'see https://example.com/a?b=1, or javascript:alert(1)' }),
          msg(3, { senderType: 'staff', senderName: 'Real Name', content: 'staff says' }),
          msg(4, { senderType: 'system', kind: 'event', content: 'transferred', meta: { event: 'transfer_human' } }),
          msg(5, { senderType: 'staff', senderName: 'Real Name', kind: 'image', content: 'https://x/y.png' }),
          msg(6, { senderType: 'staff', kind: 'note' as ChatMessage['kind'], content: 'internal secret' }),
        ],
      });
      const { container } = await mountOpen(fc);
      expect(container.querySelector('img')).toBeNull();
      expect(screen.getByText('<img src=x onerror=alert(1)> hi')).toBeInTheDocument();
      const links = container.querySelectorAll('a');
      expect(links).toHaveLength(1);
      expect(links[0].getAttribute('href')).toBe('https://example.com/a?b=1');
      expect(links[0].getAttribute('rel')).toBe('noopener noreferrer nofollow');
      expect(links[0].getAttribute('target')).toBe('_blank');
      expect(container.textContent).toContain('javascript:alert(1)');
      expect(container.textContent).not.toContain('Real Name');
      expect(screen.getByText('senderAi')).toBeInTheDocument();
      expect(screen.getAllByText('senderStaff').length).toBeGreaterThan(0);
      expect(screen.getByText('transferred')).toBeInTheDocument();
      expect(screen.getByText('imagePlaceholder')).toBeInTheDocument();
      expect(container.textContent).not.toContain('internal secret');
      // 已有会话：不再显示欢迎语
      expect(screen.queryByText('hello')).toBeNull();
    });

    it('Enter sends and clears the draft; Shift+Enter and Enter during IME composition do not send', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      fireEvent.change(input(), { target: { value: 'ni hao' } });
      fireEvent.keyDown(input(), { key: 'Enter', shiftKey: true });
      fireEvent.keyDown(input(), { key: 'Enter', isComposing: true });
      fireEvent.keyDown(input(), { key: 'Enter', keyCode: 229 });
      await flush();
      expect(fc.client.send).not.toHaveBeenCalled();
      expect(input().value).toBe('ni hao');

      fireEvent.keyDown(input(), { key: 'Enter' });
      await flush();
      expect(fc.client.send.mock.calls).toEqual([['text', 'ni hao']]);
      expect(input().value).toBe('');
    });

    it('does not send blank text; refuses text over 2000 characters and keeps it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      fireEvent.change(input(), { target: { value: '   ' } });
      fireEvent.keyDown(input(), { key: 'Enter' });
      const long = 'x'.repeat(2001);
      fireEvent.change(input(), { target: { value: long } });
      fireEvent.click(screen.getByRole('button', { name: 'send' }));
      await flush();
      expect(fc.client.send).not.toHaveBeenCalled();
      expect(input().value).toBe(long);
      expect(screen.getByRole('alert').textContent).toBe('tooLong');
    });

    it('rate limit (429) keeps the draft and shows the gentle copy; a network failure shows the generic one', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      fc.client.send.mockRejectedValueOnce(new ChatError('rate_limited', 429));
      fireEvent.change(input(), { target: { value: 'again' } });
      fireEvent.keyDown(input(), { key: 'Enter' });
      await flush();
      expect(input().value).toBe('again');
      expect(screen.getByRole('alert').textContent).toBe('rateLimited');

      fc.client.send.mockRejectedValueOnce(new ChatError('network', 0));
      fireEvent.keyDown(input(), { key: 'Enter' });
      await flush();
      expect(input().value).toBe('again');
      expect(screen.getByRole('alert').textContent).toBe('sendFailed');

      fireEvent.keyDown(input(), { key: 'Enter' });
      await flush();
      expect(input().value).toBe('');
      expect(screen.queryByRole('alert')).toBeNull();
    });

    it('a closed conversation (server state) shows the ended line and keeps old messages; a new conversation clears it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      const old = [msg(1, { senderType: 'visitor', content: 'old question' })];
      await fc.push(old);
      await fc.pushConv({ uuid: 'u1', status: 'open', handler: 'ai' });
      expect(screen.queryByText('closed')).toBeNull();
      await fc.pushConv({ uuid: 'u1', status: 'closed', handler: 'ai' });
      expect(screen.getByText('closed')).toBeInTheDocument();
      expect(screen.getByText('old question')).toBeInTheDocument();
      await fc.push([...old, msg(3, { senderType: 'visitor', content: 'new question' })]);
      await fc.pushConv({ uuid: 'u2', status: 'open', handler: 'ai' });
      expect(screen.queryByText('closed')).toBeNull();
      expect(screen.getByText('old question')).toBeInTheDocument();
    });
  });

  describe('email form', () => {
    const ai: ChatConversation = { uuid: 'u1', status: 'open', handler: 'ai' };
    const human: ChatConversation = { uuid: 'u1', status: 'open', handler: 'human' };
    const history = [
      msg(1, { senderType: 'visitor', content: 'help' }),
      msg(2, { senderType: 'system', kind: 'event', content: 'transferred', meta: { event: 'transfer_human' } }),
    ];
    const emailInput = () => screen.queryByPlaceholderText('emailPlaceholder') as HTMLInputElement | null;

    it('appears 30s after the server says the handler is human with no staff reply — not before, never for ai', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push(history);
      await fc.pushConv(ai);
      await flush(60000);
      expect(emailInput()).toBeNull();
      await fc.pushConv(human);
      await flush(29999);
      expect(emailInput()).toBeNull();
      await flush(1);
      expect(emailInput()).not.toBeNull();
    });

    it('a transfer event message alone (no server state) does not start the timer', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push(history);
      await flush(60000);
      expect(emailInput()).toBeNull();
    });

    it('is anchored to the transfer, not to opening the panel: opening later shows it at once', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      await fc.pushConv(human);
      await flush(30000);
      fireEvent.click(launcher()!);
      await flush();
      expect(emailInput()).not.toBeNull();
      // 关了再开也不重新计时
      fireEvent.keyDown(dialog()!, { key: 'Escape' });
      fireEvent.click(launcher()!);
      await flush();
      expect(emailInput()).not.toBeNull();
    });

    it('page load into a waiting conversation: counts from the transfer event time in the history', async () => {
      vi.setSystemTime(new Date('2026-10-02T00:00:10Z')); // 转人工事件在 00:00:00
      const fc = fakeClient({ conversation: human, messages: history });
      await mountOpen(fc);
      await flush(19000);
      expect(emailInput()).toBeNull();
      await flush(1000);
      expect(emailInput()).not.toBeNull();
    });

    it('page load: a transfer time in the future (clock skew) falls back to now', async () => {
      vi.setSystemTime(new Date('2026-10-01T23:00:00Z'));
      const fc = fakeClient({ conversation: human, messages: history });
      await mountOpen(fc);
      await flush(29000);
      expect(emailInput()).toBeNull();
      await flush(1000);
      expect(emailInput()).not.toBeNull();
    });

    it('staff replying after the transfer cancels it (before or after it appeared); an earlier staff message does not', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      const earlier = [...history, msg(3, { senderType: 'staff', content: 'from a previous handover' })];
      await fc.push(earlier);
      await fc.pushConv(human); // 此刻最大 id = 3
      await flush(30000);
      expect(emailInput()).not.toBeNull();
      await fc.push([...earlier, msg(4, { senderType: 'staff', content: 'here' })]);
      expect(emailInput()).toBeNull();
      await flush(60000);
      expect(emailInput()).toBeNull();
    });

    it('goes away when the conversation is handed back or closed, and restarts for a new waiting conversation', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.pushConv(human);
      await flush(30000);
      expect(emailInput()).not.toBeNull();
      await fc.pushConv({ ...human, status: 'closed' });
      expect(emailInput()).toBeNull();
      await fc.pushConv({ uuid: 'u2', status: 'open', handler: 'human' });
      await flush(29999);
      expect(emailInput()).toBeNull();
      await flush(1);
      expect(emailInput()).not.toBeNull();
    });

    it('does not appear when this browser already left an email', async () => {
      localStorage.setItem(CHAT_EMAIL_FLAG, '1');
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.pushConv(human);
      await flush(60000);
      expect(emailInput()).toBeNull();
    });

    it('validates the format client-side, submits, then shows the confirmation and remembers it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.pushConv(human);
      await flush(30000);
      fireEvent.change(emailInput()!, { target: { value: 'not-an-email' } });
      fireEvent.click(screen.getByRole('button', { name: 'emailSubmit' }));
      await flush();
      expect(fc.client.leaveEmail).not.toHaveBeenCalled();
      expect(screen.getByText('emailInvalid')).toBeInTheDocument();

      fc.client.leaveEmail.mockRejectedValueOnce(new ChatError('invalid', 422));
      fireEvent.change(emailInput()!, { target: { value: ' me@example.com ' } });
      fireEvent.click(screen.getByRole('button', { name: 'emailSubmit' }));
      await flush();
      expect(fc.client.leaveEmail).toHaveBeenCalledWith('me@example.com');
      expect(screen.getByText('emailFailed')).toBeInTheDocument();
      expect(localStorage.getItem(CHAT_EMAIL_FLAG)).toBeNull();

      fireEvent.click(screen.getByRole('button', { name: 'emailSubmit' }));
      await flush();
      expect(emailInput()).toBeNull();
      expect(screen.getByText('emailSaved')).toBeInTheDocument();
      expect(localStorage.getItem(CHAT_EMAIL_FLAG)).toBe('1');
    });
  });
});
