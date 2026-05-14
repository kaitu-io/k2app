import { describe, it, expect } from 'vitest';
import {
  chinaDetector,
  detectChineseReasonKey,
  REASON_INSTALLER_KEY,
  REASON_PREFIX_KEY,
} from '../china';

describe('detectChineseReasonKey', () => {
  describe('installer signal (strong)', () => {
    it('matches Xiaomi store installer', () => {
      expect(detectChineseReasonKey('com.example.app', 'com.xiaomi.market')).toBe(REASON_INSTALLER_KEY);
    });
    it('matches Huawei AppGallery', () => {
      expect(detectChineseReasonKey('com.example.app', 'com.huawei.appmarket')).toBe(REASON_INSTALLER_KEY);
    });
    it('matches Tencent MyApp (应用宝)', () => {
      expect(detectChineseReasonKey('com.example.app', 'com.tencent.android.qqdownloader')).toBe(REASON_INSTALLER_KEY);
    });
    it('matches OPPO Heytap', () => {
      expect(detectChineseReasonKey('com.example.app', 'com.heytap.market')).toBe(REASON_INSTALLER_KEY);
    });
    it('matches vivo store', () => {
      expect(detectChineseReasonKey('com.example.app', 'com.bbk.appstore')).toBe(REASON_INSTALLER_KEY);
    });
    it('Google Play installer with non-Chinese package returns null', () => {
      expect(detectChineseReasonKey('com.android.chrome', 'com.android.vending')).toBeNull();
    });
    it('Google Play installer with Chinese package still matches via prefix', () => {
      expect(detectChineseReasonKey('com.tencent.mm', 'com.android.vending')).toBe(REASON_PREFIX_KEY);
    });
    it('null installer falls back to prefix', () => {
      expect(detectChineseReasonKey('com.tencent.mm', null)).toBe(REASON_PREFIX_KEY);
    });
    it('undefined installer falls back to prefix', () => {
      expect(detectChineseReasonKey('com.tencent.mm', undefined)).toBe(REASON_PREFIX_KEY);
    });
  });

  describe('prefix signal — known Chinese namespaces', () => {
    it.each([
      ['com.tencent.mm', 'WeChat'],
      ['com.tencent.mobileqq', 'QQ'],
      ['com.eg.android.AlipayGphone', 'Alipay (exact match)'],
      ['com.alibaba.android.rimet', 'DingTalk'],
      ['com.taobao.taobao', 'Taobao'],
      ['com.tmall.wireless', 'Tmall'],
      ['com.baidu.searchbox', 'Baidu app'],
      ['com.bytedance.android', 'Bytedance'],
      ['com.ss.android.ugc.aweme', 'Douyin (ss.android namespace)'],
      ['com.netease.cloudmusic', 'NetEase Music'],
      ['com.sina.weibo', 'Weibo'],
      ['com.qihoo360.mobilesafe', '360 Mobile Safe'],
      ['com.miui.calculator', 'MIUI Calculator'],
      ['com.xiaomi.scanner', 'Xiaomi Scanner'],
      ['com.huawei.health', 'Huawei Health'],
      ['com.iflytek.inputmethod', 'iFlyTek IME'],
      ['com.autonavi.minimap', 'Amap'],
      ['com.iqiyi.android', 'iQIYI'],
      ['com.bilibili.app.in', 'Bilibili'],
      ['com.zhihu.android', 'Zhihu'],
      ['com.meituan.android', 'Meituan'],
      ['com.dianping.v1', 'Dianping'],
      ['com.didi.passenger', 'Didi'],
      ['com.kuaishou.nebula', 'Kuaishou Nebula'],
      ['com.smile.gifmaker', 'Kuaishou main (exact)'],
      ['com.jingdong.app.mall', 'JD'],
      ['cmb.pb', '招商银行'],
      ['com.UCMobile', 'UC Browser (exact)'],
    ])('detects %s (%s) as prefix', (pkg) => {
      expect(detectChineseReasonKey(pkg, null)).toBe(REASON_PREFIX_KEY);
    });
  });

  describe('non-Chinese apps return null', () => {
    it.each([
      ['com.android.chrome', 'Chrome (Google system)'],
      ['com.google.android.youtube', 'YouTube'],
      ['com.google.android.gms', 'Google Play Services'],
      ['com.android.shell', 'Android shell'],
      ['com.android.settings', 'Android Settings'],
      ['com.facebook.katana', 'Facebook'],
      ['com.zhiliaoapp.musically', 'TikTok international (uses cn-sounding name but NOT Chinese)'],
      ['org.mozilla.firefox', 'Firefox'],
      ['com.whatsapp', 'WhatsApp'],
      ['com.microsoft.emmx', 'Edge'],
      ['com.spotify.music', 'Spotify'],
    ])('does NOT detect %s (%s)', (pkg) => {
      expect(detectChineseReasonKey(pkg, null)).toBeNull();
      expect(detectChineseReasonKey(pkg, 'com.android.vending')).toBeNull();
    });
  });

  describe('edge cases', () => {
    it('empty package returns null', () => {
      expect(detectChineseReasonKey('', null)).toBeNull();
    });
    it('prefix entry without trailing dot does not match sub-namespaces', () => {
      expect(detectChineseReasonKey('com.UCMobileFake.app', null)).toBeNull();
    });
    it('partial prefix overlap does not match', () => {
      expect(detectChineseReasonKey('com.tencentfake.app', null)).toBeNull();
    });
    it('cn.* not matched (intentionally too broad)', () => {
      expect(detectChineseReasonKey('cn.example.foo', null)).toBeNull();
    });
  });
});

