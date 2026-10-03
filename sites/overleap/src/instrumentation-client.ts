import * as Sentry from '@sentry/nextjs';
import {
  dropInjectedMatchMediaCircularJsonErrors,
  dropNativePostMessageRejections,
  dropOutdatedBrowserSyntaxErrors,
  dropRscNavigationFallbackRejections,
} from '@/lib/sentry-filters';
import { stripChatResume } from '@/components/chat/resume-script';

// First statement that touches the URL: take a chat resume token (#chat=…) out of
// the address bar before the SDK records the initial URL.
if (typeof window !== 'undefined') stripChatResume(window);

const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;

// Error reporting only. This is a no-logs privacy product: no session replay,
// no default PII (IP, cookies, user identifiers), no performance tracing of visitors.
if (dsn) {
  Sentry.init({
    dsn,
    tracesSampleRate: 0,
    sendDefaultPii: false,
    debug: false,
    beforeSend: (event) => {
      const a = dropNativePostMessageRejections(event);
      if (!a) return null;
      const b = dropOutdatedBrowserSyntaxErrors(a);
      if (!b) return null;
      const c = dropInjectedMatchMediaCircularJsonErrors(b);
      if (!c) return null;
      return dropRscNavigationFallbackRejections(c);
    },
  });
}

export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
