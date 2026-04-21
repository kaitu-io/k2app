import { describe, it, expect } from 'vitest';
import { isWeChatAndroid } from '../device-detection';

describe('isWeChatAndroid', () => {
  it('detects WeChat on Android', () => {
    const ua = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN ABI/arm64';
    expect(isWeChatAndroid(ua)).toBe(true);
  });

  it('returns false for WeChat on iOS', () => {
    const ua = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40(0x18002830) NetType/WIFI Language/zh_CN';
    expect(isWeChatAndroid(ua)).toBe(false);
  });

  it('returns false for Chrome on Android', () => {
    const ua = 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36';
    expect(isWeChatAndroid(ua)).toBe(false);
  });

  it('returns false for WeChat on desktop', () => {
    const ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/3.8.0';
    expect(isWeChatAndroid(ua)).toBe(false);
  });

  it('returns false for empty UA', () => {
    expect(isWeChatAndroid('')).toBe(false);
  });
});
