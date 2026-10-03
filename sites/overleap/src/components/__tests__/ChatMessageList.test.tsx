import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import MessageList, { CHAT_EVENTS, type MessageListLabels } from '../chat/MessageList';
import { msg } from '@/lib/__tests__/chat-test-fakes';

const labels: MessageListLabels = {
  ai: 'AI',
  staff: 'Staff',
  image: '[img]',
  events: {
    transfer_human: 'L:transfer',
    handed_to_ai: 'L:handed',
    closed: 'L:closed',
    auto_closed: 'L:auto',
  },
};

const event = (id: number, name: string | null, content = `server-text-${id}`) =>
  msg(id, { senderType: 'system', kind: 'event', content, meta: name === null ? null : { event: name } });

// 事件正文是写给客服后台的中文；访客按事件名看到本地化文案，另一语言的访客不该看到中文正文
describe('MessageList system events', () => {
  it.each(CHAT_EVENTS)('renders the localized label for %s, not the server text', (name) => {
    render(<MessageList messages={[event(1, name, '已转人工')]} welcome={null} labels={labels} />);
    expect(screen.getByText(labels.events[name])).toBeTruthy();
    expect(screen.queryByText('已转人工')).toBeNull();
  });

  it('drops events it does not know (no meta, or a name added later on the server)', () => {
    render(<MessageList messages={[event(1, null), event(2, 'something_new')]} welcome={null} labels={labels} />);
    expect(screen.queryByText('server-text-1')).toBeNull();
    expect(screen.queryByText('server-text-2')).toBeNull();
    expect(screen.queryAllByRole('listitem')).toHaveLength(0);
  });
});
