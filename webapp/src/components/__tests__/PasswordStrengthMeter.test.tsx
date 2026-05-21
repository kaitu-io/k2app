import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';
import PasswordStrengthMeter from '../PasswordStrengthMeter';

function renderWithI18n(ui: React.ReactElement) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>);
}

describe('PasswordStrengthMeter', () => {
  it('renders nothing when hidden', () => {
    renderWithI18n(<PasswordStrengthMeter score={0} tooShort={true} hidden />);
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('renders the strength label for score=2', () => {
    renderWithI18n(<PasswordStrengthMeter score={2} tooShort={false} />);
    expect(screen.getByText(/一般|Fair|普通/)).toBeInTheDocument();
  });

  it('renders the strength label for score=4', () => {
    renderWithI18n(<PasswordStrengthMeter score={4} tooShort={false} />);
    expect(screen.getByText(/极强|Very strong|非常に強い|極強/)).toBeInTheDocument();
  });

  it('exposes progressbar role with aria-valuenow', () => {
    renderWithI18n(<PasswordStrengthMeter score={3} tooShort={false} />);
    const bar = screen.getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '3');
    expect(bar).toHaveAttribute('aria-valuemax', '4');
  });

  it('treats tooShort=true as effective score=0', () => {
    renderWithI18n(<PasswordStrengthMeter score={4} tooShort={true} />);
    const bar = screen.getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '0');
    expect(screen.getByText(/极弱|Very weak|非常に弱い|極弱/)).toBeInTheDocument();
  });
});
