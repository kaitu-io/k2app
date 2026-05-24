import * as Sentry from '@sentry/nextjs';
import {
  dropChatwootSdkErrors,
  dropNativePostMessageRejections,
  dropOutdatedBrowserSyntaxErrors,
} from '@/lib/sentry-filters';

const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;

if (dsn) {
  Sentry.init({
    dsn,
    tracesSampleRate: 1.0,
    replaysSessionSampleRate: 1.0,
    replaysOnErrorSampleRate: 1.0,
    sendDefaultPii: true,
    debug: false,
    beforeSend: (event) => {
      const afterChatwoot = dropChatwootSdkErrors(event);
      if (!afterChatwoot) return null;
      const afterNativePostMessage = dropNativePostMessageRejections(afterChatwoot);
      if (!afterNativePostMessage) return null;
      return dropOutdatedBrowserSyntaxErrors(afterNativePostMessage);
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
