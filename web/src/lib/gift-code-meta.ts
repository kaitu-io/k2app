import type { Metadata } from 'next';
import { siteBrand, localizedWordmark } from './brands';

const GIFT_META: Record<string, { title: string; description: (brand: string) => string }> = {
  'zh-CN': { title: '兑换授权码', description: (b) => `输入授权码，免费获取${b}会员` },
  'zh-TW': { title: '兌換授權碼', description: (b) => `輸入授權碼，免費獲取${b}會員` },
  'zh-HK': { title: '兌換授權碼', description: (b) => `輸入授權碼，免費獲取${b}會員` },
  'en-US': { title: 'Redeem a license key', description: (b) => `Enter your license key to get ${b} membership for free` },
  ko: { title: '라이선스 키 사용', description: (b) => `라이선스 키를 입력하고 ${b} 멤버십을 무료로 받으세요` },
  ja: { title: 'ライセンスキーを引き換える', description: (b) => `ライセンスキーを入力して ${b} のメンバーシップを無料で入手` },
  ru: { title: 'Активировать лицензионный ключ', description: (b) => `Введите лицензионный ключ и получите подписку ${b} бесплатно` },
};

/** Title / description of the license-key pages (/g and /g/<code>). */
export function giftCodeMetadata(locale: string): Metadata {
  const m = GIFT_META[locale] ?? GIFT_META['zh-CN'];
  const brand = localizedWordmark(siteBrand(), locale);
  return { title: `${m.title} | ${brand}`, description: m.description(brand) };
}
