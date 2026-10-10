import { StrictMode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
// Labels render as their keys, so assertions name the message key.
vi.mock('next-intl', () => ({ useTranslations: () => (key: string) => key }));

import ChatWidget from '../chat/ChatWidget';
import { CHAT_KNOWN_FLAG, hasResumeToken, requestOpenChat, shouldStartSession, takeResumeToken } from '../chat/gate';
import { CHAT_RESUME_GLOBAL, CHAT_RESUME_HASH_PREFIX, CHAT_RESUME_SCRIPT, stripChatResume } from '../chat/resume-script';
import {
  ChatError,
  createChatClient,
  type ChatClient,
  type ChatConversation,
  type ChatMessage,
  type SessionState,
} from '@/lib/chat-client';
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
    sendImage: vi.fn<(file: Blob) => Promise<void>>(async () => {}),
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
    visit('/en-GB/pricing?chat=preview');
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
  });

  describe('deferred start (probe said enabled; R26)', () => {
    beforeEach(() => visit('/en-GB/pricing'));

    it('shows only the launcher: no client, no session, no cookie until the visitor opens the panel', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} deferStart />);
      await flush();
      expect(launcher()).not.toBeNull();
      expect(fc.create).not.toHaveBeenCalled();
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBeNull();

      fireEvent.click(launcher()!);
      await flush();
      expect(fc.client.start).toHaveBeenCalledTimes(1);
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/pricing', {});
      expect(dialog()).not.toBeNull();
      expect(fc.client.activate).toHaveBeenCalled();
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBe('1');
    });

    it('shows a connecting line and keeps the draft while the session is being created', async () => {
      const fc = fakeClient();
      let resolve!: (s: SessionState) => void;
      fc.client.start.mockImplementationOnce(() => new Promise<SessionState>((r) => { resolve = r; }));
      render(<ChatWidget createClient={fc.create} deferStart />);
      await flush();
      fireEvent.click(launcher()!);
      await flush();
      expect(screen.getByText('connecting')).toBeInTheDocument();
      fireEvent.change(input(), { target: { value: 'hello' } });
      fireEvent.keyDown(input(), { key: 'Enter' });
      expect(fc.client.send).not.toHaveBeenCalled();
      expect(input().value).toBe('hello');
      await act(async () => { resolve(session() as unknown as SessionState); });
      await flush();
      expect(screen.queryByText('connecting')).toBeNull();
    });

    it('disappears when the session call says enabled:false', async () => {
      const fc = fakeClient({ enabled: false });
      const { container } = render(<ChatWidget createClient={fc.create} deferStart />);
      await flush();
      fireEvent.click(launcher()!);
      await flush();
      expect(container.innerHTML).toBe('');
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBeNull();
    });

    it('a failed session call folds the panel back to the launcher, and a second click retries', async () => {
      const fc = fakeClient();
      fc.client.start.mockRejectedValueOnce(new ChatError('network', 0));
      render(<ChatWidget createClient={fc.create} deferStart />);
      await flush();
      fireEvent.click(launcher()!);
      await flush();
      expect(dialog()).toBeNull();
      expect(launcher()).not.toBeNull();
      fireEvent.click(launcher()!);
      await flush();
      expect(fc.client.start).toHaveBeenCalledTimes(2);
      expect(dialog()).not.toBeNull();
    });

    it('requestOpenChat() starts the session and opens the panel', async () => {
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} deferStart />);
      await flush();
      let handled = false;
      act(() => { handled = requestOpenChat(); });
      await flush();
      expect(handled).toBe(true);
      expect(fc.client.start).toHaveBeenCalledTimes(1);
      expect(dialog()).not.toBeNull();
    });
  });

  describe('images', () => {
    const png = (size = 10) => new File([new Uint8Array(size)], 'shot.png', { type: 'image/png' });
    const attach = () => screen.queryByRole('button', { name: 'attachImage' });
    const fileInput = () => document.querySelector('input[type=file]') as HTMLInputElement;
    const choose = (file: File) => fireEvent.change(fileInput(), { target: { files: [file] } });

    it('no image button when the server has no image storage', async () => {
      await mountOpen(fakeClient());
      expect(attach()).toBeNull();
      expect(document.querySelector('input[type=file]')).toBeNull();
    });

    it('choosing an image uploads it', async () => {
      const fc = fakeClient({ images: true });
      await mountOpen(fc);
      expect(attach()).not.toBeNull();
      const file = png();
      choose(file);
      await flush();
      expect(fc.client.sendImage).toHaveBeenCalledWith(file);
    });

    it('rejects oversize and non-image files locally, without uploading', async () => {
      const fc = fakeClient({ images: true });
      await mountOpen(fc);
      choose(png(5 * 1024 * 1024 + 1));
      await flush();
      expect(screen.getByRole('alert').textContent).toBe('imageTooLarge');
      choose(new File(['<svg/>'], 'x.svg', { type: 'image/svg+xml' }));
      await flush();
      expect(screen.getByRole('alert').textContent).toBe('imageInvalid');
      expect(fc.client.sendImage).not.toHaveBeenCalled();
    });

    it('a failed upload shows an error', async () => {
      const fc = fakeClient({ images: true });
      fc.client.sendImage.mockRejectedValueOnce(new ChatError('network', 0));
      await mountOpen(fc);
      choose(png());
      await flush();
      expect(screen.getByRole('alert').textContent).toBe('imageFailed');
    });

    it('pasting a screenshot into the input uploads it; pasting text does not', async () => {
      const fc = fakeClient({ images: true });
      await mountOpen(fc);
      const file = png();
      fireEvent.paste(input(), { clipboardData: { files: [file] } });
      await flush();
      expect(fc.client.sendImage).toHaveBeenCalledWith(file);
      fireEvent.paste(input(), { clipboardData: { files: [] } });
      await flush();
      expect(fc.client.sendImage).toHaveBeenCalledTimes(1);
    });
  });

  describe('gating', () => {
    it.each(['/en-GB/pricing?chat=preview&embed=true', '/en-GB/pricing?chat=preview#embed'])(
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
      visit('/en-GB/pricing');
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
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/pricing', { preview: true });
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
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/pricing', { preview: true });
      expect(launcher()).not.toBeNull();
      expect(dialog()).toBeNull();
      expect(window.location.search).toBe('?chat=preview');
      expect(localStorage.getItem(CHAT_KNOWN_FLAG)).toBe('1');
    });

    it('a known browser probes the session without preview', async () => {
      visit('/en-GB/purchase');
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/purchase', {});
      expect(launcher()).not.toBeNull();
    });

    it('preview survives navigation within the tab (sessionStorage), without the query param', async () => {
      const first = fakeClient();
      const { unmount } = render(<ChatWidget createClient={first.create} />);
      await flush();
      unmount();
      visit('/en-GB/purchase');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/purchase', { preview: true });
    });

    it('#chat=<token> is a resume token: passed as resume, panel auto-opens, fragment removed, query kept', async () => {
      visit('/en-GB/support?utm_source=mail#chat=abc.DEF-123');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      expect(window.location.hash).toBe('');
      expect(window.location.search).toBe('?utm_source=mail');
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/support', { resume: 'abc.DEF-123' });
      expect(dialog()).not.toBeNull();
      expect(window.location.href).not.toContain('abc.DEF-123');
    });

    it('picks up the token the inline script stashed on window, and removes it', async () => {
      visit('/en-GB/support?chat=preview');
      (window as unknown as Record<string, unknown>).__chatResume = 'stashed.tok';
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/en-GB/support', { preview: true, resume: 'stashed.tok' });
      expect('__chatResume' in window).toBe(false);
      expect(dialog()).not.toBeNull();
    });

    it('a non-preview ?chat= query value is ignored: not a resume token, not a reason to probe', async () => {
      visit('/en-GB/support?chat=abc.DEF-123');
      const fc = fakeClient();
      const { container } = render(<ChatWidget createClient={fc.create} />);
      await flush();
      expect(fc.create).not.toHaveBeenCalled();
      expect(container.innerHTML).toBe('');

      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      const known = fakeClient();
      render(<ChatWidget createClient={known.create} />);
      await flush();
      expect(known.client.start).toHaveBeenCalledWith('/en-GB/support', {});
      expect(dialog()).toBeNull();
    });

    it('strict mode still passes the resume token on the second mount after it was taken', async () => {
      visit('/en-GB/support#chat=tok1');
      const fc = fakeClient();
      render(<StrictMode><ChatWidget createClient={fc.create} /></StrictMode>);
      await flush();
      expect(window.location.hash).toBe('');
      expect(fc.client.start).toHaveBeenLastCalledWith('/en-GB/support', { resume: 'tok1' });
      expect(dialog()).not.toBeNull();
    });

    it('shouldStartSession: preview, a pending resume token, or the known flag — nothing else', () => {
      visit('/a');
      expect(shouldStartSession()).toBe(false);
      visit('/a?chat=');
      expect(shouldStartSession()).toBe(false);
      visit('/a?chat=tok&x=1#contact');
      expect(shouldStartSession()).toBe(false);
      visit('/a#chat=tok');
      expect(shouldStartSession()).toBe(true);
      expect(hasResumeToken()).toBe(true); // 判断不取走
      expect(takeResumeToken()).toBe('tok');
      expect(takeResumeToken()).toBeNull();
      expect(shouldStartSession()).toBe(false);
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      expect(shouldStartSession()).toBe(true);
      localStorage.clear();
      visit('/a?chat=preview');
      expect(shouldStartSession()).toBe(true);
    });

    describe('inline resume script (root layout, before analytics)', () => {
      const run = () => new Function(CHAT_RESUME_SCRIPT)();
      const stash = () => (window as unknown as Record<string, unknown>).__chatResume;

      it('moves #chat=<token> onto window and strips the fragment, keeping path and query', () => {
        visit('/en-GB/support?utm_source=mail#chat=abc.DEF-123');
        run();
        expect(stash()).toBe('abc.DEF-123');
        expect(window.location.pathname + window.location.search).toBe('/en-GB/support?utm_source=mail');
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
        ['#chat=abc.DEF-123_x%2B', '/en-GB/support', '', false],
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

      it.each(['/en-GB/support', '/en-GB/support#contact', '/en-GB/support?chat=tok#embed'])('leaves %s alone', (url) => {
        visit(url);
        const before = window.location.href;
        run();
        expect(stash()).toBeUndefined();
        expect(window.location.href).toBe(before);
      });
    });
  });

  // Cookie 同意横幅（z-[9999]）占着同一个角：入口与面板都以它公布的高度为底边，不被它盖住，也不盖它。
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
      expect(screen.getByText('eventTransferHuman')).toBeInTheDocument(); // 事件按名取本地化文案
      expect(container.textContent).not.toContain('transferred');
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

  describe('email form (required before chatting)', () => {
    const emailInput = () => screen.queryByPlaceholderText('emailPlaceholder') as HTMLInputElement | null;
    const sendButton = () => screen.getByRole('button', { name: 'send' }) as HTMLButtonElement;
    const leave = async (value: string) => {
      fireEvent.change(emailInput()!, { target: { value } });
      fireEvent.click(screen.getByRole('button', { name: 'emailSubmit' }));
      await flush();
    };

    it('no form when the server does not require one (logged in, or email already left)', async () => {
      const fc = fakeClient({ emailRequired: false });
      await mountOpen(fc);
      expect(emailInput()).toBeNull();
      expect(input().disabled).toBe(false);
      fireEvent.change(input(), { target: { value: 'hi' } });
      fireEvent.click(sendButton());
      await flush();
      expect(fc.client.send).toHaveBeenCalledWith('text', 'hi');
    });

    it('locks the input, send button, image button and welcome options until an email is left', async () => {
      const fc = fakeClient({ emailRequired: true, images: true });
      await mountOpen(fc);
      expect(emailInput()).not.toBeNull();
      expect(input().disabled).toBe(true);
      expect(sendButton().disabled).toBe(true);
      expect((screen.getByRole('button', { name: 'attachImage' }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByText('hello')).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Install' })).toBeNull();
      fireEvent.keyDown(input(), { key: 'Enter' });
      await flush();
      expect(fc.client.send).not.toHaveBeenCalled();
    });

    it('validates the format client-side, retries a failed submit, then unlocks the chat', async () => {
      const fc = fakeClient({ emailRequired: true });
      await mountOpen(fc);
      await leave('not-an-email');
      expect(fc.client.leaveEmail).not.toHaveBeenCalled();
      expect(screen.getByText('emailInvalid')).toBeInTheDocument();

      fc.client.leaveEmail.mockRejectedValueOnce(new ChatError('invalid', 422));
      await leave(' me@example.com ');
      expect(fc.client.leaveEmail).toHaveBeenCalledWith('me@example.com');
      expect(screen.getByText('emailFailed')).toBeInTheDocument();
      expect(input().disabled).toBe(true);

      fireEvent.click(screen.getByRole('button', { name: 'emailSubmit' }));
      await flush();
      expect(emailInput()).toBeNull();
      expect(screen.getByText('emailSaved')).toBeInTheDocument();
      expect(input().disabled).toBe(false);
      expect(document.activeElement).toBe(input());
      expect(screen.getByRole('button', { name: 'Install' })).toBeInTheDocument();
      fireEvent.change(input(), { target: { value: 'hi' } });
      fireEvent.click(sendButton());
      await flush();
      expect(fc.client.send).toHaveBeenCalledWith('text', 'hi');
    });

    it('a send the server rejects with email_required brings the form back and keeps the draft, without an error line', async () => {
      const fc = fakeClient({ emailRequired: false });
      fc.client.send.mockRejectedValueOnce(new ChatError('email_required', 422));
      await mountOpen(fc);
      fireEvent.change(input(), { target: { value: 'hi' } });
      fireEvent.click(sendButton());
      await flush();
      expect(emailInput()).not.toBeNull();
      expect(input().value).toBe('hi');
      expect(input().disabled).toBe(true);
      expect(screen.queryByRole('alert')).toBeNull();
    });

    it('an image upload rejected with email_required brings the form back', async () => {
      const fc = fakeClient({ emailRequired: false, images: true });
      fc.client.sendImage.mockRejectedValueOnce(new ChatError('email_required', 422));
      await mountOpen(fc);
      const fileInput = document.querySelector('input[type=file]') as HTMLInputElement;
      fireEvent.change(fileInput, { target: { files: [new File([new Uint8Array(10)], 'a.png', { type: 'image/png' })] } });
      await flush();
      expect(emailInput()).not.toBeNull();
      expect(screen.queryByRole('alert')).toBeNull();
    });
  });
});