describe('chinaDetector.detect()', () => {
  it('exposes stable region + i18n key fields', () => {
    expect(chinaDetector.region).toBe('cn');
    expect(chinaDetector.sectionTitleKey).toBe('dashboard:appBypass.cn.section');
    expect(chinaDetector.noteSmartKey).toBe('dashboard:appBypass.cn.noteSmart');
    expect(chinaDetector.noteGlobalKey).toBe('dashboard:appBypass.cn.noteGlobal');
  });

  it('returns only Chinese apps, alphabetically sorted by zh-Hans-CN', () => {
    const detected = chinaDetector.detect([
      { packageName: 'com.tencent.mm', label: '微信' },
      { packageName: 'com.android.chrome', label: 'Chrome' },
      { packageName: 'com.eg.android.AlipayGphone', label: '支付宝' },
      { packageName: 'com.google.android.youtube', label: 'YouTube' },
    ]);
    // zh-Hans-CN collation sorts by pinyin: 微信 (wei) < 支付宝 (zhi).
    expect(detected.map((d) => d.packageName)).toEqual([
      'com.tencent.mm',
      'com.eg.android.AlipayGphone',
    ]);
    expect(detected[0].reasonKey).toBe(REASON_PREFIX_KEY);
    expect(detected[1].reasonKey).toBe(REASON_PREFIX_KEY);
  });

  it('installer-source apps carry the installer reason key', () => {
    const detected = chinaDetector.detect([
      {
        packageName: 'com.example.random',
        label: 'Random',
        installerPackageName: 'com.xiaomi.market',
      },
    ]);
    expect(detected).toHaveLength(1);
    expect(detected[0].reasonKey).toBe(REASON_INSTALLER_KEY);
  });

  it('returns empty list for empty input', () => {
    expect(chinaDetector.detect([])).toEqual([]);
  });

  it('preserves iconUrl when provided', () => {
    const detected = chinaDetector.detect([
      { packageName: 'com.tencent.mm', label: 'WeChat', iconUrl: 'data:image/png;base64,XYZ' },
    ]);
    expect(detected[0].iconUrl).toBe('data:image/png;base64,XYZ');
  });
});
