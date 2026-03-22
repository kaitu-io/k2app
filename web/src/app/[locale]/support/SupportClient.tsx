"use client";

import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import Image from 'next/image';
import {
  Activity,
  MousePointerClick,
  GraduationCap,
  Download,
  UserPlus,
  Monitor,
  Smartphone,
  Zap,
  LifeBuoy,
  Home,
  MessageCircle,
  Mail,
  ChevronDown,
} from 'lucide-react';
import { useState } from 'react';

const VIDEO_URL = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/kaitu_guide.mp4';

const openChat = () => {
  const w = window as unknown as Record<string, unknown>;
  if (w.$chatwoot && typeof (w.$chatwoot as Record<string, unknown>).toggle === 'function') {
    (w.$chatwoot as { toggle: (action: string) => void }).toggle('open');
  }
};

const email = ['bnb', '@', 'kaitu', '.io'].join('');

const FAQ_KEYS = [
  'multiDevice', 'verifyCode', 'paymentSafety', 'wechatPay',
  'windowsBlueScreen', 'macPassword', 'androidInstall',
  'globalMode', 'connectionFailed', 'platforms',
] as const;

function FaqItem({ questionKey, t }: { questionKey: string; t: (key: string) => string }) {
  const [open, setOpen] = useState(false);
  return (
    <Card
      className="p-6 cursor-pointer hover:shadow-lg transition-shadow"
      onClick={() => setOpen(!open)}
    >
      <div className="flex items-center justify-between">
        <h3 className="font-semibold text-foreground pr-4">
          {t(`guide-parents.faq.items.${questionKey}.question`)}
        </h3>
        <ChevronDown className={`w-5 h-5 text-muted-foreground flex-shrink-0 transition-transform ${open ? 'rotate-180' : ''}`} />
      </div>
      {open && (
        <p className="text-muted-foreground text-sm mt-3 pt-3 border-t border-border">
          {t(`guide-parents.faq.items.${questionKey}.answer`)}
        </p>
      )}
    </Card>
  );
}

