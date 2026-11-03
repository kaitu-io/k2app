import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { readFileSync, readdirSync } from 'fs';
import { join } from 'path';
import HomeTrialCard from '../HomeTrialCard';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', () => ({ useNavigate: () => mockNavigate }));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (k: string, o?: Record<string, unknown>) => (o ? `${k}|${JSON.stringify(o)}` : k),
  }),
}));

const NOW = 1_800_000_000;

describe('HomeTrialCard', () => {
  beforeEach(() => {
    mockNavigate.mockReset();
  });

  it('button navigates to /purchase with from=trial_card', () => {
    render(<HomeTrialCard daysLeft={3} endsAtSec={NOW} urgent={false} />);
    fireEvent.click(screen.getByTestId('home-trial-continue'));
    expect(mockNavigate).toHaveBeenCalledWith('/purchase', { state: { from: 'trial_card' } });
  });

  it('shows days in message', () => {
    render(<HomeTrialCard daysLeft={3} endsAtSec={NOW} urgent />);
    expect(screen.getByTestId('home-trial-card').textContent).toContain('"days":3');
  });
});

describe('trial card copy', () => {
  const base = join(__dirname, '../../../i18n/locales');
  const forbidden = /官网|官方网站|折|%|\$|kaitu|开途|開途|overleap|discount|website|official/i;
  for (const loc of readdirSync(base).filter((d) => !d.includes('.'))) {
    it(`${loc} copy has no website/discount/price/brand`, () => {
      const d = JSON.parse(readFileSync(join(base, loc, 'dashboard.json'), 'utf8')).dashboard;
      expect(d.trialRenewOff).toBeTruthy();
      expect(d.trialContinue).toBeTruthy();
      expect(d.trialRenewOff).toContain('{{days}}');
      expect(d.trialRenewOff).toContain('{{date}}');
      expect(d.trialRenewOff).not.toMatch(forbidden);
      expect(d.trialContinue).not.toMatch(forbidden);
    });
  }
});
