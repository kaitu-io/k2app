import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import PasswordStrengthMeter from '../PasswordStrengthMeter';

// Note: `next-intl` is globally mocked in `src/test/setup.ts` to return raw
// keys, so we assert on key strings rather than translated copy. The i18n
// JSON files are validated separately by `jq` in the implementation step.

describe('PasswordStrengthMeter (web)', () => {
  it('renders progressbar with aria-valuenow', () => {
    render(<PasswordStrengthMeter score={3} tooShort={false} />);
    const bar = screen.getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '3');
    expect(bar).toHaveAttribute('aria-valuemin', '0');
    expect(bar).toHaveAttribute('aria-valuemax', '4');
  });

  it('renders nothing when hidden', () => {
    render(<PasswordStrengthMeter score={0} tooShort={true} hidden />);
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('renders strength label for score=2 (fair)', () => {
    render(<PasswordStrengthMeter score={2} tooShort={false} />);
    // The mocked translator returns raw keys; we assert on the fair-key path
    // so the wiring from score→i18n-key is locked down.
    expect(screen.getByText(/password\.strength\.fair/)).toBeInTheDocument();
  });

  it('treats tooShort=true as effective score=0', () => {
    render(<PasswordStrengthMeter score={4} tooShort={true} />);
    const bar = screen.getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuenow', '0');
    // veryWeak key, not strong
    expect(screen.getByText(/password\.strength\.veryWeak/)).toBeInTheDocument();
  });
});
