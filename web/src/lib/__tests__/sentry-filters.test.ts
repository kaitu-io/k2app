import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import type { ErrorEvent } from '@sentry/nextjs';
import { dropOutdatedBrowserSyntaxErrors } from '../sentry-filters';

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
