/**
 * Onboarding Store Unit Tests
 *
 * Tests:
 * - Phase progression (full flow with invite)
 * - Phase progression (no invite feature)
 * - iOS phase 6 config override
 * - complete() persists to storage
 * - tryStart() skips when completed
 * - tryStart() starts when not completed
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/onboarding.store.test.ts
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';

// Mock isFeatureEnabled before importing store
vi.mock('../../config/apps', () => ({
  isFeatureEnabled: vi.fn(() => true),
  getCurrentAppConfig: vi.fn(() => ({ features: {} })),
}));

import { isFeatureEnabled } from '../../config/apps';
const mockFeature = vi.mocked(isFeatureEnabled);

// Mock window._platform
const mockStorage = {
  get: vi.fn(),
  set: vi.fn().mockResolvedValue(undefined),
  remove: vi.fn(),
  has: vi.fn(),
  clear: vi.fn(),
  keys: vi.fn(),
};

const mockPlatform = {
  os: 'windows' as string,
  version: '1.0.0',
  storage: mockStorage,
  syncLocale: vi.fn(),
  openExternal: vi.fn(),
};

Object.defineProperty(window, '_platform', {
  value: mockPlatform,
  writable: true,
  configurable: true,
});

import { useOnboardingStore, getPhaseConfig } from '../onboarding.store';

describe('onboarding.store', () => {
  beforeEach(() => {
    // Reset store state
    useOnboardingStore.setState({ phase: 1, active: false, phases: [], _advancing: false });
    vi.clearAllMocks();
    mockFeature.mockReturnValue(true);
    mockPlatform.os = 'windows';
    mockStorage.set.mockResolvedValue(undefined);
  });

  describe('phase progression (full flow with invite)', () => {
    it('progresses through all 6 phases then completes', () => {
      const store = useOnboardingStore.getState();
      store.start();

      let state = useOnboardingStore.getState();
      expect(state.active).toBe(true);
      expect(state.phase).toBe(1);
      expect(state.phases).toEqual([1, 2, 3, 4, 5, 6]);

      state.nextPhase();
      expect(useOnboardingStore.getState().phase).toBe(2);

      useOnboardingStore.getState().nextPhase();
      expect(useOnboardingStore.getState().phase).toBe(3);

      useOnboardingStore.getState().nextPhase();
      expect(useOnboardingStore.getState().phase).toBe(4);

      useOnboardingStore.getState().nextPhase();
      expect(useOnboardingStore.getState().phase).toBe(5);

      useOnboardingStore.getState().nextPhase();
      expect(useOnboardingStore.getState().phase).toBe(6);

      // Last phase → complete
      useOnboardingStore.getState().nextPhase();
      expect(useOnboardingStore.getState().active).toBe(false);
    });
  });

  describe('phase progression (no invite)', () => {
    it('skips phases 4-5 when invite feature disabled', () => {
      mockFeature.mockReturnValue(false);

      useOnboardingStore.getState().start();

      const state = useOnboardingStore.getState();
      expect(state.phases).toEqual([1, 2, 3, 6]);

      state.nextPhase(); // 1→2
      useOnboardingStore.getState().nextPhase(); // 2→3
      useOnboardingStore.getState().nextPhase(); // 3→6
      expect(useOnboardingStore.getState().phase).toBe(6);

      useOnboardingStore.getState().nextPhase(); // 6→complete
      expect(useOnboardingStore.getState().active).toBe(false);
    });
  });

  describe('getPhaseConfig', () => {
    it('returns dashboard target for phase 6 on all platforms', () => {
      const config = getPhaseConfig(6);
      expect(config.targets).toContain('[data-tour="nav-dashboard"]');
      expect(config.i18nKey).toBe('phase6');
    });

    it('returns correct config for each phase', () => {
      expect(getPhaseConfig(1).targets).toContain('[data-tour="collapse-toggle"]');
      expect(getPhaseConfig(3).targets).toContain('[data-tour="feedback-button"]');
      expect(getPhaseConfig(4).targets).toContain('[data-tour="nav-invite"]');
    });

    it('phase 5 has fallback targets for desktop and mobile', () => {
      const config = getPhaseConfig(5);
      expect(config.targets).toEqual(['[data-tour="invite-share"]', '[data-tour="invite-copy"]']);
    });
  });

  describe('complete()', () => {
    it('sets active to false and persists to storage', () => {
      useOnboardingStore.getState().start();
      expect(useOnboardingStore.getState().active).toBe(true);

      useOnboardingStore.getState().complete();
      expect(useOnboardingStore.getState().active).toBe(false);
      expect(mockStorage.set).toHaveBeenCalledWith('onboarding_completed', true);
    });
  });

  describe('tryStart()', () => {
    it('skips when already completed', async () => {
      mockStorage.get.mockResolvedValue(true);
      await useOnboardingStore.getState().tryStart();
      expect(useOnboardingStore.getState().active).toBe(false);
    });

    it('starts when not completed', async () => {
      mockStorage.get.mockResolvedValue(false);
      await useOnboardingStore.getState().tryStart();
      expect(useOnboardingStore.getState().active).toBe(true);
      expect(useOnboardingStore.getState().phase).toBe(1);
    });

    it('starts when storage returns null/undefined', async () => {
      mockStorage.get.mockResolvedValue(null);
      await useOnboardingStore.getState().tryStart();
      expect(useOnboardingStore.getState().active).toBe(true);
    });
  });
});
