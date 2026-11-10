import { describe, it, expect } from 'vitest';
import { giftCodeMetadata } from '../gift-code-meta';

describe('giftCodeMetadata', () => {
  it.each(['zh-CN', 'en-US', 'ko', 'ja', 'ru'])('%s: redeem pages stay out of the index', (locale) => {
    const m = giftCodeMetadata(locale);
    expect(m.robots).toEqual({ index: false, follow: false });
  });
});
