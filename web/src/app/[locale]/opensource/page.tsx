"use client";

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Card } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Github, Calendar, Clock, Heart } from 'lucide-react';
import Header from '@/components/Header';
import Footer from '@/components/Footer';

const OPENSOURCE_DATE = new Date('2026-06-04T00:00:00Z'); // June 4, 2026

interface TimeRemaining {
  days: number;
  hours: number;
  minutes: number;
  seconds: number;
}

export default function OpenSourcePage() {
  const t = useTranslations();
  const [timeRemaining, setTimeRemaining] = useState<TimeRemaining | null>(null);
  const [isClient, setIsClient] = useState(false);

  useEffect(() => {
    setIsClient(true);

    const calculateTimeRemaining = (): TimeRemaining => {
      const now = new Date();
      const diff = OPENSOURCE_DATE.getTime() - now.getTime();

      if (diff <= 0) {
        return { days: 0, hours: 0, minutes: 0, seconds: 0 };
      }

      const days = Math.floor(diff / (1000 * 60 * 60 * 24));
      const hours = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
      const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
      const seconds = Math.floor((diff % (1000 * 60)) / 1000);

      return { days, hours, minutes, seconds };
    };

    // Update immediately
    setTimeRemaining(calculateTimeRemaining());

    // Update every second
    const interval = setInterval(() => {
      setTimeRemaining(calculateTimeRemaining());
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  if (!isClient || !timeRemaining) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-blue-50 via-white to-purple-50 dark:from-gray-900 dark:via-gray-800 dark:to-gray-900">
        <Header />
        <div className="max-w-4xl mx-auto px-4 py-20 text-center">
          <div className="animate-pulse">
            <div className="h-12 bg-gray-200 dark:bg-gray-700 rounded mb-4"></div>
            <div className="h-8 bg-gray-200 dark:bg-gray-700 rounded"></div>
          </div>
        </div>
        <Footer />
      </div>
    );
  }

  const isOpenSourced = timeRemaining.days === 0 && timeRemaining.hours === 0 &&
                        timeRemaining.minutes === 0 && timeRemaining.seconds === 0;

  return (
    <div className="min-h-screen bg-gradient-to-br from-blue-50 via-white to-purple-50 dark:from-gray-900 dark:via-gray-800 dark:to-gray-900">
      <Header />

      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        {/* Hero Section */}
        <div className="text-center mb-12">
          <div className="inline-flex items-center justify-center p-4 bg-gradient-to-br from-green-100 to-blue-100 dark:from-green-900/30 dark:to-blue-900/30 rounded-full mb-6">
            <Github className="w-12 h-12 text-green-600 dark:text-green-400" />
          </div>

          <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 dark:text-white mb-4">
            {t('theme.opensource.title')}
          </h1>

          <p className="text-xl text-gray-600 dark:text-gray-300 mb-2">
            {t('theme.opensource.subtitle')}
          </p>

          <p className="text-lg text-gray-500 dark:text-gray-400">
            {t('theme.opensource.description')}
          </p>
        </div>

        {/* Countdown Card */}
        <Card className="p-8 mb-8 border-2 border-blue-200 dark:border-blue-800 bg-white dark:bg-gray-800 shadow-2xl">
          <div className="text-center mb-8">
            <div className="flex items-center justify-center mb-4">
              <Calendar className="w-6 h-6 text-blue-600 dark:text-blue-400 mr-2" />
              <h2 className="text-2xl font-bold text-gray-900 dark:text-white">
                {t('theme.opensource.targetDate')}
              </h2>
            </div>
            <p className="text-3xl font-bold text-blue-600 dark:text-blue-400">
              {t('theme.opensource.targetDateValue')}
            </p>
          </div>

          {isOpenSourced ? (
            <div className="text-center py-12">
              <Heart className="w-20 h-20 text-red-500 mx-auto mb-6 animate-pulse" />
              <h2 className="text-4xl font-bold text-gray-900 dark:text-white mb-4">
                {t('theme.opensource.released')}
              </h2>
              <p className="text-xl text-gray-600 dark:text-gray-300 mb-8">
                {t('theme.opensource.releasedDesc')}
              </p>
              <Button size="lg" className="bg-green-600 hover:bg-green-700">
                <Github className="w-5 h-5 mr-2" />
                {t('theme.opensource.viewOnGithub')}
              </Button>
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
                <div className="text-center p-6 bg-gradient-to-br from-blue-50 to-purple-50 dark:from-blue-900/20 dark:to-purple-900/20 rounded-lg">
                  <div className="text-5xl font-bold text-blue-600 dark:text-blue-400 mb-2">
                    {timeRemaining.days}
                  </div>
                  <div className="text-sm text-gray-600 dark:text-gray-400 uppercase tracking-wide">
                    {t('theme.opensource.days')}
                  </div>
                </div>

                <div className="text-center p-6 bg-gradient-to-br from-purple-50 to-pink-50 dark:from-purple-900/20 dark:to-pink-900/20 rounded-lg">
                  <div className="text-5xl font-bold text-purple-600 dark:text-purple-400 mb-2">
                    {timeRemaining.hours}
                  </div>
                  <div className="text-sm text-gray-600 dark:text-gray-400 uppercase tracking-wide">
                    {t('theme.opensource.hours')}
                  </div>
                </div>

                <div className="text-center p-6 bg-gradient-to-br from-pink-50 to-orange-50 dark:from-pink-900/20 dark:to-orange-900/20 rounded-lg">
                  <div className="text-5xl font-bold text-pink-600 dark:text-pink-400 mb-2">
                    {timeRemaining.minutes}
                  </div>
                  <div className="text-sm text-gray-600 dark:text-gray-400 uppercase tracking-wide">
                    {t('theme.opensource.minutes')}
                  </div>
                </div>

                <div className="text-center p-6 bg-gradient-to-br from-orange-50 to-yellow-50 dark:from-orange-900/20 dark:to-yellow-900/20 rounded-lg">
                  <div className="text-5xl font-bold text-orange-600 dark:text-orange-400 mb-2">
                    {timeRemaining.seconds}
                  </div>
                  <div className="text-sm text-gray-600 dark:text-gray-400 uppercase tracking-wide">
                    {t('theme.opensource.seconds')}
                  </div>
                </div>
              </div>

              <div className="text-center">
                <div className="inline-flex items-center text-gray-600 dark:text-gray-400">
                  <Clock className="w-5 h-5 mr-2" />
                  <span className="text-lg font-medium">
                    {t('theme.opensource.countingDown')}
                  </span>
                </div>
              </div>
            </>
          )}
        </Card>

        {/* Why Open Source */}
        <Card className="p-8 bg-gradient-to-r from-green-50 to-blue-50 dark:from-green-900/20 dark:to-blue-900/20 border-2 border-green-200 dark:border-green-800">
          <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-6 text-center">
            {t('theme.opensource.whyTitle')}
          </h3>

          <div className="grid md:grid-cols-2 gap-6">
            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-green-600 rounded-full flex items-center justify-center text-white font-bold">
                {"1"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason1Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason1Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-blue-600 rounded-full flex items-center justify-center text-white font-bold">
                {"2"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason2Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason2Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-purple-600 rounded-full flex items-center justify-center text-white font-bold">
                {"3"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason3Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason3Desc')}
                </p>
              </div>
            </div>

            <div className="flex items-start space-x-3">
              <div className="flex-shrink-0 w-8 h-8 bg-pink-600 rounded-full flex items-center justify-center text-white font-bold">
                {"4"}
              </div>
              <div>
                <h4 className="font-semibold text-gray-900 dark:text-white mb-2">
                  {t('theme.opensource.reason4Title')}
                </h4>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  {t('theme.opensource.reason4Desc')}
                </p>
              </div>
            </div>
          </div>
        </Card>
      </div>

      <Footer />
    </div>
  );
}
