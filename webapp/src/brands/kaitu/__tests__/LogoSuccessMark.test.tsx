import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { render, screen } from '@testing-library/react';
import { LogoSuccessMark } from '../LogoSuccessMark';

describe('LogoSuccessMark', () => {
  it('renders the static logo when not in the success state', () => {
    render(<LogoSuccessMark success={false} />);
    const svg = screen.getByTestId('logo-success-mark');
    expect(svg.getAttribute('data-state')).toBe('idle');
    expect(svg.querySelectorAll('path')).toHaveLength(3);
  });

  it('switches to the success state', () => {
    const { rerender } = render(<LogoSuccessMark success={false} />);
    rerender(<LogoSuccessMark success />);
    expect(screen.getByTestId('logo-success-mark').getAttribute('data-state')).toBe('success');
  });

  it('gives each instance its own clip path', () => {
    render(
      <>
        <LogoSuccessMark success={false} />
        <LogoSuccessMark success />
      </>,
    );
    const ids = screen
      .getAllByTestId('logo-success-mark')
      .map((svg) => svg.querySelector('clipPath')?.id);
    expect(ids[0]).toBeTruthy();
    expect(ids[0]).not.toBe(ids[1]);
  });

  // The component redraws logo.svg by hand; this keeps the two from drifting.
  it('uses the same stem, vertex, widths and colours as logo.svg', () => {
    const master = readFileSync(resolve(__dirname, '../../../../brand-assets/kaitu/logo.svg'), 'utf8');
    const { container } = render(<LogoSuccessMark success={false} />);
    const html = container.innerHTML;
    for (const token of ['M164 92V420', 'stroke-width="92"', 'stroke-width="56"', 'rx="114"', '#0a0a0f', '#00ff88']) {
      expect(master, `logo.svg lost ${token}`).toContain(token);
      expect(html, `component lost ${token}`).toContain(token);
    }
    expect(master).toContain('L278 256L');
    expect(html).toContain('M278 256L');
  });
});
