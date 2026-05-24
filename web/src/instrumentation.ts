import * as Sentry from '@sentry/nextjs';
import { dropFailedFormDataParseFromBotProbes } from '@/lib/sentry-filters';

const beforeSend = (event: Sentry.ErrorEvent) =>
  dropFailedFormDataParseFromBotProbes(event);

export async function register() {
  const dsn = process.env.NEXT_PUBLIC_SENTRY_DSN;
  if (!dsn) return;

  if (process.env.NEXT_RUNTIME === 'nodejs') {
    Sentry.init({
      dsn,
      tracesSampleRate: 1.0,
      sendDefaultPii: true,
      debug: false,
      beforeSend,
    });
  }

  if (process.env.NEXT_RUNTIME === 'edge') {
    Sentry.init({
      dsn,
      tracesSampleRate: 1.0,
      sendDefaultPii: true,
      debug: false,
      beforeSend,
    });
  }
}

export const onRequestError = Sentry.captureRequestError;
