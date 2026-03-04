/**
 * Onboarding Store — 用户引导状态管理
 *
 * 管理首次登录引导的 phase 进度、活跃状态、跳过/完成。
 * 通过 _platform.storage 持久化完成标记，跨平台可靠。
 *
 * Phase 流程:
 *   1: 折叠 → 2: 展开 → 3: 反馈FAB
 *   → 4: 邀请导航 → 5: 购买导航（非iOS）
 *
 * 所有 phase 都在 / 路由，点击目标元素推进，导航类点击被拦截。
 */

import { create } from 'zustand';
import { isFeatureEnabled } from '../config/apps';

const STORAGE_KEY = 'onboarding_completed';

/** All possible phases */
type Phase = 1 | 2 | 3 | 4 | 5;

/** Route each phase expects */
const PHASE_ROUTE: Record<Phase, string> = {
  1: '/',
  2: '/',
  3: '/',
  4: '/',
  5: '/',
};

interface OnboardingState {
  /** Current phase (1-5) */
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
  /** Get the expected route for the current phase */
  getExpectedRoute: () => string;
  /** Initialize: check storage for completion */
  init: () => Promise<void>;
}

function buildPhaseList(): Phase[] {
  const isIOS = window._platform?.os === 'ios';
  const hasInvite = isFeatureEnabled('invite');

  const phases: Phase[] = [1, 2, 3];

  if (hasInvite) {
    phases.push(4);
  }

  if (!isIOS) {
    phases.push(5);
  }

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

  getExpectedRoute: () => {
    return PHASE_ROUTE[get().phase];
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
