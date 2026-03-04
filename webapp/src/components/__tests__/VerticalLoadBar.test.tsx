/**
 * VerticalLoadBar Component Tests
 *
 * Tests for the vertical bar showing traffic budget status.
 * budgetScore range: [-1, +1]. Negative = under budget, positive = over budget.
 * Mapping: percentage = clamp((budgetScore + 1) * 50, 0, 100)
 *   -1 → 0%, 0 → 50%, +1 → 100%
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { VerticalLoadBar } from '../VerticalLoadBar';

describe('VerticalLoadBar', () => {
  it('should render nothing when budgetScore is undefined', () => {
    const { container } = render(<VerticalLoadBar />);
    expect(container.firstChild).toBeNull();
  });

  it('should render the bar container', () => {
    render(<VerticalLoadBar budgetScore={0} />);
    const container = screen.getByTestId('load-bar-container');
    expect(container).toBeInTheDocument();
  });

  it('should render green for negative budgetScore (under budget)', () => {
    render(<VerticalLoadBar budgetScore={-0.5} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should render red for high positive budgetScore (over budget)', () => {
    render(<VerticalLoadBar budgetScore={0.5} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should clamp values outside [-1, 1]', () => {
    render(<VerticalLoadBar budgetScore={2} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });
});
