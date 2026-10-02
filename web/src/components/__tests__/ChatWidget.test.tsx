import { StrictMode } from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import ChatWidget from '../chat/ChatWidget';
import { CHAT_EMAIL_FLAG, CHAT_KNOWN_FLAG, requestOpenChat, shouldProbeSession } from '../chat/gate';
import { ChatError, createChatClient, type ChatClient, type ChatMessage, type SessionState } from '@/lib/chat-client';
import { FakeWS, fakeFetch, msg, session, setHidden } from '@/lib/__tests__/chat-test-fakes';

const flush = (ms = 0) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

function fakeClient(state: Partial<SessionState> = {}) {
  const listeners = new Set<(m: ChatMessage[]) => void>();
  const full = { ...(session() as unknown as SessionState), ...state };
  const client = {
    start: vi.fn(async () => {
      listeners.forEach((cb) => cb(full.messages));
      return full;
    }),
    send: vi.fn(async () => {}),
    leaveEmail: vi.fn(async () => {}),
    onMessages: (cb: (m: ChatMessage[]) => void) => {
      listeners.add(cb);
      return () => { listeners.delete(cb); };
    },
    stop: vi.fn(() => listeners.clear()),
  } satisfies ChatClient;
  const push = (m: ChatMessage[]) => act(() => { listeners.forEach((cb) => cb(m)); });
  return { client, push, create: vi.fn(() => client) };
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

    it('?chat=<token> is a resume token: passed as resume, panel auto-opens, token stripped from the URL', async () => {
      visit('/zh-CN/support?utm_source=mail&chat=abc.DEF-123#contact');
      const fc = fakeClient();
      render(<ChatWidget createClient={fc.create} />);
      expect(window.location.search).toBe('?utm_source=mail');
      expect(window.location.hash).toBe('#contact');
      await flush();
      expect(fc.client.start).toHaveBeenCalledWith('/zh-CN/support', { resume: 'abc.DEF-123' });
      expect(dialog()).not.toBeNull();
      expect(window.location.href).not.toContain('abc.DEF-123');
    });

    it('strict mode still passes the resume token on the second mount after it was stripped', async () => {
      visit('/zh-CN/support?chat=tok1');
      const fc = fakeClient();
      render(<StrictMode><ChatWidget createClient={fc.create} /></StrictMode>);
      await flush();
      expect(window.location.search).toBe('');
      expect(fc.client.start).toHaveBeenLastCalledWith('/zh-CN/support', { resume: 'tok1' });
      expect(dialog()).not.toBeNull();
    });

    it('shouldProbeSession: only a non-empty ?chat= or the known flag', () => {
      expect(shouldProbeSession('')).toBe(false);
      expect(shouldProbeSession('?chat=')).toBe(false);
      expect(shouldProbeSession('?x=1')).toBe(false);
      expect(shouldProbeSession('?chat=preview')).toBe(true);
      expect(shouldProbeSession('?chat=tok')).toBe(true);
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
      expect(shouldProbeSession('')).toBe(true);
    });
  });

  describe('lifecycle', () => {
    it('strict-mode double mount leaves exactly one live socket; unmount leaves none and no timers', async () => {
      const f = fakeFetch({
        'POST /api/chat/session': () => session(),
        'GET /api/chat/messages': () => ({ messages: [] }),
        'GET /api/chat/ws-token': () => ({ token: 't' }),
      });
      const create = () => createChatClient({ fetch: f.fn, WebSocket: FakeWS });
      const { unmount } = render(<StrictMode><ChatWidget createClient={create} /></StrictMode>);
      await flush();
      expect(FakeWS.live()).toHaveLength(1);
      expect(FakeWS.instances).toHaveLength(1);
      expect(launcher()).not.toBeNull();
      FakeWS.last().open();
      await flush();
      unmount();
      expect(FakeWS.live()).toHaveLength(0);
      await flush(60000);
      expect(FakeWS.instances).toHaveLength(1);
      expect(vi.getTimerCount()).toBe(0);
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

    it('a closed conversation shows the ended line and keeps old messages; a new visitor message clears it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      const old = [
        msg(1, { senderType: 'visitor', content: 'old question' }),
        msg(2, { senderType: 'system', kind: 'event', content: 'closed by staff', meta: { event: 'closed' } }),
      ];
      await fc.push(old);
      expect(screen.getByText('closed')).toBeInTheDocument();
      expect(screen.getByText('old question')).toBeInTheDocument();
      await fc.push([...old, msg(3, { senderType: 'visitor', content: 'new question' })]);
      expect(screen.queryByText('closed')).toBeNull();
      expect(screen.getByText('old question')).toBeInTheDocument();
    });
  });

  describe('email form', () => {
    const transferred = [
      msg(1, { senderType: 'visitor', content: 'help' }),
      msg(2, { senderType: 'system', kind: 'event', content: 'transferred', meta: { event: 'transfer_human' } }),
    ];
    const emailInput = () => screen.queryByPlaceholderText('emailPlaceholder') as HTMLInputElement | null;

    it('appears 30s after the handler becomes human with no staff reply — not before', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await flush(60000);
      expect(emailInput()).toBeNull(); // ai 接待时不出现
      await fc.push(transferred);
      await flush(29999);
      expect(emailInput()).toBeNull();
      await flush(1);
      expect(emailInput()).not.toBeNull();
    });

    it('counts the 30s from opening the panel when that is later than the transfer', async () => {
      const fc = fakeClient({ conversation: { uuid: 'u', status: 'open', handler: 'human' }, messages: transferred });
      render(<ChatWidget createClient={fc.create} />);
      await flush(120000);
      fireEvent.click(launcher()!);
      await flush(29999);
      expect(emailInput()).toBeNull();
      await flush(1);
      expect(emailInput()).not.toBeNull();
    });

    it('does not appear when staff replied within 30s, and goes away when staff replies later', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push(transferred);
      await flush(10000);
      const replied = [...transferred, msg(3, { senderType: 'staff', content: 'here' })];
      await fc.push(replied);
      await flush(60000);
      expect(emailInput()).toBeNull();
    });

    it('does not appear when this browser already left an email', async () => {
      localStorage.setItem(CHAT_EMAIL_FLAG, '1');
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push(transferred);
      await flush(60000);
      expect(emailInput()).toBeNull();
    });

    it('validates the format client-side, submits, then shows the confirmation and remembers it', async () => {
      const fc = fakeClient();
      await mountOpen(fc);
      await fc.push(transferred);
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
