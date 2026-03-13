/**
 * Onboarding Store — 用户引导状态管理
 *
 * 管理首次登录引导的 phase 进度、活跃状态、跳过/完成。
 * 通过 _platform.storage 持久化完成标记，跨平台可靠。
 *
 * Phase 流程:
 *   1: 折叠面板 → 2: 展开面板 → 3: 反馈按钮
 *   → 4: 邀请导航(/) → 5: 分享按钮(/invite)
 *   → 6: 回到仪表板(所有平台)
 */

import { create } from 'zustand';
import { isFeatureEnabled } from '../config/apps';

const STORAGE_KEY = 'onboarding_completed';

/** All possible phases */
export type Phase = 1 | 2 | 3 | 4 | 5 | 6;

/** Phase configuration for the guide UI */
export interface PhaseConfig {
  target: string;
  placement: 'top' | 'bottom' | 'left' | 'right';
  /** Tooltip i18n key suffix: `onboarding.${i18nKey}.title/content` */
  i18nKey: string;
}

const PHASE_CONFIG: Record<Phase, PhaseConfig> = {
  1: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', i18nKey: 'phase1' },
  2: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', i18nKey: 'phase2' },
  3: { target: '[data-tour="feedback-button"]', placement: 'left', i18nKey: 'phase3' },
  4: { target: '[data-tour="nav-invite"]', placement: 'top', i18nKey: 'phase4' },
  5: { target: '[data-tour="invite-share"]', placement: 'bottom', i18nKey: 'phase5' },
  6: { target: '[data-tour="nav-dashboard"]', placement: 'top', i18nKey: 'phase6' },
};

/** Get phase config (no platform-specific overrides needed) */
export function getPhaseConfig(phase: Phase): PhaseConfig {
  return PHASE_CONFIG[phase];
}

interface OnboardingState {
  /** Current phase */
  phase: Phase;
  /** Whether onboarding is actively running */
  active: boolean;
  /** Ordered list of phases for this platform */
  phases: Phase[];
  // Actions
  /** Start the onboarding tour */
  start: () => void;
  /** Check storage and start if not completed */
  tryStart: () => Promise<void>;
  /** Advance to the next phase */
  nextPhase: () => void;
  /** Skip/complete the onboarding */
  complete: () => void;
  /** Initialize: check storage for completion */
  init: () => Promise<void>;
}

function buildPhaseList(): Phase[] {
  const hasInvite = isFeatureEnabled('invite');

  const phases: Phase[] = [1, 2, 3];

  if (hasInvite) {
    phases.push(4, 5);
  }

  // All platforms get final step (iOS targets dashboard, others target purchase)
  phases.push(6);

  return phases;
}

export const useOnboardingStore = create<OnboardingState>()((set, get) => ({
  phase: 1,
  active: false,
  phases: [],
  start: () => {
    const phases = buildPhaseList();
    set({ active: true, phase: phases[0], phases });
  },

  tryStart: async () => {
    const completed = await window._platform?.storage.get<boolean>(STORAGE_KEY);
    if (!completed) {
      get().start();
    }
  },

  nextPhase: () => {
    const { phase, phases } = get();
    const currentIndex = phases.indexOf(phase);
    if (currentIndex < phases.length - 1) {
      set({ phase: phases[currentIndex + 1] });
    } else {
      // Last phase done
      get().complete();
    }
  },

  complete: () => {
    set({ active: false });
    // Persist completion
    window._platform?.storage.set(STORAGE_KEY, true).catch(() => {});
  },

  init: async () => {
    try {
      const completed = await window._platform?.storage.get<boolean>(STORAGE_KEY);
      if (completed) {
        set({ active: false });
      }
      // If not completed, don't auto-start — Layout will trigger start after login
    } catch {
      // Storage unavailable, treat as not completed
    }
  },
}));
