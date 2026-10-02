import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';

// 挂件本体换成替身：工厂只在真的发生动态 import 时执行，借此断言"门没开就不加载代码块"。
const widget = vi.hoisted(() => ({ loads: 0, impl: (): unknown => null }));
vi.mock('../chat/ChatWidget', () => {
  widget.loads++;
  return { default: () => widget.impl() };
});

import ChatWidgetLazy, { ChatErrorBoundary } from '../chat/ChatWidgetLazy';
import { CHAT_KNOWN_FLAG } from '../chat/gate';

const visit = (url: string) => window.history.pushState({}, '', url);

describe('ChatWidgetLazy', () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    widget.impl = () => <div data-testid="chat-widget" />;
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  // 顺序有意义：先跑完所有"门没开"的用例，确认模块工厂一次都没执行过。
  it.each([
    ['dark launch: no preview, no token, no known flag', '/zh-CN/pricing', undefined],
    ['embedded page', '/zh-CN/pricing?chat=preview&embed=true', undefined],
    ['brand has chat off', '/zh-CN/pricing', 'overleap'],
  ])('renders nothing and never imports the widget chunk — %s', async (_name, url, brand) => {
    visit(url);
    if (brand) {
      vi.stubEnv('NEXT_PUBLIC_BRAND', brand);
      localStorage.setItem(CHAT_KNOWN_FLAG, '1');
    }
    const { container } = render(<ChatWidgetLazy />);
    await act(async () => { await new Promise((r) => setTimeout(r, 20)); });
    expect(container.innerHTML).toBe('');
    expect(widget.loads).toBe(0);
  });

  it('imports and renders the widget when the gate says yes', async () => {
    visit('/zh-CN/pricing');
    localStorage.setItem(CHAT_KNOWN_FLAG, '1');
    render(<ChatWidgetLazy />);
    await waitFor(() => expect(screen.getByTestId('chat-widget')).toBeInTheDocument());
    expect(widget.loads).toBe(1);
  });

  it('a widget that throws while rendering is swallowed: the page around it keeps rendering', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    widget.impl = () => { throw new Error('boom'); };
    visit('/zh-CN/pricing?chat=preview');
    render(
      <main>
        <p>{'checkout'}</p>
        <ChatWidgetLazy />
      </main>,
    );
    await act(async () => { await new Promise((r) => setTimeout(r, 50)); });
    expect(screen.getByText('checkout')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-widget')).toBeNull();
  });

  it('ChatErrorBoundary renders null for a throwing child and passes healthy children through', () => {
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
  });
});
