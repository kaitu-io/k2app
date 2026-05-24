import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import type { ErrorEvent } from '@sentry/nextjs';
import {
  dropChatwootSdkErrors,
  dropFailedFormDataParseFromBotProbes,
  dropNativePostMessageRejections,
  dropOutdatedBrowserSyntaxErrors,
} from '../sentry-filters';

const originalUA = window.navigator.userAgent;

function spoofUA(ua: string) {
  Object.defineProperty(window.navigator, 'userAgent', { value: ua, configurable: true });
}

const IOS_12 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 12_5_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1';
const IOS_17 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1';
const CHROME_DESKTOP =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

function syntaxErrorEvent(value: string): ErrorEvent {
  return {
    exception: {
      values: [{ type: 'SyntaxError', value }],
    },
  } as ErrorEvent;
}

afterEach(() => {
  spoofUA(originalUA);
});

describe('dropOutdatedBrowserSyntaxErrors', () => {
  describe('the targeted lookbehind SyntaxError', () => {
    const value = 'Invalid regular expression: invalid group specifier name';

    it('drops the event on outdated iOS (12.5)', () => {
      spoofUA(IOS_12);
      expect(dropOutdatedBrowserSyntaxErrors(syntaxErrorEvent(value))).toBeNull();
    });

    it('KEEPS the event on modern iOS Safari 17 (would be a real regression)', () => {
      spoofUA(IOS_17);
      const event = syntaxErrorEvent(value);
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('KEEPS the event on Chrome desktop (would be a real regression)', () => {
      spoofUA(CHROME_DESKTOP);
      const event = syntaxErrorEvent(value);
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });
  });

  describe('unrelated errors pass through unchanged', () => {
    beforeEach(() => spoofUA(IOS_12));

    it('keeps TypeErrors on outdated iOS', () => {
      const event = {
        exception: { values: [{ type: 'TypeError', value: "Cannot read property 'x' of undefined" }] },
      } as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps other SyntaxErrors not about lookbehind', () => {
      const event = syntaxErrorEvent('Unexpected token <');
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps events with no exception payload', () => {
      const event = { message: 'just a log' } as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps events with empty exception values', () => {
      const event = { exception: { values: [] } } as unknown as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });
  });
});

function chatwootEvent(frameFilenames: string[]): ErrorEvent {
  return {
    exception: {
      values: [
        {
          type: 'TypeError',
          value: "Cannot read properties of null (reading 'postMessage')",
          stacktrace: { frames: frameFilenames.map((filename) => ({ filename })) },
        },
      ],
    },
  } as ErrorEvent;
}

describe('dropChatwootSdkErrors', () => {
  it('drops the Chatwoot postMessage TypeError (frame URL = self-hosted SDK)', () => {
    const event = chatwootEvent(['https://chat.anc.52j.me/packs/js/sdk.js']);
    expect(dropChatwootSdkErrors(event)).toBeNull();
  });

  it('drops when the Chatwoot frame is buried below a Sentry helper frame', () => {
    const event = chatwootEvent([
      'webpack-internal:///./node_modules/@sentry/browser/build/npm/esm/prod/helpers.js',
      'https://chat.anc.52j.me/packs/js/sdk.js',
    ]);
    expect(dropChatwootSdkErrors(event)).toBeNull();
  });

  it('drops when filename is the Sentry app:// normalized form', () => {
    const event = chatwootEvent(['app:///packs/js/sdk.js']);
    expect(dropChatwootSdkErrors(event)).toBeNull();
  });

  it('KEEPS the same TypeError when it originates in our own code (real regression)', () => {
    const event = chatwootEvent([
      'https://overleap.io/_next/static/chunks/main-abc123.js',
      'https://overleap.io/_next/static/chunks/app/[locale]/layout-def456.js',
    ]);
    expect(dropChatwootSdkErrors(event)).toBe(event);
  });

  it('keeps events with no stacktrace', () => {
    const event = {
      exception: { values: [{ type: 'TypeError', value: 'something' }] },
    } as ErrorEvent;
    expect(dropChatwootSdkErrors(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropChatwootSdkErrors(event)).toBe(event);
  });

  it('keeps events with empty frames array', () => {
    const event = {
      exception: {
        values: [{ type: 'TypeError', value: 'x', stacktrace: { frames: [] } }],
      },
    } as unknown as ErrorEvent;
    expect(dropChatwootSdkErrors(event)).toBe(event);
  });
});

function nextOnRequestErrorEvent(
  value: string,
  mechanismType: string = 'auto.function.nextjs.on_request_error',
  type: string = 'TypeError'
): ErrorEvent {
  return {
    exception: {
      values: [{ type, value, mechanism: { handled: false, type: mechanismType } }],
    },
  } as ErrorEvent;
}

describe('dropFailedFormDataParseFromBotProbes', () => {
  it('drops the exact Next.js Server-Action FormData parse TypeError', () => {
    // Reproduces Sentry issue 7494712884: bot POSTs junk body to
    // /[locale]/[...slug]/page, Next.js tries to dispatch as Server Action,
    // undici throws inside captureRequestError.
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBeNull();
  });

  it('drops the variant without trailing period (defensive against framework wording changes)', () => {
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBeNull();
  });

  it('KEEPS the same string if it ever appears OUTSIDE the onRequestError mechanism', () => {
    // Hypothetical: app code or another library throws the same string.
    // We do NOT mask it — only the framework's request-error path is noise.
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.', 'generic');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('KEEPS other TypeErrors coming through onRequestError (real app bugs)', () => {
    const event = nextOnRequestErrorEvent("Cannot read properties of undefined (reading 'x')");
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('KEEPS non-TypeError exceptions with the same value (unexpected, surface it)', () => {
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.', undefined, 'Error');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with empty exception values', () => {
    const event = { exception: { values: [] } } as unknown as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no mechanism field', () => {
    const event = {
      exception: {
        values: [{ type: 'TypeError', value: 'Failed to parse body as FormData.' }],
      },
    } as unknown as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });
});

function nativePostMessageRejectionEvent(
  framesFilenames: string[] | null,
  type: string = 'InvalidAccessError',
  mechanismType: string = 'auto.browser.global_handlers.onunhandledrejection'
): ErrorEvent {
  const stacktrace =
    framesFilenames === null
      ? undefined
      : { frames: framesFilenames.map((filename) => ({ filename, function: 'postMessage' })) };
  return {
    exception: {
      values: [
        {
          type,
          value: 'The object does not support the operation or argument.',
          mechanism: { handled: false, type: mechanismType },
          stacktrace,
        },
      ],
    },
  } as ErrorEvent;
}

describe('dropNativePostMessageRejections', () => {
  it('drops the Baidu Explorer / iOS postMessage InvalidAccessError (only [native code] frame)', () => {
    // Reproduces Sentry issue 7496826552: iOS Safari/Baidu WebView strips
    // the SDK frame from native errors, leaving only "postMessage in [native code]".
    const event = nativePostMessageRejectionEvent(['[native code]']);
    expect(dropNativePostMessageRejections(event)).toBeNull();
  });

  it('drops when the exception has no stacktrace at all', () => {
    const event = nativePostMessageRejectionEvent(null);
    expect(dropNativePostMessageRejections(event)).toBeNull();
  });

  it('drops when frames array is empty', () => {
    const event = nativePostMessageRejectionEvent([]);
    expect(dropNativePostMessageRejections(event)).toBeNull();
  });

  it('KEEPS the InvalidAccessError when an app frame is present (would be a real bug)', () => {
    const event = nativePostMessageRejectionEvent([
      '[native code]',
      'https://www.kaitu.io/_next/static/chunks/main-abc123.js',
    ]);
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });

  it('KEEPS the same DOMException when it is a synchronous error (not an unhandled rejection)', () => {
    const event = nativePostMessageRejectionEvent(['[native code]'], 'InvalidAccessError', 'generic');
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });

  it('KEEPS other DOMException types with same shape (only InvalidAccessError is known noise)', () => {
    const event = nativePostMessageRejectionEvent(['[native code]'], 'SecurityError');
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });

  it('keeps events with empty exception values', () => {
    const event = { exception: { values: [] } } as unknown as ErrorEvent;
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });

  it('keeps events with no mechanism field', () => {
    const event = {
      exception: {
        values: [
          {
            type: 'InvalidAccessError',
            value: 'The object does not support the operation or argument.',
            stacktrace: { frames: [{ filename: '[native code]', function: 'postMessage' }] },
          },
        ],
      },
    } as unknown as ErrorEvent;
    expect(dropNativePostMessageRejections(event)).toBe(event);
  });
});
