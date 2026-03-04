/**
 * OnboardingGuide — react-joyride 引导组件
 *
 * 在 Layout 层渲染，根据当前 phase + route 决定显示哪些 steps。
 * Controlled mode: 由 onboarding store 驱动 stepIndex。
 *
 * 所有 phase 都是交互式：用户点击实际目标元素推进。
 * 导航类目标（FAB、导航项）的点击被拦截，防止离开当前页面。
 * 视觉: 青色发光边框 + 脉冲动画 + 弹跳箭头。
 */

import { useCallback, useEffect, useMemo } from 'react';
import Joyride, { ACTIONS, STATUS } from 'react-joyride';
import type { CallBackProps, Step, Styles, TooltipRenderProps } from 'react-joyride';
import { useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Box, Paper, Typography, useTheme } from '@mui/material';
import { keyframes } from '@mui/system';
import { useOnboardingStore } from '../stores/onboarding.store';

/** Bouncing arrow animation */
const bounce = keyframes`
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-6px); }
`;

/** Inject global keyframes for spotlight pulse (react-joyride uses inline styles) */
const PULSE_STYLE_ID = 'onboarding-pulse-style';
function ensurePulseStyle() {
  if (document.getElementById(PULSE_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = PULSE_STYLE_ID;
  style.textContent = `
    @keyframes onboarding-pulse {
      0%, 100% { box-shadow: 0 0 0 3px rgba(0,212,255,0.8), 0 0 20px rgba(0,212,255,0.4); }
      50% { box-shadow: 0 0 0 6px rgba(0,212,255,0.6), 0 0 30px rgba(0,212,255,0.3), 0 0 60px rgba(0,212,255,0.1); }
    }
  `;
  document.head.appendChild(style);
}

/** Arrow emoji based on tooltip placement relative to target */
function getArrowEmoji(placement: string | undefined): string {
  switch (placement) {
    case 'top':
    case 'top-start':
    case 'top-end':
      return '👇';
    case 'left':
    case 'left-start':
    case 'left-end':
      return '👉';
    case 'right':
    case 'right-start':
    case 'right-end':
      return '👈';
    default:
      return '👆';
  }
}

interface PhaseConfig {
  target: string;
  placement: Step['placement'];
  route: string;
  /** Whether to intercept click and prevent navigation */
  preventClick: boolean;
  /** Target element uses position:fixed */
  isFixed?: boolean;
  /** Delay (ms) before advancing, for animated targets */
  advanceDelay?: number;
}

/** Map phase → target + placement. All phases on route '/', all interactive. */
const PHASE_CONFIG: Record<number, PhaseConfig> = {
  1: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', route: '/', preventClick: false, advanceDelay: 350 },
  2: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', route: '/', preventClick: false },
  3: { target: '[data-tour="feedback-button"]', placement: 'left', route: '/', preventClick: true, isFixed: true },
  4: { target: '[data-tour="nav-invite"]', placement: 'right', route: '/', preventClick: true },
  5: { target: '[data-tour="nav-purchase"]', placement: 'right', route: '/', preventClick: true },
};

/** Compact interactive tooltip — bouncing arrow + skip link */
function OnboardingTooltip({ step, skipProps, tooltipProps }: TooltipRenderProps) {
  const { t } = useTranslation('onboarding');
  const arrow = getArrowEmoji(step.placement);

  return (
    <Paper
      {...tooltipProps}
      sx={{
        p: '12px 16px',
        maxWidth: 260,
        borderRadius: 3,
        textAlign: 'center',
      }}
    >
      {step.title && (
        <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 0.5 }}>
          {step.title}
        </Typography>
      )}
      <Typography variant="body2" sx={{ lineHeight: 1.6, whiteSpace: 'pre-line' }}>
        {step.content}
      </Typography>

      <Box sx={{ mt: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 0.5 }}>
        <Box sx={{ fontSize: '1.2rem', animation: `${bounce} 1s infinite` }}>
          {arrow}
        </Box>
        <Typography
          component="button"
          variant="caption"
          {...skipProps}
          sx={{
            color: 'text.disabled',
            cursor: 'pointer',
            background: 'none',
            border: 'none',
            p: 0,
            fontSize: '0.75rem',
            '&:hover': { color: 'text.secondary' },
          }}
        >
          {t('onboarding.skip')}
        </Typography>
      </Box>
    </Paper>
  );
}

