import React, { useMemo, useRef } from 'react';
import { Popper, Paper, Typography, Box } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { TargetRect } from './useTargetRect';

interface OnboardingTooltipProps {
  rect: TargetRect;
  placement: 'top' | 'bottom' | 'left' | 'right';
  i18nKey: string;
  currentIndex: number;
  totalSteps: number;
  onSkip: () => void;
}

/**
 * Tooltip anchored to target via virtual element.
 * Virtual element's getBoundingClientRect() reads from rectRef — always fresh values.
 * useMemo keeps stable reference so Popper doesn't recreate instance on re-render.
 *
 * Update chain:
 *   RAF tick → setRect() → re-render → MUI forceUpdate() → virtual getBoundingClientRect()
 *   → reads rectRef.current → Popper repositions. 100% deterministic.
 */
const OnboardingTooltip: React.FC<OnboardingTooltipProps> = ({
  rect,
  placement,
  i18nKey,
  currentIndex,
  totalSteps,
  onSkip,
}) => {
  const { t } = useTranslation('onboarding');

  // Store latest rect in ref so virtual element always reads current values
  const rectRef = useRef(rect);
  rectRef.current = rect;

  // Virtual element — Popper calls getBoundingClientRect() on each forceUpdate
  const virtualAnchor = useMemo(
    () => ({
      getBoundingClientRect: () => ({
        top: rectRef.current.top,
        left: rectRef.current.left,
        bottom: rectRef.current.top + rectRef.current.height,
        right: rectRef.current.left + rectRef.current.width,
        width: rectRef.current.width,
        height: rectRef.current.height,
        x: rectRef.current.left,
        y: rectRef.current.top,
        toJSON: () => {},
      }),
    }),
    [],
  );

  return (
    <Popper
      open
      anchorEl={virtualAnchor}
      placement={placement}
      modifiers={[{ name: 'offset', options: { offset: [0, 12] } }]}
      style={{ zIndex: 1320 }}
    >
      <Paper sx={{ p: '14px 18px', maxWidth: 280, borderRadius: 2 }}>
        <Typography variant="subtitle2" fontWeight={600} mb={0.5}>
          {t(`onboarding.${i18nKey}.title`)}
        </Typography>
        <Typography
          variant="body2"
          color="text.secondary"
          lineHeight={1.6}
          whiteSpace="pre-line"
        >
          {t(`onboarding.${i18nKey}.content`)}
        </Typography>
        <Box mt={1.5} display="flex" alignItems="center" justifyContent="space-between">
          <Typography
            component="button"
            variant="caption"
            onClick={onSkip}
            sx={{
              color: 'text.disabled',
              cursor: 'pointer',
              background: 'none',
              border: 'none',
              p: 0,
            }}
          >
            {t('onboarding.skip')}
          </Typography>
          <Typography variant="caption" color="text.disabled" fontSize="0.7rem">
            {currentIndex + 1}/{totalSteps}
          </Typography>
        </Box>
      </Paper>
    </Popper>
  );
};

export default OnboardingTooltip;
