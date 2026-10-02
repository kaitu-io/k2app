import * as Sentry from '@sentry/nextjs';
import {
  dropFailedFormDataParseFromBotProbes,
  dropFailedServerActionLookupFromBotProbes,
  scrubChatApiBreadcrumb,
  scrubChatApiEvent,
  scrubChatApiSpan,
} from '@/lib/sentry-filters';

const beforeSend = (rawEvent: Sentry.ErrorEvent) => {
  const event = scrubChatApiEvent(rawEvent);
  const afterFormData = dropFailedFormDataParseFromBotProbes(event);
  if (!afterFormData) return null;
  return dropFailedServerActionLookupFromBotProbes(afterFormData);
};

// `/api/*` 是 rewrites：请求经过 Next 服务端，SDK（sendDefaultPii）会把请求体与 cookie 采进
// 事件。`/api/chat/*` 的请求体里有继续对话令牌与访客消息正文，cookie 是访客身份凭据——
// 这四个钩子只擦 chat 路由（见 lib/sentry-filters.ts），其余路由的行为不变。
// 服务端与 edge 两个 init 必须都带上：少一个，那个运行时就原样外发。
const chatScrubHooks = {
  beforeSend,
  beforeSendTransaction: scrubChatApiEvent,
  beforeSendSpan: scrubChatApiSpan,
  beforeBreadcrumb: scrubChatApiBreadcrumb,
};

export async function register() {
  const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;
  if (!dsn) return;

  if (process.env.NEXT_RUNTIME === 'nodejs') {
    Sentry.init({
      dsn,
      tracesSampleRate: 1.0,
      sendDefaultPii: true,
      debug: false,
      ...chatScrubHooks,
    });
  }

  if (process.env.NEXT_RUNTIME === 'edge') {
    Sentry.init({
      dsn,
      tracesSampleRate: 1.0,
      sendDefaultPii: true,
      debug: false,
      ...chatScrubHooks,
    });
  }
}

export const onRequestError = Sentry.captureRequestError;
