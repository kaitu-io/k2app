import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import Image from 'next/image';
import QRCode from 'qrcode';
import { routing } from '@/i18n/routing';
import { getBrand } from '@/lib/brand-server';
import { APP_STORE_PROVIDER_TOKEN, appStoreUrl, qrTargetUrl, resolveAdCode } from '@/lib/ad-landing';
import AdLandingClient from './AdLandingClient';

type Locale = (typeof routing.locales)[number];
type SearchParams = Record<string, string | string[] | undefined>;

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string; code: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale: locale as Locale, namespace: 'adLanding' });
  return {
    title: t('adLanding.metaTitle'),
    // 广告着陆页不进搜索引擎，免得与 SEO 页互相稀释
    robots: { index: false, follow: false },
  };
}

function toSearchString(sp: SearchParams): string {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(sp)) {
    if (v === undefined) continue;
    for (const one of Array.isArray(v) ? v : [v]) qs.append(k, one);
  }
  const s = qs.toString();
  return s ? `?${s}` : '';
}

/**
 * X 广告着陆页 /go/<码>。码只做归因：未知码按稳定版渲染（不 404），码原样进
 * App Store ct 与二维码。二维码在服务端本地生成，内容是同一页的 q 码 + utm_*。
 */
export default async function AdLandingPage({
  params,
  searchParams,
}: {
  params: Promise<{ locale: string; code: string }>;
  searchParams: Promise<SearchParams>;
}) {
  const { locale: rawLocale, code: rawCode } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const brand = await getBrand();
  const { code, entry } = resolveAdCode(rawCode);
  const search = toSearchString(await searchParams);
  const qrSvg = await QRCode.toString(qrTargetUrl(brand.baseUrl, locale, code, search), {
    type: 'svg',
    margin: 1,
    color: { dark: '#14231c', light: '#ffffff' },
  });

  return (
    <div
      className="min-h-screen bg-[#f4f1ea] text-[#14231c] antialiased"
      style={{ fontFamily: "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Helvetica Neue', 'Microsoft YaHei', sans-serif" }}
    >
      <header className="mx-auto flex w-full max-w-[1180px] items-center gap-2 px-5 py-4 lg:px-16 lg:py-6">
        <Image src="/kaitu-icon.png" alt="" width={28} height={28} className="rounded-[8px]" />
        <span className="text-[17px] font-bold">{brand.wordmark}</span>
      </header>
      <AdLandingClient
        code={code}
        variant={entry.variant}
        appStoreHref={appStoreUrl(brand.storeLinks.ios, code, APP_STORE_PROVIDER_TOKEN)}
        qrSvg={qrSvg}
      />
    </div>
  );
}
