/**
 * ModeChip Component Tests
 *
 * Covers the 4 render branches:
 *   1. auto mode with a detected country → flag + localized name + change button
 *   2. auto mode without detection → "Smart mode (detecting...)" placeholder
 *   3. global override → 🌐 Global mode
 *   4. manual override (chnroute) → 🇨🇳 China bypass (manual)
 *   5. manual override (global)   → 🌐 Global (manual)
 *
 * The store is mutated directly via `useConfigStore.setState()` between
 * renders — this keeps the tests free of async loadConfig plumbing and
 * storage mocks.
 */

import { describe, it, expect, beforeEach, afterEach, beforeAll } from 'vitest';
import { render, screen, cleanup } from '../../test/utils/render';

import ModeChip from '../ModeChip';
import { useConfigStore } from '../../stores/config.store';
import { changeLanguage } from '../../i18n/i18n';

// Snapshot the default state so each test starts clean.
const defaultState = useConfigStore.getState();

beforeAll(async () => {
  // Force English so our text assertions are stable regardless of how the
  // navigator locale is mocked in the shared setup.
  await changeLanguage('en-US');
});

beforeEach(() => {
  useConfigStore.setState({
    ...defaultState,
    ruleMode: 'chnroute',
    detectedCountry: null,
    suggestedProfile: null,
    modeOverride: 'auto',
    lastAcknowledgedCountry: null,
    loaded: true,
  });
});

afterEach(() => {
  cleanup();
});

describe('ModeChip', () => {
  it('auto + detectedCountry → renders country name and change button', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'ru',
      suggestedProfile: 'ruroute',
    });

    render(<ModeChip />);

    expect(screen.getByTestId('mode-chip')).toBeInTheDocument();
    expect(screen.getByText('Russia')).toBeInTheDocument();
    expect(screen.getByText(/Smart bypass/i)).toBeInTheDocument();
    expect(screen.getByTestId('mode-chip-change')).toBeInTheDocument();
  });

  it('auto without detection → renders detecting placeholder', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: null,
    });

    render(<ModeChip />);

    expect(screen.getByText(/detecting/i)).toBeInTheDocument();
    expect(screen.queryByText('Russia')).not.toBeInTheDocument();
  });

  it('modeOverride=global → renders Global mode chip', () => {
    useConfigStore.setState({
      modeOverride: 'global',
      detectedCountry: 'cn', // should be ignored in global branch
    });

    render(<ModeChip />);

    expect(screen.getByText(/Global mode/i)).toBeInTheDocument();
    // "Smart bypass" is the auto-branch label — must NOT show in global.
    expect(screen.queryByText(/Smart bypass/i)).not.toBeInTheDocument();
  });

  it('modeOverride=manual + ruleMode=chnroute → renders China bypass (manual)', () => {
    useConfigStore.setState({
      modeOverride: 'manual',
      ruleMode: 'chnroute',
    });

    render(<ModeChip />);

    expect(screen.getByText(/China bypass.*manual/i)).toBeInTheDocument();
  });

  it('modeOverride=manual + ruleMode=global → renders Global (manual)', () => {
    useConfigStore.setState({
      modeOverride: 'manual',
      ruleMode: 'global',
    });

    render(<ModeChip />);

    expect(screen.getByText(/Global.*manual/i)).toBeInTheDocument();
  });

  it('always renders a change button regardless of mode', () => {
    const modes = [
      { modeOverride: 'auto' as const, detectedCountry: 'ir' },
      { modeOverride: 'auto' as const, detectedCountry: null },
      { modeOverride: 'global' as const, detectedCountry: null },
      { modeOverride: 'manual' as const, ruleMode: 'chnroute' as const, detectedCountry: null },
    ];

    for (const partial of modes) {
      useConfigStore.setState(partial);
      const { unmount } = render(<ModeChip />);
      expect(screen.getByTestId('mode-chip-change')).toBeInTheDocument();
      unmount();
    }
  });
});
