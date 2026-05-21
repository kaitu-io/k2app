'use client';

import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';

export interface PasswordStrengthMeterProps {
  /** zxcvbn score 0-4 (4 = strongest) */
  score: 0 | 1 | 2 | 3 | 4;
  /** When true, effective score is forced to 0 regardless of `score`. */
  tooShort: boolean;
  /** When true, render nothing (caller controls visibility). */
  hidden?: boolean;
}

const BAR_COLORS: Record<0 | 1 | 2 | 3 | 4, string> = {
  0: 'bg-red-500',
  1: 'bg-red-500',
  2: 'bg-amber-500',
  3: 'bg-blue-500',
  4: 'bg-green-500',
};

const TEXT_COLORS: Record<0 | 1 | 2 | 3 | 4, string> = {
  0: 'text-red-500',
  1: 'text-red-500',
  2: 'text-amber-500',
  3: 'text-blue-500',
  4: 'text-green-500',
};

const SCORE_KEY: Record<0 | 1 | 2 | 3 | 4, string> = {
  0: 'password.strength.veryWeak',
  1: 'password.strength.weak',
  2: 'password.strength.fair',
  3: 'password.strength.good',
  4: 'password.strength.strong',
};

/**
 * Password strength meter — shadcn-styled bar + label.
 * Reads zxcvbn 0-4 score (clamped to 0 when `tooShort`).
 * i18n keys live under `admin.account.password.strength.*`.
 */
export default function PasswordStrengthMeter({
  score,
  tooShort,
  hidden,
}: PasswordStrengthMeterProps) {
  const t = useTranslations('admin.account');
  if (hidden) return null;
  const effective: 0 | 1 | 2 | 3 | 4 = tooShort ? 0 : score;
  const widthPct = ((effective + 1) / 5) * 100;

  return (
    <div className="mt-1" aria-live="polite">
      <div
        role="progressbar"
        aria-valuenow={effective}
        aria-valuemin={0}
        aria-valuemax={4}
        aria-label={t('password.strength.label')}
        className="h-1.5 w-full overflow-hidden rounded bg-muted"
      >
        <div
          className={cn('h-full transition-all', BAR_COLORS[effective])}
          style={{ width: `${widthPct}%` }}
        />
      </div>
      <p className={cn('mt-1 text-xs', TEXT_COLORS[effective])}>
        {t('password.strength.label')}: {t(SCORE_KEY[effective])}
      </p>
    </div>
  );
}