export function OnboardingGuide() {
  const theme = useTheme();
  const location = useLocation();
  const { active, phase, nextPhase, complete } = useOnboardingStore();

  const currentConfig = PHASE_CONFIG[phase];
  const isOnCorrectRoute = currentConfig && location.pathname === currentConfig.route;
  const shouldRun = active && isOnCorrectRoute;

  // Inject global pulse animation
  useEffect(() => {
    if (shouldRun) ensurePulseStyle();
  }, [shouldRun]);

  // Click listener on target element — advances phase, optionally prevents navigation
  useEffect(() => {
    if (!shouldRun || !currentConfig) return;
    const el = document.querySelector(currentConfig.target);
    if (!el) return;

    const handler = (e: Event) => {
      if (currentConfig.preventClick) {
        e.preventDefault();
        e.stopPropagation();
      }
      if (currentConfig.advanceDelay) {
        setTimeout(nextPhase, currentConfig.advanceDelay);
      } else {
        nextPhase();
      }
    };

    el.addEventListener('click', handler, { capture: true, once: true });
    return () => el.removeEventListener('click', handler, { capture: true } as EventListenerOptions);
  }, [shouldRun, phase, currentConfig, nextPhase]);

  const { t } = useTranslation('onboarding');
  const phaseKey = `phase${phase}` as const;

  const steps: Step[] = useMemo(() => {
    if (!currentConfig) return [];

    return [{
      target: currentConfig.target,
      title: t(`onboarding.${phaseKey}.title`),
      content: t(`onboarding.${phaseKey}.content`),
      placement: currentConfig.placement,
      disableBeacon: true,
      disableOverlayClose: true,
      spotlightClicks: true,
      hideFooter: true,
      showSkipButton: false,
      isFixed: currentConfig.isFixed ?? false,
      locale: {
        next: t('onboarding.next'),
        skip: t('onboarding.skip'),
        back: t('onboarding.back'),
        close: t('onboarding.close'),
        last: t('onboarding.last'),
      },
    }];
  }, [currentConfig, phase, phaseKey, t]);

  const joyrideStyles: Partial<Styles> = useMemo(() => ({
    options: {
      backgroundColor: theme.palette.background.paper,
      textColor: theme.palette.text.primary,
      primaryColor: theme.palette.primary.main,
      arrowColor: theme.palette.background.paper,
      overlayColor: 'rgba(0, 0, 0, 0.7)',
      zIndex: theme.zIndex.modal + 10,
    },
    spotlight: {
      borderRadius: 12,
      boxShadow: '0 0 0 3px rgba(0, 212, 255, 0.8), 0 0 20px rgba(0, 212, 255, 0.4), 0 0 40px rgba(0, 212, 255, 0.2)',
      animation: 'onboarding-pulse 2s ease-in-out infinite',
    },
  }), [theme]);

  const handleCallback = useCallback((data: CallBackProps) => {
    const { action, status } = data;

    if (status === STATUS.FINISHED || status === STATUS.SKIPPED) {
      complete();
      return;
    }

    if (action === ACTIONS.SKIP) {
      complete();
      return;
    }
  }, [complete]);

  if (!shouldRun || steps.length === 0) {
    return null;
  }

  return (
    <Joyride
      key={phase}
      steps={steps}
      run={shouldRun}
      stepIndex={0}
      continuous={false}
      callback={handleCallback}
      scrollToFirstStep={false}
      disableScrolling
      showProgress={false}
      spotlightPadding={16}
      styles={joyrideStyles}
      tooltipComponent={OnboardingTooltip}
    />
  );
}
