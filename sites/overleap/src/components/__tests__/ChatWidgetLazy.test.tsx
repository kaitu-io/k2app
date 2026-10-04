import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';

// 挂件本体换成替身：工厂只在真的发生动态 import 时执行，借此断言"门没开就不加载代码块"。
type WidgetProps = { deferStart?: boolean };
const widget = vi.hoisted(() => ({ loads: 0, impl: ((): unknown => null) as (props: WidgetProps) => unknown }));
vi.mock('../chat/ChatWidget', () => {
  widget.loads++;
  return { default: (props: WidgetProps) => widget.impl(props) };
});

const sentry = vi.hoisted(() => ({ captureException: vi.fn() }));
vi.mock('@sentry/nextjs', () => sentry);

import ChatWidgetLazy, { ChatErrorBoundary } from '../chat/ChatWidgetLazy';
import { CHAT_ENABLED_CACHE, CHAT_KNOWN_FLAG, probeChatEnabled } from '../chat/gate';

const visit = (url: string) => window.history.pushState({}, '', url);

/** 探测接口的替身：返回给定的 enabled，记录调用。 */
function stubProbe(enabled: boolean | 'error') {
  const fetchMock = vi.fn(async () =>
    enabled === 'error'
      ? new Response('', { status: 502 })
      : new Response(JSON.stringify({ code: 0, data: { enabled } }), { status: 200 }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('ChatWidgetLazy', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    widget.impl = (props) => <div data-testid="chat-widget" data-defer={String(Boolean(props.deferStart))} />;
    sentry.captureException.mockClear();
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  // 顺序有意义：先跑完所有"门没开"的用例，确认模块工厂一次都没执行过。
  it.each([
    ['probe says chat is off', '/en-GB/pricing'],
    ['probe fails', '/en-GB/pricing'],
    ['embedded page', '/en-GB/pricing?chat=preview&embed=true'],
  ])('renders nothing and never imports the widget chunk — %s', async (name, url) => {
    visit(url);
    const fetchMock = stubProbe(name === 'probe fails' ? 'error' : false);
    const { container } = render(<ChatWidgetLazy />);
    await act(async () => { await new Promise((r) => setTimeout(r, 20)); });
    expect(container.innerHTML).toBe('');
    expect(widget.loads).toBe(0);
    // 内嵌页连探测都不发
    expect(fetchMock).toHaveBeenCalledTimes(name.startsWith('probe') ? 1 : 0);
  });

  it('probe says enabled: loads the widget in deferred-start mode', async () => {
    visit('/en-GB/pricing');
    const fetchMock = stubProbe(true);
    render(<ChatWidgetLazy />);
    await waitFor(() => expect(screen.getByTestId('chat-widget')).toBeInTheDocument());
    expect(screen.getByTestId('chat-widget').dataset.defer).toBe('true');
    expect(fetchMock).toHaveBeenCalledWith('/api/chat/enabled', { headers: { 'X-K2-Brand': 'overleap' } });
  });

  it('imports and renders the widget when the gate says yes', async () => {
    visit('/en-GB/pricing');
    localStorage.setItem(CHAT_KNOWN_FLAG, '1');
    const fetchMock = stubProbe(false);
    render(<ChatWidgetLazy />);
    await waitFor(() => expect(screen.getByTestId('chat-widget')).toBeInTheDocument());
    expect(widget.loads).toBe(1);
    // 建过会话的浏览器直接启动，不必探测
    expect(screen.getByTestId('chat-widget').dataset.defer).toBe('false');
    expect(fetchMock).not.toHaveBeenCalled();
    expect(sentry.captureException).not.toHaveBeenCalled();
  });

  it('a widget that throws while rendering is swallowed: the page around it keeps rendering', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    widget.impl = () => { throw new Error('boom'); };
    visit('/en-GB/pricing?chat=preview');
    render(
      <main>
        <p>{'checkout'}</p>
        <ChatWidgetLazy />
      </main>,
    );
    await act(async () => { await new Promise((r) => setTimeout(r, 50)); });
    expect(screen.getByText('checkout')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-widget')).toBeNull();
    // 吞掉但要上报，且只报一次
    await waitFor(() => expect(sentry.captureException).toHaveBeenCalled());
    await act(async () => { await new Promise((r) => setTimeout(r, 20)); });
    expect(sentry.captureException).toHaveBeenCalledTimes(1);
    const [error, hint] = sentry.captureException.mock.calls[0];
    expect((error as Error).message).toBe('boom');
    expect(hint).toEqual({ tags: { component: 'chat-widget' } });
  });

  it('ChatErrorBoundary renders null for a throwing child and passes healthy children through', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    const Boom = () => { throw new Error('boom'); };
    const { container } = render(
      <div>
        <span>{'page'}</span>
        <ChatErrorBoundary><Boom /></ChatErrorBoundary>
        <ChatErrorBoundary><b>{'ok'}</b></ChatErrorBoundary>
      </div>,
    );
    expect(container.textContent).toBe('pageok');
    await waitFor(() => expect(sentry.captureException).toHaveBeenCalled());
    expect(sentry.captureException).toHaveBeenCalledTimes(1);
    expect(sentry.captureException.mock.calls[0][1]).toEqual({ tags: { component: 'chat-widget' } });
  });
});

describe('probeChatEnabled', () => {
  beforeEach(() => sessionStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  const ok = (enabled: unknown) => vi.fn(async () => new Response(JSON.stringify({ code: 0, data: { enabled } })));

  it('asks the server with the brand header and caches the answer for the tab', async () => {
    const f = ok(true);
    expect(await probeChatEnabled('overleap', f)).toBe(true);
    expect(await probeChatEnabled('overleap', f)).toBe(true);
    expect(f).toHaveBeenCalledTimes(1);
    expect(f).toHaveBeenCalledWith('/api/chat/enabled', { headers: { 'X-K2-Brand': 'overleap' } });
  });

  it('re-asks after the cache expires', async () => {
    sessionStorage.setItem(CHAT_ENABLED_CACHE, JSON.stringify({ enabled: false, at: Date.now() - 6 * 60_000 }));
    const f = ok(true);
    expect(await probeChatEnabled('overleap', f)).toBe(true);
    expect(f).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['HTTP error', vi.fn(async () => new Response('', { status: 500 }))],
    ['network error', vi.fn(async () => { throw new TypeError('Failed to fetch'); })],
    ['non-zero code', vi.fn(async () => new Response(JSON.stringify({ code: 500, data: { enabled: true } })))],
    ['non-boolean enabled', ok('yes')],
  ])('treats %s as off', async (_name, f) => {
    expect(await probeChatEnabled('overleap', f as unknown as typeof fetch)).toBe(false);
  });

  it('does not cache network failures', async () => {
    const f = vi.fn(async () => { throw new TypeError('Failed to fetch'); });
    await probeChatEnabled('overleap', f as unknown as typeof fetch);
    expect(sessionStorage.getItem(CHAT_ENABLED_CACHE)).toBeNull();
  });
});
