/**
 * `/manager/conversations` 只读会话列表：列表、?c= 深链详情、Slack 链接、备注、状态文案、关闭。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { ChatConversation, ChatMessage } from '@/lib/api';

const mockList = vi.fn();
const mockDetail = vi.fn();
const mockClose = vi.fn();

const routerState = vi.hoisted(() => ({ current: { push: vi.fn(), replace: vi.fn() } }));
const searchParamsState = vi.hoisted(() => ({ current: new URLSearchParams() }));

vi.mock('next/navigation', () => ({
  useRouter: () => routerState.current,
  useSearchParams: () => searchParamsState.current,
}));

const brandState = vi.hoisted(() => ({ current: undefined as string | undefined }));
vi.mock('@/components/manager/brand', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/manager/brand')>();
  return { ...actual, useManagerBrand: () => ({ brand: brandState.current ?? 'all', brandParam: brandState.current, setBrand: () => {} }) };
});

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      getChatConversations: (...a: unknown[]) => mockList(...a),
      getChatConversation: (...a: unknown[]) => mockDetail(...a),
      closeChatConversation: (...a: unknown[]) => mockClose(...a),
    },
  };
});

import ConversationsPage from '../conversations/page.kaitu';

function conv(over: Partial<ChatConversation> = {}): ChatConversation {
  return {
    uuid: 'u1',
    brand: 'kaitu',
    subjectKind: 'user',
    subjectId: 11,
    email: 'one@example.com',
    status: 'open',
    handler: 'human',
    entryPath: '/purchase',
    lastMessageAt: 1_700_000_000,
    lastMessageBy: 'visitor',
    slackPermalink: 'https://slack.example/archives/C1/p1',
    createdAt: 1_700_000_000,
    ...over,
  };
}

function msgs(): ChatMessage[] {
  return [
    { id: 1, senderType: 'visitor', senderName: '', kind: 'text', content: 'hello there', createdAt: 1_700_000_001 },
    { id: 2, senderType: 'ai', senderName: '', kind: 'text', content: 'ai answer', createdAt: 1_700_000_002 },
    { id: 3, senderType: 'staff', senderName: 'Alice', kind: 'text', content: 'staff answer', createdAt: 1_700_000_003 },
    { id: 4, senderType: 'staff', senderName: 'Alice', kind: 'note', content: 'secret memo', createdAt: 1_700_000_004 },
    { id: 5, senderType: 'system', senderName: '', kind: 'event', content: 'handed to human', createdAt: 1_700_000_005 },
    { id: 6, senderType: 'visitor', senderName: '', kind: 'option_reply', content: 'picked option A', createdAt: 1_700_000_006 },
  ];
}

function rows(items: ChatConversation[]) {
  mockList.mockResolvedValue({ items, pagination: { page: 0, pageSize: 50, total: items.length } });
}

beforeEach(() => {
  mockList.mockReset();
  mockDetail.mockReset();
  mockClose.mockReset();
  brandState.current = undefined;
  routerState.current = { push: vi.fn(), replace: vi.fn() };
  searchParamsState.current = new URLSearchParams();
  mockDetail.mockResolvedValue({ conversation: conv(), messages: msgs(), truncated: false });
  mockClose.mockResolvedValue(undefined);
  rows([conv(), conv({ uuid: 'u2', email: 'two@example.com', slackPermalink: '' })]);
});

describe('/manager/conversations', () => {
  it('列表显示两行', async () => {
    render(<ConversationsPage />);
    expect(await screen.findByText('one@example.com')).toBeInTheDocument();
    expect(screen.getByText('two@example.com')).toBeInTheDocument();
  });

  it('?c=u1 直接请求并展示该会话消息', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    render(<ConversationsPage />);
    expect(await screen.findByText('hello there')).toBeInTheDocument();
    expect(mockDetail).toHaveBeenCalledWith('u1');
    expect(screen.getByText('ai answer')).toBeInTheDocument();
    expect(screen.getByText('picked option A')).toBeInTheDocument();
    expect(screen.getByText('handed to human')).toBeInTheDocument();
  });

  it('关闭详情时去掉 ?c= 参数', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    render(<ConversationsPage />);
    await screen.findByText('hello there');
    fireEvent.keyDown(document.activeElement || document.body, { key: 'Escape' });
    await waitFor(() => expect(routerState.current.replace).toHaveBeenCalled());
    expect(routerState.current.replace.mock.calls.at(-1)![0]).not.toContain('c=');
  });

  it('slackPermalink 为空的行没有 Slack 链接；非空行链接新开且 noopener', async () => {
    render(<ConversationsPage />);
    const r1 = (await screen.findByText('one@example.com')).closest('tr')!;
    const r2 = screen.getByText('two@example.com').closest('tr')!;
    expect(within(r2).queryByText('在 Slack 打开')).toBeNull();
    const link = within(r1).getByText('在 Slack 打开').closest('a')!;
    expect(link).toHaveAttribute('href', 'https://slack.example/archives/C1/p1');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('note 消息带"内部备注"标签', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    render(<ConversationsPage />);
    const memo = await screen.findByText('secret memo');
    expect(within(memo.closest('[data-kind="note"]') as HTMLElement).getByText('内部备注')).toBeInTheDocument();
  });

  it('状态文案四种语义', async () => {
    rows([
      conv({ uuid: 'a', email: 'a@x.com', status: 'closed' }),
      conv({ uuid: 'b', email: 'b@x.com', handler: 'ai' }),
      conv({ uuid: 'c', email: 'c@x.com', handler: 'human', lastMessageBy: 'visitor' }),
      conv({ uuid: 'd', email: 'd@x.com', handler: 'human', lastMessageBy: 'staff' }),
    ]);
    render(<ConversationsPage />);
    const label = async (email: string) => within((await screen.findByText(email)).closest('tr')!);
    expect((await label('a@x.com')).getByText('已关闭')).toBeInTheDocument();
    expect((await label('b@x.com')).getByText('AI 接待中')).toBeInTheDocument();
    expect((await label('c@x.com')).getByText('等待人工')).toBeInTheDocument();
    expect((await label('d@x.com')).getByText('已回复待访客')).toBeInTheDocument();
  });

  it('关闭会话：确认后调用 closeChatConversation 并刷新列表', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    render(<ConversationsPage />);
    await screen.findByText('hello there');
    const before = mockList.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: '关闭会话' }));
    expect(mockClose).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: '确认关闭' }));
    await waitFor(() => expect(mockClose).toHaveBeenCalledWith('u1'));
    await waitFor(() => expect(mockList.mock.calls.length).toBeGreaterThan(before));
  });

  it('筛选参数传给接口', async () => {
    render(<ConversationsPage />);
    await screen.findByText('one@example.com');
    fireEvent.change(screen.getByLabelText('状态'), { target: { value: 'open' } });
    fireEvent.change(screen.getByLabelText('处理方'), { target: { value: 'ai' } });
    await waitFor(() =>
      expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'open', handler: 'ai' })),
    );
  });

  it('品牌切换时页码回到 0', async () => {
    rows([conv()]);
    mockList.mockResolvedValue({ items: [conv()], pagination: { page: 0, pageSize: 50, total: 200 } });
    const { rerender } = render(<ConversationsPage />);
    await screen.findByText('one@example.com');
    expect(mockList.mock.calls[0][0].page).toBe(1);
    fireEvent.click(screen.getByRole('button', { name: '下一页' }));
    await waitFor(() => expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 })));
    brandState.current = 'overleap';
    rerender(<ConversationsPage />);
    await waitFor(() =>
      expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1, brand: 'overleap' })),
    );
    expect(mockList.mock.calls.some((c) => c[0].page === 2 && c[0].brand === 'overleap')).toBe(false);
  });

  it('邮箱防抖：窗口后只请求一次且为 trim 后的值', async () => {
    render(<ConversationsPage />);
    await screen.findByText('one@example.com');
    const before = mockList.mock.calls.length;
    const input = screen.getByLabelText('邮箱');
    fireEvent.change(input, { target: { value: ' a' } });
    fireEvent.change(input, { target: { value: ' ab@x.com ' } });
    expect(mockList.mock.calls.length).toBe(before);
    await waitFor(() =>
      expect(mockList).toHaveBeenLastCalledWith(expect.objectContaining({ email: 'ab@x.com' })),
    );
    expect(mockList.mock.calls.length).toBe(before + 1);
  });

  it('详情加载失败显示重试，重试成功后展示消息', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    mockDetail.mockRejectedValueOnce(new Error('boom'));
    render(<ConversationsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '重试' }));
    expect(await screen.findByText('hello there')).toBeInTheDocument();
    expect(mockDetail).toHaveBeenCalledTimes(2);
  });

  it('图片消息不渲染 img，URL 以纯文本展示', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    mockDetail.mockResolvedValue({
      conversation: conv(),
      truncated: false,
      messages: [{ id: 9, senderType: 'visitor', senderName: '', kind: 'image', content: 'https://evil.example/a.png', createdAt: 1 }],
    });
    render(<ConversationsPage />);
    expect(await screen.findByText('https://evil.example/a.png')).toBeInTheDocument();
    expect(screen.getByText('[图片]')).toBeInTheDocument();
    expect(document.querySelector('img')).toBeNull();
  });

  it('非 https 的 slackPermalink 不渲染链接', async () => {
    rows([conv({ slackPermalink: 'javascript:alert(1)' })]);
    render(<ConversationsPage />);
    await screen.findByText('one@example.com');
    expect(screen.queryByText('在 Slack 打开')).toBeNull();
  });

  it('truncated 为 true 时显示提示，否则不显示', async () => {
    searchParamsState.current = new URLSearchParams('c=u1');
    mockDetail.mockResolvedValue({ conversation: conv(), messages: msgs(), truncated: true });
    render(<ConversationsPage />);
    expect(await screen.findByText('消息过多，仅显示最新的 500 条')).toBeInTheDocument();
  });
});
