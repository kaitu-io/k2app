import { Box, LinearProgress, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

export interface PasswordStrengthMeterProps {
  /** zxcvbn 0-4 score. */
  score: 0 | 1 | 2 | 3 | 4;
  /** When true, the bar is rendered as very-weak regardless of score. */
  tooShort: boolean;
  /** Suppress rendering entirely (e.g., field is empty). */
  hidden?: boolean;
}

// Visual palette aligned with the MUI severity tokens — dark theme only.
const SCORE_COLOR: Record<0 | 1 | 2 | 3 | 4, 'error' | 'warning' | 'info' | 'success'> = {
  0: 'error',
  1: 'error',
  2: 'warning',
  3: 'info',
  4: 'success',
};

const SCORE_KEY: Record<0 | 1 | 2 | 3 | 4, string> = {
  0: 'account:password.strength.veryWeak',
  1: 'account:password.strength.weak',
  2: 'account:password.strength.fair',
  3: 'account:password.strength.good',
  4: 'account:password.strength.strong',
};

export default function PasswordStrengthMeter({ score, tooShort, hidden }: PasswordStrengthMeterProps) {
  const { t } = useTranslation();
  if (hidden) return null;
  // When too short, render as very-weak — never leave the user wondering
  // what the meter would say if the rest were fine.
  const effectiveScore: 0 | 1 | 2 | 3 | 4 = tooShort ? 0 : score;
  const color = SCORE_COLOR[effectiveScore];
  // Display (score+1)/5 to ensure even score=0 shows a sliver — empty bar
  // looks like a rendering bug.
  const value = ((effectiveScore + 1) / 5) * 100;

  return (
    <Box sx={{ mt: 0.5 }} aria-live="polite">
      <LinearProgress
        variant="determinate"
        value={value}
        color={color}
        role="progressbar"
        aria-valuenow={effectiveScore}
        aria-valuemin={0}
        aria-valuemax={4}
        aria-label={t('account:password.strength.label')}
        sx={{ borderRadius: 1, height: 6 }}
      />
      <Typography variant="caption" color={`${color}.main`} sx={{ display: 'block', mt: 0.5 }}>
        {t('account:password.strength.label')}: {t(SCORE_KEY[effectiveScore])}
      </Typography>
    </Box>
  );
}