export default function SupportClient() {
  const t = useTranslations();

  return (
    <div className="min-h-screen bg-background">
      <Header />

      {/* Hero Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-4">
            {t('guide-parents.hero.title')}
          </h1>
          <p className="text-xl text-muted-foreground mb-8">
            {t('guide-parents.hero.subtitle')}
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <Link href="/install">
              <Button size="lg">
                <Download className="w-5 h-5 mr-2" />
                {t('guide-parents.hero.downloadButton')}
              </Button>
            </Link>
            <a href="#quickstart">
              <Button size="lg" variant="outline">
                {t('guide-parents.hero.guideButton')}
              </Button>
            </a>
          </div>
        </div>
      </section>

      {/* Video Section */}
      <section className="pb-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-2xl font-bold text-foreground text-center mb-2">
            {t('guide-parents.video.title')}
          </h2>
          <p className="text-muted-foreground text-center mb-6">
            {t('guide-parents.video.description')}
          </p>
          <div className="rounded-xl overflow-hidden bg-black/50 shadow-2xl">
            <video
              controls
              preload="metadata"
              playsInline
              className="w-full aspect-video"
              poster=""
            >
              <source src={VIDEO_URL} type="video/mp4" />
            </video>
          </div>
        </div>
      </section>

      {/* Why Kaitu Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 bg-card/50">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-foreground text-center mb-12">
            {t('guide-parents.whyKaitu.title')}
          </h2>
          <div className="grid sm:grid-cols-3 gap-6">
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-green-900/50 rounded-full flex items-center justify-center">
                <Activity className="w-6 h-6 text-green-600" />
              </div>
              <h3 className="font-bold text-foreground mb-2">
                {t('guide-parents.whyKaitu.stable.title')}
              </h3>
              <p className="text-sm text-muted-foreground">
                {t('guide-parents.whyKaitu.stable.description')}
              </p>
            </Card>
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-blue-900/50 rounded-full flex items-center justify-center">
                <MousePointerClick className="w-6 h-6 text-blue-600" />
              </div>
              <h3 className="font-bold text-foreground mb-2">
                {t('guide-parents.whyKaitu.simple.title')}
              </h3>
              <p className="text-sm text-muted-foreground">
                {t('guide-parents.whyKaitu.simple.description')}
              </p>
            </Card>
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-purple-900/50 rounded-full flex items-center justify-center">
                <GraduationCap className="w-6 h-6 text-purple-600" />
              </div>
              <h3 className="font-bold text-foreground mb-2">
                {t('guide-parents.whyKaitu.education.title')}
              </h3>
              <p className="text-sm text-muted-foreground">
                {t('guide-parents.whyKaitu.education.description')}
              </p>
            </Card>
          </div>
        </div>
      </section>

      {/* Quick Start Section — 4 Steps */}
      <section id="quickstart" className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-foreground text-center mb-12">
            {t('guide-parents.quickStart.title')}
          </h2>
          <div className="space-y-10">

            {/* Step 1: 如何开通 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-blue-900/50 rounded-full flex items-center justify-center">
                <UserPlus className="w-6 h-6 text-blue-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-foreground mb-2">
                  <span className="text-blue-600 mr-2">1.</span>
                  {t('guide-parents.quickStart.step1.title')}
                </h3>
                <p className="text-muted-foreground mb-4">
                  {t('guide-parents.quickStart.step1.description')}
                </p>
                <div className="space-y-2 text-sm text-muted-foreground">
                  <p>• {t('guide-parents.quickStart.step1.tips.verifyCode')}</p>
                  <p>• {t('guide-parents.quickStart.step1.tips.payment')}</p>
                  <p>• {t('guide-parents.quickStart.step1.tips.wechatPay')}</p>
                  <p>• {t('guide-parents.quickStart.step1.tips.safety')}</p>
                </div>
              </div>
            </div>

            {/* Step 2: 如何安装 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-green-900/50 rounded-full flex items-center justify-center">
                <Download className="w-6 h-6 text-green-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-foreground mb-2">
                  <span className="text-green-600 mr-2">2.</span>
                  {t('guide-parents.quickStart.step2.title')}
                </h3>
                <p className="text-muted-foreground mb-4">
                  {t('guide-parents.quickStart.step2.description')}
                </p>
                <div className="grid sm:grid-cols-2 gap-3 mb-4">
                  <Card className="p-4">
                    <div className="flex items-center gap-2 mb-1">
                      <Monitor className="w-4 h-4 text-muted-foreground" />
                      <span className="font-medium text-foreground text-sm">macOS</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('guide-parents.quickStart.step2.platforms.macos')}</p>
                  </Card>
                  <Card className="p-4">
                    <div className="flex items-center gap-2 mb-1">
                      <Monitor className="w-4 h-4 text-blue-600" />
                      <span className="font-medium text-foreground text-sm">Windows</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('guide-parents.quickStart.step2.platforms.windows')}</p>
                  </Card>
                  <Card className="p-4">
                    <div className="flex items-center gap-2 mb-1">
                      <Smartphone className="w-4 h-4 text-muted-foreground" />
                      <span className="font-medium text-foreground text-sm">iOS</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('guide-parents.quickStart.step2.platforms.ios')}</p>
                  </Card>
                  <Card className="p-4">
                    <div className="flex items-center gap-2 mb-1">
                      <Smartphone className="w-4 h-4 text-green-600" />
                      <span className="font-medium text-foreground text-sm">Android</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{t('guide-parents.quickStart.step2.platforms.android')}</p>
                  </Card>
                </div>
                <Link href="/install">
                  <Button variant="outline" size="sm">
                    <Download className="w-4 h-4 mr-2" />
                    {t('guide-parents.quickStart.step2.downloadButton')}
                  </Button>
                </Link>
              </div>
            </div>

            {/* Step 3: 如何使用 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-purple-900/50 rounded-full flex items-center justify-center">
                <Zap className="w-6 h-6 text-purple-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-foreground mb-2">
                  <span className="text-purple-600 mr-2">3.</span>
                  {t('guide-parents.quickStart.step3.title')}
                </h3>
                <p className="text-muted-foreground mb-4">
                  {t('guide-parents.quickStart.step3.description')}
                </p>
                <div className="space-y-2 text-sm text-muted-foreground">
                  <p>• {t('guide-parents.quickStart.step3.tips.mode')}</p>
                  <p>• {t('guide-parents.quickStart.step3.tips.feedback')}</p>
                  <p>• {t('guide-parents.quickStart.step3.tips.devices')}</p>
                </div>
              </div>
            </div>

            {/* Step 4: 如何获得帮助 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-orange-900/50 rounded-full flex items-center justify-center">
                <LifeBuoy className="w-6 h-6 text-orange-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-foreground mb-2">
                  <span className="text-orange-600 mr-2">4.</span>
                  {t('guide-parents.quickStart.step4.title')}
                </h3>
                <p className="text-muted-foreground mb-4">
                  {t('guide-parents.quickStart.step4.description')}
                </p>
                <div className="space-y-2 text-sm text-muted-foreground mb-4">
                  <p>• {t('guide-parents.quickStart.step4.tips.ai')}</p>
                  <p>• {t('guide-parents.quickStart.step4.tips.video')}</p>
                  <p>• {t('guide-parents.quickStart.step4.tips.screenshot')}</p>
                </div>
              </div>
            </div>

          </div>
        </div>
      </section>

      {/* FAQ Section */}
      <section id="faq" className="py-16 px-4 sm:px-6 lg:px-8 bg-card/50">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-foreground text-center mb-12">
            {t('guide-parents.faq.title')}
          </h2>
          <div className="space-y-4">
            {FAQ_KEYS.map((key) => (
              <FaqItem key={key} questionKey={key} t={t} />
            ))}
          </div>
        </div>
      </section>

      {/* Contact Section */}
      <section id="contact" className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <div className="text-center mb-12">
            <h2 className="text-3xl font-bold text-foreground mb-4">
              {t('guide-parents.contact.title')}
            </h2>
            <p className="text-muted-foreground text-lg">
              {t('guide-parents.contact.description')}
            </p>
          </div>
          <div className="grid sm:grid-cols-3 gap-6">
            <Card
              className="p-6 text-center cursor-pointer hover:shadow-lg transition-shadow"
              onClick={openChat}
            >
              <div className="w-12 h-12 mx-auto mb-4 bg-blue-900/50 rounded-full flex items-center justify-center">
                <MessageCircle className="w-6 h-6 text-blue-600" />
              </div>
              <h3 className="font-bold text-foreground mb-1">
                {t('guide-parents.contact.liveChatButton')}
              </h3>
              <p className="text-sm text-muted-foreground">
                {t('guide-parents.contact.liveChatDescription')}
              </p>
            </Card>

            <a href={`mailto:${email}`} className="block">
              <Card className="p-6 text-center hover:shadow-lg transition-shadow h-full">
                <div className="w-12 h-12 mx-auto mb-4 bg-red-900/50 rounded-full flex items-center justify-center">
                  <Mail className="w-6 h-6 text-red-600" />
                </div>
                <h3 className="font-bold text-foreground mb-1">
                  {t('guide-parents.contact.email.title')}
                </h3>
                <p className="text-sm text-muted-foreground">
                  {t('guide-parents.contact.email.description')}
                </p>
              </Card>
            </a>

            <a href={t('guide-parents.contact.whatsapp.link')} target="_blank" rel="noopener noreferrer" className="block">
              <Card className="p-6 text-center hover:shadow-lg transition-shadow h-full">
                <div className="w-12 h-12 mx-auto mb-4">
                  <Image src="/icons/whatsapp.svg" alt="WhatsApp" width={48} height={48} />
                </div>
                <h3 className="font-bold text-foreground mb-1">
                  {t('guide-parents.contact.whatsapp.title')}
                </h3>
                <p className="text-sm text-muted-foreground">
                  {t('guide-parents.contact.whatsapp.description')}
                </p>
              </Card>
            </a>
          </div>
        </div>
      </section>

      {/* Back to Home */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 text-center">
        <Link href="/">
          <Button size="lg" variant="outline">
            <Home className="w-5 h-5 mr-2" />
            {t('guide-parents.backToHome')}
          </Button>
        </Link>
      </section>

      <Footer />
    </div>
  );
}
