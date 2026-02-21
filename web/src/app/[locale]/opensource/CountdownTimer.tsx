"use client";

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { Clock, Heart, Github } from 'lucide-react';
import { Button } from '@/components/ui/button';

interface TimeRemaining {
  days: number;
  hours: number;
  minutes: number;
  seconds: number;
}

interface CountdownTimerProps {
  /** ISO date string for the open-source release target date */
  targetDateISO: string;
}

/**
 * Client island for the countdown timer on the opensource page.
 *
 * Uses useState + setInterval to tick every second.
 * Receives target date as a prop (serializable from server).
 * Renders days/hours/minutes/seconds or the released state.
 */
export default function CountdownTimer({ targetDateISO }: CountdownTimerProps) {
  const t = useTranslations();
  const [timeRemaining, setTimeRemaining] = useState<TimeRemaining | null>(null);

  useEffect(() => {
    const targetDate = new Date(targetDateISO);

    const calculateTimeRemaining = (): TimeRemaining => {
      const now = new Date();
      const diff = targetDate.getTime() - now.getTime();

      if (diff <= 0) {
        return { days: 0, hours: 0, minutes: 0, seconds: 0 };
      }

      const days = Math.floor(diff / (1000 * 60 * 60 * 24));
      const hours = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
      const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
      const seconds = Math.floor((diff % (1000 * 60)) / 1000);

      return { days, hours, minutes, seconds };
    };

    // Update immediately on mount
    setTimeRemaining(calculateTimeRemaining());

    // Update every second
    const interval = setInterval(() => {
      setTimeRemaining(calculateTimeRemaining());
    }, 1000);

    return () => clearInterval(interval);
  }, [targetDateISO]);

  if (!timeRemaining) {
    // Initial server-compatible render — show nothing until hydrated
    return (
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8 animate-pulse">
        {['days', 'hours', 'minutes', 'seconds'].map(unit => (
          <div key={unit} className="text-center p-6 bg-gray-100 dark:bg-gray-700 rounded-lg">
            <div className="h-12 bg-gray-200 dark:bg-gray-600 rounded mb-2" />
            <div className="h-4 bg-gray-200 dark:bg-gray-600 rounded w-1/2 mx-auto" />
          </div>
        ))}
      </div>
    );
  }

  const isOpenSourced =
    timeRemaining.days === 0 &&
    timeRemaining.hours === 0 &&
    timeRemaining.minutes === 0 &&
    timeRemaining.seconds === 0;

  if (isOpenSourced) {
    return (
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
    );
  }

  return (
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
  );
}
