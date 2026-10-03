'use client';

import type { ReactNode } from 'react';
import type { ChatMessage, ChatWelcome } from '@/lib/chat-client';
import { cn } from '@/lib/utils';
import { linkParts } from './linkify';

/** 服务端系统事件（meta.event）。事件正文是写给客服后台的中文，访客看到的按事件名取本地化文案。 */
export const CHAT_EVENTS = ['transfer_human', 'handed_to_ai', 'closed', 'auto_closed'] as const;
export type ChatEventName = (typeof CHAT_EVENTS)[number];

export interface MessageListLabels {
  ai: string;
  staff: string;
  image: string;
  events: Record<ChatEventName, string>;
}

/** 认识的事件取本地化文案；不认识的（服务端新加了事件）不渲染，免得把中文正文露给别的语言的访客。 */
function eventLabel(m: ChatMessage, events: MessageListLabels['events']): string | null {
  const name = m.meta?.event;
  return typeof name === 'string' && (CHAT_EVENTS as readonly string[]).includes(name)
    ? events[name as ChatEventName]
    : null;
}

/** 纯文本渲染 + 只把 http(s) 链接变成 <a>。全程是 React 文本节点，不解析 HTML。 */
function linkify(text: string): ReactNode[] {
  let offset = 0;
  return linkParts(text).map((part) => {
    const key = offset;
    if (typeof part === 'string') {
      offset += part.length;
      return part;
    }
    offset += part.url.length;
    return (
      <a
        key={key}
        href={part.url}
        target="_blank"
        rel="noopener noreferrer nofollow"
        className="underline underline-offset-2 break-all"
      >
        {part.url}
      </a>
    );
  });
}

function Bubble({ mine, label, pending, children }: { mine: boolean; label?: string; pending?: boolean; children: ReactNode }) {
  return (
    <li className={cn('flex flex-col gap-1', mine ? 'items-end' : 'items-start')}>
      {label && <span className="text-xs text-muted-foreground px-1">{label}</span>}
      <div
        className={cn(
          'max-w-[85%] rounded-2xl px-3 py-2 text-sm whitespace-pre-wrap break-words',
          mine ? 'bg-primary text-primary-foreground rounded-br-sm' : 'bg-muted text-foreground rounded-bl-sm',
          pending && 'opacity-60',
        )}
      >
        {children}
      </div>
    </li>
  );
}

export default function MessageList({
  messages,
  welcome,
  labels,
}: {
  messages: readonly ChatMessage[];
  /** 选项表：option_reply 落库的是 value，气泡里显示对应的 label。 */
  welcome: ChatWelcome | null;
  labels: MessageListLabels;
}) {
  const optionLabel = (value: string) => welcome?.options.find((o) => o.value === value)?.label ?? value;

  return (
    <ul className="flex flex-col gap-3">
      {messages.map((m) => {
        const key = m.pending ? `c:${m.clientId}` : `m:${m.id}`;
        if (m.kind === 'event') {
          const text = eventLabel(m, labels.events);
          if (text === null) return null;
          return (
            <li key={key} className="text-center text-xs text-muted-foreground">
              {text}
            </li>
          );
        }
        // 只认识这几种；options 第一期不用，note 永不下发——其余一律不渲染
        if (m.kind !== 'text' && m.kind !== 'option_reply' && m.kind !== 'image') return null;

        const mine = m.senderType === 'visitor';
        // 对访客不显示客服真实姓名（senderName 有值也不用）
        const label = m.senderType === 'ai' ? labels.ai : m.senderType === 'staff' ? labels.staff : undefined;
        return (
          <Bubble key={key} mine={mine} label={label} pending={m.pending}>
            {m.kind === 'image' ? labels.image : m.kind === 'option_reply' ? optionLabel(m.content) : linkify(m.content)}
          </Bubble>
        );
      })}
    </ul>
  );
}
