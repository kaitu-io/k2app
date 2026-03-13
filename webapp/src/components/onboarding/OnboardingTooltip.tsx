import React, { useMemo, useRef, useState } from 'react';
import { Popper, Box, IconButton } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import { useTranslation } from 'react-i18next';
import type { TargetRect } from './useTargetRect';
import { ONBOARDING } from './tokens';

interface OnboardingTooltipProps {
  rect: TargetRect;
  placement: 'top' | 'bottom' | 'left' | 'right';
  i18nKey: string;
  currentIndex: number;
  totalSteps: number;
  onSkip: () => void;
  onNext: () => void;
}

// ── Arrow helpers ──

/** Absolute positioning for the arrow relative to the card */
function arrowPosition(p: string): React.CSSProperties {
  const { size, height } = ONBOARDING.arrow;
  switch (p) {
    case 'bottom': return { top: -height, left: '50%', marginLeft: -size / 2 };
    case 'top':    return { bottom: -height, left: '50%', marginLeft: -size / 2 };
    case 'left':   return { right: -height, top: '50%', marginTop: -size / 2 };
    case 'right':  return { left: -height, top: '50%', marginTop: -size / 2 };
    default:       return { top: -height, left: '50%', marginLeft: -size / 2 };
  }
}

function arrowKeyframes(p: string): string {
  const d = ONBOARDING.arrow.bounceDistance;
  switch (p) {
    case 'bottom': return `0%,100%{transform:rotate(0deg) translateY(0)} 50%{transform:rotate(0deg) translateY(-${d}px)}`;
    case 'top':    return `0%,100%{transform:rotate(180deg) translateY(0)} 50%{transform:rotate(180deg) translateY(${d}px)}`;
    case 'left':   return `0%,100%{transform:rotate(90deg) translateX(0)} 50%{transform:rotate(90deg) translateX(${d}px)}`;
    case 'right':  return `0%,100%{transform:rotate(-90deg) translateX(0)} 50%{transform:rotate(-90deg) translateX(-${d}px)}`;
    default:       return `0%,100%{transform:rotate(0deg) translateY(0)} 50%{transform:rotate(0deg) translateY(-${d}px)}`;
  }
}

// ── Component ──

const OnboardingTooltip: React.FC<OnboardingTooltipProps> = ({
  rect,
  placement,
  i18nKey,
  currentIndex,
  totalSteps,
  onSkip,
  onNext,
}) => {
  const { t } = useTranslation('onboarding');
  const isLast = currentIndex === totalSteps - 1;

  // Track Popper's actual placement (may differ from config after flip)
  const [actualPlacement, setActualPlacement] = useState(placement);

  // Store latest rect in ref so virtual element always reads current values
  const rectRef = useRef(rect);
  rectRef.current = rect;

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

  const modifiers = useMemo(
    () => [
      { name: 'offset', options: { offset: ONBOARDING.popperOffset } },
      {
        name: 'reportPlacement',
        enabled: true,
        phase: 'afterWrite' as const,
        fn: ({ state }: any) => {
          const p = state.placement.split('-')[0];
          setActualPlacement((prev: string) => (prev !== p ? p : prev));
        },
      },
    ],
    [],
  );

  const { size, height: arrowH, color: arrowColor, duration } = ONBOARDING.arrow;
  const kf = arrowKeyframes(actualPlacement);
  const animName = `ob-bounce-${actualPlacement}`;

  return (
    <Popper
      open
      anchorEl={virtualAnchor}
      placement={placement}
      modifiers={modifiers}
      style={{ zIndex: ONBOARDING.z.card }}
    >
      {/* Inject arrow keyframes */}
      <style>{`@keyframes ${animName}{${kf}}`}</style>

      <Box sx={{ position: 'relative' }}>
        {/* Animated arrow */}
        <Box
          component="span"
          sx={{
            position: 'absolute',
            ...arrowPosition(actualPlacement),
            zIndex: ONBOARDING.z.arrow,
            animation: `${animName} ${duration} ease-in-out infinite`,
            lineHeight: 0,
            pointerEvents: 'none',
          }}
        >
          <svg width={size} height={arrowH} viewBox={`0 0 ${size} ${arrowH}`}>
            <polygon
              points={`${size / 2},0 ${size},${arrowH} 0,${arrowH}`}
              fill={arrowColor}
            />
          </svg>
        </Box>

        {/* Card */}
        <Box
          sx={{
            background: ONBOARDING.card.bg,
            border: ONBOARDING.card.border,
            boxShadow: ONBOARDING.card.shadow,
            borderRadius: `${ONBOARDING.card.radius}px`,
            padding: ONBOARDING.card.padding,
            maxWidth: ONBOARDING.card.maxWidth,
          }}
        >
          {/* Title row */}
          <Box display="flex" alignItems="center" justifyContent="space-between" mb={1}>
            <Box
              component="span"
              sx={{
                fontSize: ONBOARDING.title.fontSize,
                fontWeight: ONBOARDING.title.fontWeight,
                color: ONBOARDING.title.color,
                flex: 1,
                minWidth: 0,
              }}
            >
              {t(`onboarding.${i18nKey}.title`)}
            </Box>
            <Box display="flex" alignItems="center" gap={0.5} flexShrink={0}>
              <IconButton
                size="small"
                onClick={onSkip}
                aria-label={t('onboarding.skip')}
                sx={{
                  color: 'rgba(255,255,255,0.4)',
                  p: '2px',
                  '&:hover': { color: 'rgba(255,255,255,0.7)' },
                }}
              >
                <CloseIcon sx={{ fontSize: 16 }} />
              </IconButton>
              <Box
                component="span"
                sx={{
                  fontSize: 11,
                  color: 'rgba(255,255,255,0.35)',
                  ml: 0.25,
                  whiteSpace: 'nowrap',
                }}
              >
                {currentIndex + 1}/{totalSteps}
              </Box>
            </Box>
          </Box>

          {/* Body */}
          <Box
            sx={{
              fontSize: ONBOARDING.body.fontSize,
              lineHeight: ONBOARDING.body.lineHeight,
              letterSpacing: ONBOARDING.body.letterSpacing,
              color: ONBOARDING.body.color,
              whiteSpace: 'pre-line',
              mb: 1.5,
            }}
          >
            {t(`onboarding.${i18nKey}.content`)}
          </Box>

          {/* Bottom bar */}
          <Box display="flex" alignItems="center" justifyContent="space-between">
            <Box
              component="span"
              sx={{
                fontSize: ONBOARDING.hint.fontSize,
                color: ONBOARDING.hint.color,
              }}
            >
              {t('onboarding.hint')}
            </Box>
            <Box
              component="button"
              onClick={onNext}
              sx={{
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                fontSize: ONBOARDING.nextButton.fontSize,
                fontWeight: ONBOARDING.nextButton.fontWeight,
                color: ONBOARDING.nextButton.color,
                p: 0,
                '&:hover': { opacity: 0.8 },
              }}
            >
              {isLast ? t('onboarding.done') : t('onboarding.next')} →
            </Box>
          </Box>
        </Box>
      </Box>
    </Popper>
  );
};

export default OnboardingTooltip;
