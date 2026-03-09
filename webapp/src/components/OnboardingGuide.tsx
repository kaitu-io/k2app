/**
 * OnboardingGuide — react-joyride 引导组件
 *
 * 在 Layout 层渲染，根据当前 phase + route 决定显示哪些 steps。
 * Controlled mode: 由 onboarding store 驱动 stepIndex。
 *
 * 使用 tooltip 内的「下一步」按钮推进，不依赖点击目标元素。
 * 部分 phase 需要导航到其他页面（如 /invite），由 navigateTo 字段控制。
 * 视觉: 青色发光边框 + 脉冲动画。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import Joyride, { ACTIONS, STATUS } from 'react-joyride';
import type { CallBackProps, Step, Styles, TooltipRenderProps } from 'react-joyride';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Box, Button, Paper, Typography, useTheme } from '@mui/material';
import { useOnboardingStore } from '../stores/onboarding.store';

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

interface PhaseConfig {
  target: string;
  placement: Step['placement'];
  route: string;
  /** Target element uses position:fixed */
  isFixed?: boolean;
  /** Navigate to this path when "Next" is clicked (before advancing phase) */
  navigateTo?: string;
}

/** Map phase → target + placement */
const PHASE_CONFIG: Record<number, PhaseConfig> = {
  1: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', route: '/' },
  3: { target: '[data-tour="feedback-button"]', placement: 'left', route: '/', isFixed: true },
  4: { target: '[data-tour="nav-invite"]', placement: 'top', route: '/', navigateTo: '/invite' },
  5: { target: '[data-tour="invite-share"]', placement: 'top', route: '/invite', navigateTo: '/' },
  6: { target: '[data-tour="nav-purchase"]', placement: 'top', route: '/' },
};

/** Professional tooltip with Next button + step indicator */
function OnboardingTooltip({ step, tooltipProps }: TooltipRenderProps) {
  const { t } = useTranslation('onboarding');
  const { phase, phases, nextPhase, complete } = useOnboardingStore();
  const navigate = useNavigate();
  const currentIndex = phases.indexOf(phase);
  const totalSteps = phases.length;
  const isLast = currentIndex === totalSteps - 1;
  const currentConfig = PHASE_CONFIG[phase];

  const handleNext = () => {
    if (currentConfig?.navigateTo) {
      navigate(currentConfig.navigateTo);
    }
    nextPhase();
  };

  return (
    <Paper
      {...tooltipProps}
      sx={{
        p: '14px 18px',
        maxWidth: 280,
        borderRadius: 2,
      }}
    >
      {step.title && (
        <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 0.5 }}>
          {step.title}
        </Typography>
      )}
      <Typography variant="body2" sx={{ lineHeight: 1.6, whiteSpace: 'pre-line', color: 'text.secondary' }}>
        {step.content}
      </Typography>

      <Box sx={{ mt: 1.5, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography
          component="button"
          variant="caption"
          onClick={() => complete()}
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

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="caption" sx={{ color: 'text.disabled', fontSize: '0.7rem' }}>
            {currentIndex + 1}/{totalSteps}
          </Typography>
          <Button
            size="small"
            variant="contained"
            onClick={handleNext}
            sx={{ minWidth: 64, textTransform: 'none', fontSize: '0.8rem' }}
          >
            {isLast ? t('onboarding.last') : t('onboarding.next')}
          </Button>
        </Box>
      </Box>
    </Paper>
  );
}

export function OnboardingGuide() {
  const theme = useTheme();
  const location = useLocation();
  const { active, phase, complete } = useOnboardingStore();

  const currentConfig = PHASE_CONFIG[phase];
  const isOnCorrectRoute = currentConfig && location.pathname === currentConfig.route;
  const shouldRun = active && isOnCorrectRoute;

  // Wait for target element to exist (handles lazy-loaded pages like InviteHub)
  const [targetReady, setTargetReady] = useState(false);
  useEffect(() => {
    if (!shouldRun || !currentConfig) {
      setTargetReady(false);
      return;
    }
    // Check immediately
    if (document.querySelector(currentConfig.target)) {
      setTargetReady(true);
      return;
    }
    // Poll until target appears (e.g. after lazy load + data fetch)
    const interval = setInterval(() => {
      if (document.querySelector(currentConfig.target)) {
        setTargetReady(true);
        clearInterval(interval);
      }
    }, 200);
    return () => clearInterval(interval);
  }, [shouldRun, currentConfig]);

  // Inject global pulse animation
  useEffect(() => {
    if (shouldRun && targetReady) ensurePulseStyle();
  }, [shouldRun, targetReady]);

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
      spotlightClicks: false,
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

  if (!shouldRun || !targetReady || steps.length === 0) {
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
