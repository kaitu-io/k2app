import { describe, it, expect } from 'vitest';
import { resolveAdCode, detectAdPlatform, appStoreUrl, qrCodeFor, qrTargetUrl } from '../ad-landing';

describe('resolveAdCode', () => {
  it('known code', () => expect(resolveAdCode('xb1')).toEqual({ code: 'xb1', entry: { variant: 'compare', channel: 'x' } }));
  it('case-insensitive', () => expect(resolveAdCode('XA1').code).toBe('xa1'));
  it('unknown code falls back to stable but keeps the code', () =>
    expect(resolveAdCode('zz9')).toEqual({ code: 'zz9', entry: { variant: 'stable', channel: 'x' } }));
  it('strips anything outside [a-z0-9-] so a mangled link still resolves', () => expect(resolveAdCode('xa1%20').code).toBe('xa1'));
});
describe('detectAdPlatform', () => {
  const iphoneX = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Twitter for iPhone/10.60';
  it('X in-app browser on iPhone → ios', () => expect(detectAdPlatform(iphoneX, 5)).toBe('ios'));
  it('iPadOS desktop UA with touch → ios', () =>
    expect(detectAdPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15', 5)).toBe('ios'));
  it('Mac without touch → desktop', () =>
    expect(detectAdPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15', 0)).toBe('desktop'));
  it('Android (incl. X app) → android', () =>
    expect(detectAdPlatform('Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/129 Mobile Safari/537.36 TwitterAndroid', 5)).toBe('android'));
  it('Windows → desktop', () => expect(detectAdPlatform('Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/129', 0)).toBe('desktop'));
});
describe('appStoreUrl', () => {
  const base = 'https://apps.apple.com/app/id6448744655';
  it('no token → plain link', () => expect(appStoreUrl(base, 'xa1', '')).toBe(base));
  it('with token → pt, ct, mt=8', () => expect(appStoreUrl(base, 'xa1', '12345')).toBe(`${base}?pt=12345&ct=xa1&mt=8`));
});
describe('qrCodeFor', () => {
  it('appends q', () => expect(qrCodeFor('xa1')).toBe('xa1q'));
  it('does not double the suffix for a registered qr code', () => expect(qrCodeFor('xa1q')).toBe('xa1q'));
  it('mac code gets mq', () => expect(qrCodeFor('xb1m')).toBe('xb1mq'));
  it('mac qr code unchanged', () => expect(qrCodeFor('xb1mq')).toBe('xb1mq'));
  it('mac codes resolve to compare', () => {
    expect(resolveAdCode('xb1m').entry.variant).toBe('compare');
    expect(resolveAdCode('xb1mq').entry.variant).toBe('compare');
  });
});
describe('qrTargetUrl', () => {
  it('keeps only utm_* and adds the q code', () =>
    expect(qrTargetUrl('https://kaitu.io', 'zh-CN', 'xa1', '?utm_source=x&utm_content=xa1&twclid=abc'))
      .toBe('https://kaitu.io/zh-CN/go/xa1q?utm_source=x&utm_content=xa1'));
});
