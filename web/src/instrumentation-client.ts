import * as Sentry from '@sentry/nextjs';
import {
  dropChatwootSdkErrors,
  dropInjectedMatchMediaCircularJsonErrors,
  dropNativePostMessageRejections,
  dropOutdatedBrowserSyntaxErrors,
  dropRscNavigationFallbackRejections,
  scrubChatResumeBreadcrumb,
  scrubChatResumeEvent,
} from '@/lib/sentry-filters';
import { stripChatResume } from '@/components/chat/resume-script';

// MUST stay the first statement of this module. An emailed chat-resume link
// carries its token in the URL fragment (#chat=…); this moves it to
// window.__chatResume and rewrites the address bar before Sentry can see it.
// Next runs this file before `beforeInteractive` scripts, so the inline copy
// in the root layout is too late for Sentry and is only a fallback.
// Why "before Sentry.init" is enough even though the imports above are
// evaluated first: the SDK reads `location` and patches `history` inside
// `init()` (Replay's initialUrl, the history breadcrumb instrumentation), not
// at import time — so neither the token nor our own replaceState is observed.
if (typeof window !== 'undefined') stripChatResume(window);

const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;

if (dsn) {
  Sentry.init({
    dsn,
    tracesSampleRate: 1.0,
    replaysSessionSampleRate: 1.0,
    replaysOnErrorSampleRate: 1.0,
    sendDefaultPii: true,
    debug: false,
    beforeBreadcrumb: scrubChatResumeBreadcrumb,
    beforeSend: (rawEvent) => {
      const event = scrubChatResumeEvent(rawEvent);
      const afterChatwoot = dropChatwootSdkErrors(event);
      if (!afterChatwoot) return null;
      const afterNativePostMessage = dropNativePostMessageRejections(afterChatwoot);
      if (!afterNativePostMessage) return null;
      const afterSyntaxErrors = dropOutdatedBrowserSyntaxErrors(afterNativePostMessage);
      if (!afterSyntaxErrors) return null;
      const afterCircularJson = dropInjectedMatchMediaCircularJsonErrors(afterSyntaxErrors);
      if (!afterCircularJson) return null;
      return dropRscNavigationFallbackRejections(afterCircularJson);
    },
    integrations: [
      Sentry.replayIntegration({
        // Show UI text so we can see error messages and page state when debugging.
        // Inputs (passwords, emails) stay masked by default — Replay's mask covers
        // <input>/<textarea>/<select> regardless of maskAllText.
        maskAllText: false,
        blockAllMedia: true,
      }),
    ],
  });
}

export const onRouterTransitionStart = Sentry.captureRouterTransitionStart;
