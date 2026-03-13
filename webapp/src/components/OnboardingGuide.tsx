/**
 * OnboardingGuide — 交互式引导组件
 *
 * 在 Layout 层渲染。通过 selector 追踪目标 DOM，完全解耦业务逻辑。
 * 用户点击目标元素推进引导（无 Next 按钮）。
 *
 * 架构:
 *   SpotlightOverlay (position:fixed SVG) — 暗色遮罩 + evenodd 挖洞
 *   OnboardingTooltip (MUI Popper + virtual element) — 提示气泡
 *   useTargetRect (RAF polling) — 每帧追踪目标 viewport 坐标
 *
 * position:fixed 元素不受 CSS body zoom 影响，彻底解决 Windows 缩放错位问题。
 */

import { useEffect, useRef, useState } from 'react';
import { useOnboardingStore } from '../stores/onboarding.store';
import { getPhaseConfig } from '../stores/onboarding.store';
import { useTargetRect } from './onboarding/useTargetRect';
import SpotlightOverlay from './onboarding/SpotlightOverlay';
import OnboardingTooltip from './onboarding/OnboardingTooltip';

const STYLE_ID = 'onboarding-pulse-style';
const PULSE_CSS = `
@keyframes onboarding-pulse {
  0%, 100% { box-shadow: 0 0 0 3px rgba(0,212,255,0.8), 0 0 20px rgba(0,212,255,0.4); }
  50% { box-shadow: 0 0 0 5px rgba(0,212,255,0.6), 0 0 30px rgba(0,212,255,0.3); }
}
.onboarding-target-glow {
  animation: onboarding-pulse 2s ease-in-out infinite;
  border-radius: 12px;
  position: relative;
  z-index: 1310;
}
.onboarding-target-glow-fixed {
  animation: onboarding-pulse 2s ease-in-out infinite;
  border-radius: 12px;
  z-index: 1310;
}
`;

export function OnboardingGuide() {
  const { active, phase, phases, nextPhase, complete } = useOnboardingStore();
  const config = active && phase ? getPhaseConfig(phase) : null;
  const { rect, element } = useTargetRect(config?.target ?? null);
  const prevElementRef = useRef<HTMLElement | null>(null);
  const [showTooltip, setShowTooltip] = useState(true);

  // Inject/remove pulse keyframes
  useEffect(() => {
    if (!active) return;
    if (!document.getElementById(STYLE_ID)) {
      const style = document.createElement('style');
      style.id = STYLE_ID;
      style.textContent = PULSE_CSS;
      document.head.appendChild(style);
    }
    return () => {
      document.getElementById(STYLE_ID)?.remove();
    };
  }, [active]);

  // Apply/remove glow class on target element
  useEffect(() => {
    // Remove glow from previous target
    if (prevElementRef.current && prevElementRef.current !== element) {
      prevElementRef.current.classList.remove(
        'onboarding-target-glow',
        'onboarding-target-glow-fixed',
      );
    }

    if (element) {
      const isFixed = window.getComputedStyle(element).position === 'fixed';
      element.classList.add(
        isFixed ? 'onboarding-target-glow-fixed' : 'onboarding-target-glow',
      );
      prevElementRef.current = element;
    }

    return () => {
      if (element) {
        element.classList.remove('onboarding-target-glow', 'onboarding-target-glow-fixed');
      }
    };
  }, [element]);

  // Click detection on target — advance guide
  useEffect(() => {
    if (!element || !active) return;

    const handler = () => {
      // Let the native click handler execute first (navigation, toggle, etc.)
      setTimeout(() => {
        // Phase 1→2: wait for collapse animation before showing tooltip
        if (phase === 1) {
          setShowTooltip(false);
          setTimeout(() => {
            nextPhase();
            setShowTooltip(true);
          }, 400);
        } else {
          nextPhase();
        }
      }, 0);
    };

    element.addEventListener('click', handler);
    return () => element.removeEventListener('click', handler);
  }, [element, active, phase, nextPhase]);

  // Reset showTooltip when phase changes (except during 1→2 transition handled above)
  useEffect(() => {
    setShowTooltip(true);
  }, [phase]);

  if (!active || !config || !rect) return null;

  const currentIndex = phases.indexOf(phase!);
  const totalSteps = phases.length;

  return (
    <>
      <SpotlightOverlay rect={rect} />
      {showTooltip && (
        <OnboardingTooltip
          rect={rect}
          placement={config.placement}
          i18nKey={config.i18nKey}
          currentIndex={currentIndex}
          totalSteps={totalSteps}
          onSkip={complete}
        />
      )}
    </>
  );
}
