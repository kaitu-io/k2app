import { describe, expect, it } from 'vitest';
import { pickMessages } from '../src/i18n/pick-messages';

const messages = {
  nav: { nav: { login: 'L' }, footer: { copyright: 'C' }, languageDetection: { a: 1 } },
  chat: { title: 'T' },
  errors: { badRequest: 'x', conflict: 'y' },
};

describe('pickMessages', () => {
  it('keeps only the listed <file>.<top> paths', () => {
    expect(pickMessages(messages, ['nav.nav'])).toEqual({ nav: { nav: { login: 'L' } } });
  });

  it('a bare file name keeps the whole file', () => {
    expect(pickMessages(messages, ['chat'])).toEqual({ chat: { title: 'T' } });
  });

  it('merges several tops of one file and several files', () => {
    expect(pickMessages(messages, ['nav.nav', 'nav.footer', 'chat'])).toEqual({
      nav: { nav: { login: 'L' }, footer: { copyright: 'C' } },
      chat: { title: 'T' },
    });
  });

  it('a whole-file entry wins over a partial one, in either order', () => {
    expect(pickMessages(messages, ['errors', 'errors.badRequest'])).toEqual({ errors: messages.errors });
    expect(pickMessages(messages, ['errors.badRequest', 'errors'])).toEqual({ errors: messages.errors });
  });

  it('only the first two segments count', () => {
    expect(pickMessages(messages, ['nav.nav.login'])).toEqual({ nav: { nav: { login: 'L' } } });
  });

  it('ignores paths that do not exist and never mutates the source', () => {
    const before = JSON.stringify(messages);
    expect(pickMessages(messages, ['nope', 'nav.nope'])).toEqual({});
    expect(JSON.stringify(messages)).toBe(before);
  });
});
