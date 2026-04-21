'use client';

import { useTranslations } from 'next-intl';
import { ArrowUpRight } from 'lucide-react';

export default function WeChatBrowserGuide() {
  const t = useTranslations();

  return (
    <div
      className="fixed inset-0 bg-black/95 text-white overflow-y-auto"
      style={{ zIndex: 2147483647 }}
    >
      <div className="min-h-full flex flex-col px-6 py-8">
        <div className="flex justify-end">
          <div className="flex flex-col items-center">
            <ArrowUpRight className="w-14 h-14 text-yellow-300 animate-bounce" strokeWidth={2.5} />
            <span className="text-xs text-yellow-300 mt-1 font-medium whitespace-nowrap">
              {t('purchase.wechatGuide.tapHere')}
            </span>
          </div>
        </div>

        <div className="flex-1 flex flex-col items-center justify-center mt-10">
          <div className="max-w-sm w-full text-center space-y-6">
            <h2 className="text-2xl font-bold leading-relaxed">
              {t('purchase.wechatGuide.title')}
            </h2>

            <div className="bg-white/10 rounded-xl p-5 text-left space-y-3 text-base leading-relaxed">
              <p className="flex items-start">
                <span className="inline-flex items-center justify-center shrink-0 bg-yellow-300 text-black rounded-full w-6 h-6 text-sm font-bold mr-2 mt-0.5">{1}</span>
                <span>{t('purchase.wechatGuide.step1')}</span>
              </p>
              <p className="flex items-start">
                <span className="inline-flex items-center justify-center shrink-0 bg-yellow-300 text-black rounded-full w-6 h-6 text-sm font-bold mr-2 mt-0.5">{2}</span>
                <span>{t('purchase.wechatGuide.step2')}</span>
              </p>
            </div>

            <p className="text-sm text-white/70 leading-relaxed">
              {t('purchase.wechatGuide.reason')}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
