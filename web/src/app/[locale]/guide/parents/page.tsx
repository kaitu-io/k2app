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
  LogIn,
  CreditCard,
  Zap,
  Apple,
  Monitor,
  Home,
  MessageCircle,
  Mail,
} from 'lucide-react';

const openChat = () => {
  const w = window as unknown as Record<string, unknown>;
  if (w.$chatwoot && typeof (w.$chatwoot as Record<string, unknown>).toggle === 'function') {
    (w.$chatwoot as { toggle: (action: string) => void }).toggle('open');
  }
};

const email = ['bnb', '@', 'kaitu', '.io'].join('');

export default function GuideParentsPage() {
  const t = useTranslations();

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />

      {/* Hero Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto text-center">
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-gray-900 dark:text-white mb-4">
            {t('guide-parents.hero.title')}
          </h1>
          <p className="text-xl text-gray-600 dark:text-gray-300 mb-8">
            {t('guide-parents.hero.subtitle')}
          </p>
          <div className="flex flex-col sm:flex-row gap-4 justify-center">
            <Link href="/install">
              <Button size="lg">
                <Download className="w-5 h-5 mr-2" />
                {t('guide-parents.hero.downloadButton')}
              </Button>
            </Link>
            <a href="#guides">
              <Button size="lg" variant="outline">
                {t('guide-parents.hero.guideButton')}
              </Button>
            </a>
          </div>
        </div>
      </section>

      {/* Why Kaitu Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 bg-gradient-to-b from-blue-50 to-white dark:from-blue-900/20 dark:to-gray-800">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white text-center mb-12">
            {t('guide-parents.whyKaitu.title')}
          </h2>
          <div className="grid sm:grid-cols-3 gap-6">
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-green-100 dark:bg-green-900/50 rounded-full flex items-center justify-center">
                <Activity className="w-6 h-6 text-green-600" />
              </div>
              <h3 className="font-bold text-gray-900 dark:text-white mb-2">
                {t('guide-parents.whyKaitu.stable.title')}
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                {t('guide-parents.whyKaitu.stable.description')}
              </p>
            </Card>
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-blue-100 dark:bg-blue-900/50 rounded-full flex items-center justify-center">
                <MousePointerClick className="w-6 h-6 text-blue-600" />
              </div>
              <h3 className="font-bold text-gray-900 dark:text-white mb-2">
                {t('guide-parents.whyKaitu.simple.title')}
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                {t('guide-parents.whyKaitu.simple.description')}
              </p>
            </Card>
            <Card className="p-6 text-center">
              <div className="w-12 h-12 mx-auto mb-4 bg-purple-100 dark:bg-purple-900/50 rounded-full flex items-center justify-center">
                <GraduationCap className="w-6 h-6 text-purple-600" />
              </div>
              <h3 className="font-bold text-gray-900 dark:text-white mb-2">
                {t('guide-parents.whyKaitu.education.title')}
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                {t('guide-parents.whyKaitu.education.description')}
              </p>
            </Card>
          </div>
        </div>
      </section>

      {/* Quick Start Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white text-center mb-12">
            {t('guide-parents.quickStart.title')}
          </h2>
          <div className="space-y-8">
            {/* Step 1 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-blue-100 dark:bg-blue-900/50 rounded-full flex items-center justify-center">
                <Download className="w-6 h-6 text-blue-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
                  <span className="text-blue-600 mr-2">{"1."}</span>
                  {t('guide-parents.quickStart.step1.title')}
                </h3>
                <p className="text-gray-600 dark:text-gray-300 mb-4">
                  {t('guide-parents.quickStart.step1.description')}
                </p>
                <div className="flex flex-wrap gap-3">
                  <Link href="/install">
                    <Button variant="outline" size="sm">
                      {t('guide-parents.quickStart.step1.ios')}
                    </Button>
                  </Link>
                  <Link href="/install">
                    <Button variant="outline" size="sm">
                      {t('guide-parents.quickStart.step1.android')}
                    </Button>
                  </Link>
                  <Link href="/install">
                    <Button variant="outline" size="sm">
                      {t('guide-parents.quickStart.step1.macos')}
                    </Button>
                  </Link>
                  <Link href="/install">
                    <Button variant="outline" size="sm">
                      {t('guide-parents.quickStart.step1.windows')}
                    </Button>
                  </Link>
                </div>
              </div>
            </div>

            {/* Step 2 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-green-100 dark:bg-green-900/50 rounded-full flex items-center justify-center">
                <LogIn className="w-6 h-6 text-green-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
                  <span className="text-green-600 mr-2">{"2."}</span>
                  {t('guide-parents.quickStart.step2.title')}
                </h3>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('guide-parents.quickStart.step2.description')}
                </p>
              </div>
            </div>

            {/* Step 3 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-purple-100 dark:bg-purple-900/50 rounded-full flex items-center justify-center">
                <CreditCard className="w-6 h-6 text-purple-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
                  <span className="text-purple-600 mr-2">{"3."}</span>
                  {t('guide-parents.quickStart.step3.title')}
                </h3>
                <p className="text-gray-600 dark:text-gray-300 mb-4">
                  {t('guide-parents.quickStart.step3.description')}
                </p>
                <Link href="/purchase">
                  <Button variant="outline" size="sm">
                    {t('guide-parents.quickStart.step3.button')}
                  </Button>
                </Link>
              </div>
            </div>

            {/* Step 4 */}
            <div className="flex gap-6">
              <div className="flex-shrink-0 w-12 h-12 bg-orange-100 dark:bg-orange-900/50 rounded-full flex items-center justify-center">
                <Zap className="w-6 h-6 text-orange-600" />
              </div>
              <div className="flex-1">
                <h3 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
                  <span className="text-orange-600 mr-2">{"4."}</span>
                  {t('guide-parents.quickStart.step4.title')}
                </h3>
                <p className="text-gray-600 dark:text-gray-300">
                  {t('guide-parents.quickStart.step4.description')}
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Guides Section */}
      <section id="guides" className="py-16 px-4 sm:px-6 lg:px-8 bg-gradient-to-b from-gray-50 to-white dark:from-gray-800 dark:to-gray-900">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white text-center mb-12">
            {t('guide-parents.guides.title')}
          </h2>
          <div className="grid sm:grid-cols-2 gap-6">
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <div className="flex items-center mb-4">
                <div className="w-12 h-12 bg-gray-100 dark:bg-gray-700 rounded-lg flex items-center justify-center mr-4">
                  <Apple className="w-6 h-6 text-gray-700 dark:text-gray-300" />
                </div>
                <div>
                  <h3 className="font-bold text-gray-900 dark:text-white">
                    {t('guide-parents.guides.mac.title')}
                  </h3>
                  <p className="text-sm text-gray-600 dark:text-gray-300">
                    {t('guide-parents.guides.mac.description')}
                  </p>
                </div>
              </div>
              <a href="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/mac-guide.pdf" target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm">
                  <Download className="w-4 h-4 mr-2" />
                  {t('guide-parents.guides.download')}
                </Button>
              </a>
            </Card>
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <div className="flex items-center mb-4">
                <div className="w-12 h-12 bg-blue-100 dark:bg-blue-900/50 rounded-lg flex items-center justify-center mr-4">
                  <Monitor className="w-6 h-6 text-blue-600" />
                </div>
                <div>
                  <h3 className="font-bold text-gray-900 dark:text-white">
                    {t('guide-parents.guides.windows.title')}
                  </h3>
                  <p className="text-sm text-gray-600 dark:text-gray-300">
                    {t('guide-parents.guides.windows.description')}
                  </p>
                </div>
              </div>
              <a href="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/win-guide.pdf" target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm">
                  <Download className="w-4 h-4 mr-2" />
                  {t('guide-parents.guides.download')}
                </Button>
              </a>
            </Card>
          </div>
        </div>
      </section>

      {/* FAQ Section */}
      <section id="faq" className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white text-center mb-12">
            {t('guide-parents.faq.title')}
          </h2>
          <div className="space-y-6">
            {(['multiDevice', 'connectionFailed', 'purchase', 'platforms', 'childSafety'] as const).map((key) => (
              <Card key={key} className="p-6">
                <h3 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t(`guide-parents.faq.items.${key}.question`)}
                </h3>
                <p className="text-gray-600 dark:text-gray-300 text-sm">
                  {t(`guide-parents.faq.items.${key}.answer`)}
                </p>
              </Card>
            ))}
          </div>
        </div>
      </section>

      {/* Contact Section */}
      <section id="contact" className="py-16 px-4 sm:px-6 lg:px-8 bg-gradient-to-b from-gray-50 to-white dark:from-gray-800 dark:to-gray-900">
        <div className="max-w-4xl mx-auto">
          <div className="text-center mb-12">
            <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-4">
              {t('guide-parents.contact.title')}
            </h2>
            <p className="text-gray-600 dark:text-gray-300 text-lg">
              {t('guide-parents.contact.description')}
            </p>
          </div>
          <div className="grid sm:grid-cols-3 gap-6">
            {/* Live Chat */}
            <Card
              className="p-6 text-center cursor-pointer hover:shadow-lg transition-shadow"
              onClick={openChat}
            >
              <div className="w-12 h-12 mx-auto mb-4 bg-blue-100 dark:bg-blue-900/50 rounded-full flex items-center justify-center">
                <MessageCircle className="w-6 h-6 text-blue-600" />
              </div>
              <h3 className="font-bold text-gray-900 dark:text-white mb-1">
                {t('guide-parents.contact.liveChatButton')}
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                {t('guide-parents.contact.liveChatDescription')}
              </p>
            </Card>

            {/* Email */}
            <a href={`mailto:${email}`} className="block">
              <Card className="p-6 text-center hover:shadow-lg transition-shadow h-full">
                <div className="w-12 h-12 mx-auto mb-4 bg-red-100 dark:bg-red-900/50 rounded-full flex items-center justify-center">
                  <Mail className="w-6 h-6 text-red-600" />
                </div>
                <h3 className="font-bold text-gray-900 dark:text-white mb-1">
                  {t('guide-parents.contact.email.title')}
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('guide-parents.contact.email.description')}
                </p>
              </Card>
            </a>

            {/* WhatsApp */}
            <a href={t('guide-parents.contact.whatsapp.link')} target="_blank" rel="noopener noreferrer" className="block">
              <Card className="p-6 text-center hover:shadow-lg transition-shadow h-full">
                <div className="w-12 h-12 mx-auto mb-4">
                  <Image src="/icons/whatsapp.svg" alt="WhatsApp" width={48} height={48} />
                </div>
                <h3 className="font-bold text-gray-900 dark:text-white mb-1">
                  {t('guide-parents.contact.whatsapp.title')}
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-300">
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
