import { describe, it, expect } from 'vitest';
import { linkParts } from '../chat/linkify';

const urls = (text: string) => linkParts(text).flatMap((p) => (typeof p === 'string' ? [] : [p.url]));
const joined = (text: string) => linkParts(text).map((p) => (typeof p === 'string' ? p : p.url)).join('');

describe('linkParts', () => {
  it('does not swallow adjacent CJK text or fullwidth punctuation into the link', () => {
    const text = '请访问https://kaitu.io/install，然后点击下载';
    expect(linkParts(text)).toEqual(['请访问', { url: 'https://kaitu.io/install' }, '，然后点击下载']);
    expect(urls('看这里https://example.com/a?b=1&c=2。谢谢')).toEqual(['https://example.com/a?b=1&c=2']);
    expect(urls('https://example.com/路径')).toEqual(['https://example.com/']);
  });

  it('trims trailing sentence punctuation', () => {
    expect(urls('see https://example.com/a?b=1, or https://example.com/b.')).toEqual([
      'https://example.com/a?b=1',
      'https://example.com/b',
    ]);
    expect(urls('really? https://example.com/x!?')).toEqual(['https://example.com/x']);
  });

  it('keeps a closing paren that balances one inside the URL, drops one that does not', () => {
    expect(urls('https://en.wikipedia.org/wiki/Foo_(bar)')).toEqual(['https://en.wikipedia.org/wiki/Foo_(bar)']);
    expect(urls('(see https://example.com/a)')).toEqual(['https://example.com/a']);
    expect(urls('(https://en.wikipedia.org/wiki/Foo_(bar)).')).toEqual(['https://en.wikipedia.org/wiki/Foo_(bar)']);
  });

  it('never links other schemes', () => {
    for (const text of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'JaVaScRiPt:alert(1)', 'ftp://x.y/z', 'http://', 'https://?x']) {
      expect(urls(text), text).toEqual([]);
    }
  });

  it('loses no text', () => {
    for (const text of ['', 'plain', 'a https://x.io/y, b http://z.io. c', '请访问https://kaitu.io/install，然后']) {
      expect(joined(text)).toBe(text);
    }
  });
});
