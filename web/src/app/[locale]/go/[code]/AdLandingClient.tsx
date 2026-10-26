'use client';

import { useEffect, useRef, useState } from 'react';
import Image from 'next/image';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { track } from '@/lib/funnel';
import { detectAdPlatform, type AdPlatform, type AdVariant } from '@/lib/ad-landing';

export interface AdLandingClientProps {
  code: string;
  variant: AdVariant;
  appStoreHref: string;
  qrSvg: string;
}

const INK = '#14231c';
const MUTED = '#4d5c54';
const PRIMARY_BTN =
  'flex h-[54px] w-full items-center justify-center rounded-[14px] bg-[#14231c] text-[17px] font-semibold text-[#f4f1ea] no-underline transition-opacity hover:opacity-90';

/**
 * 广告着陆页的客户端部分：设备分流 + CTA + 埋点。
 *
 * SSR 与首帧一律按 iPhone 渲染（投放受众主体，App Store 按钮），挂载后再按
 * UA 改成桌面二维码卡或安卓下载按钮——不会在 iPhone 上闪出二维码。
 * page_view 由全站 FunnelPageView 发，这里只发 install_click / qr_view。
 */
export default function AdLandingClient({ variant, appStoreHref, qrSvg }: AdLandingClientProps) {
  const t = useTranslations('adLanding');
  const [platform, setPlatform] = useState<AdPlatform>('ios');
  const qrReported = useRef(false);

  useEffect(() => {
    setPlatform(detectAdPlatform(navigator.userAgent, navigator.maxTouchPoints));
  }, []);

  useEffect(() => {
    if (platform !== 'desktop' || qrReported.current) return;
    qrReported.current = true;
    track('qr_view', { source: 'ad' });
  }, [platform]);

  const clickIos = () => track('install_click', { plan: 'ios', source: 'ad' });
  const clickAndroid = () => track('install_click', { plan: 'android', source: 'ad' });

  const iosButton = (
    <a href={appStoreHref} onClick={clickIos} className={PRIMARY_BTN}>
      {t('adLanding.ctaAppStore')}
    </a>
  );
  const androidButton = (
    <Link href="/install" onClick={clickAndroid} className={PRIMARY_BTN}>
      {t('adLanding.ctaAndroid')}
    </Link>
  );
  const openOnPhone = (
    <a href={appStoreHref} onClick={clickIos} className="text-[14px] text-[#0b6b46] underline-offset-4 hover:underline">
      {t('adLanding.openOnPhone')}{' →'}
    </a>
  );

  const primaryCta =
    platform === 'desktop' ? (
      <div className="flex flex-col items-center gap-5 rounded-[24px] border border-[#e3ddd0] bg-white p-8 shadow-[0_20px_50px_rgba(20,35,28,0.08)]">
        <div className="text-[22px] font-bold">{t('adLanding.qrTitle')}</div>
        <div
          className="h-[200px] w-[200px] [&>svg]:h-full [&>svg]:w-full"
          // 我方服务端用 qrcode 从固定格式的 URL 生成的 SVG，不含用户 HTML
          dangerouslySetInnerHTML={{ __html: qrSvg }}
        />
        <p className="m-0 max-w-[300px] text-center text-[15px] leading-relaxed" style={{ color: MUTED }}>
          {t('adLanding.qrHint')}
        </p>
        {openOnPhone}
      </div>
    ) : platform === 'android' ? (
      androidButton
    ) : (
      iosButton
    );

  const bottomCta = platform === 'desktop' ? openOnPhone : platform === 'android' ? androidButton : iosButton;

  const why = [
    ['oneTapTitle', 'oneTapBody'],
    ['devicesTitle', 'devicesBody'],
    ['coexistTitle', 'coexistBody'],
  ] as const;

  return (
    <>
      <section className="mx-auto w-full max-w-[1180px] lg:grid lg:grid-cols-2 lg:items-center lg:gap-16 lg:px-16 lg:pb-16 lg:pt-6">
        <div className="relative aspect-square max-h-[45svh] w-full overflow-hidden lg:max-h-none lg:rounded-[28px]">
          <Image
            src="/ads/x-hero.jpg"
            alt={t('adLanding.heroAlt')}
            fill
            priority
            sizes="(min-width: 1024px) 560px, 100vw"
            className="object-cover object-[50%_35%]"
          />
          <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-[#0d1813]/90 via-[#0d1813]/55 to-transparent px-6 pb-7 pt-28 lg:px-9 lg:pb-9">
            <h1 className="m-0 text-[30px] font-bold leading-[1.25] tracking-[-0.01em] text-[#f4f1ea] lg:text-[36px]">
              {t('adLanding.heroTitle')}
            </h1>
          </div>
        </div>

        <div className="flex flex-col gap-4 px-6 pb-12 pt-8 lg:px-0 lg:pb-0 lg:pt-0">
          <p className="m-0 text-[22px] font-semibold leading-[1.45] lg:text-[28px]">{t(`adLanding.subtitle.${variant}`)}</p>
          <div className="mt-2">{primaryCta}</div>
          <p className="m-0 text-center text-[13px] text-[#5d6b63]">{t('adLanding.platforms')}</p>
        </div>
      </section>

      <section className="mx-auto w-full max-w-[720px] px-6 py-10 lg:py-14">
        <h2 className="m-0 mb-6 text-[22px] font-bold">{t('adLanding.why.title')}</h2>
        <div className="flex flex-col gap-5">
          {why.map(([title, body]) => (
            <div key={title} className="flex gap-4">
              <div className="mt-1 h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: INK }} />
              <div className="flex flex-col gap-1">
                <div className="text-[16px] font-semibold">{t(`adLanding.why.${title}`)}</div>
                <div className="text-[14px] leading-relaxed" style={{ color: MUTED }}>
                  {t(`adLanding.why.${body}`)}
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="mx-auto flex w-full max-w-[720px] flex-col items-center gap-4 border-t border-[#ddd6c8] px-6 pb-10 pt-8">
        <div className="w-full max-w-[420px] text-center">{bottomCta}</div>
        <p className="m-0 max-w-[520px] text-center text-[12px] leading-relaxed text-[#5d6b63]">{t('adLanding.trialNote')}</p>
      </section>

      <footer className="mx-auto flex w-full max-w-[720px] gap-4 px-6 pb-8 text-[12px] text-[#6b766f]">
        <Link href="/privacy" className="text-[#6b766f]">
          {t('adLanding.privacy')}
        </Link>
        <Link href="/terms" className="text-[#6b766f]">
          {t('adLanding.terms')}
        </Link>
      </footer>
    </>
  );
}
