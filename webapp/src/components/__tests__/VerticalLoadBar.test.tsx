/**
 * VerticalLoadBar Component Tests
 *
 * Tests for the vertical load bar component used to display node load.
 * Shows a de-emphasized vertical progress bar with color coding.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { VerticalLoadBar } from '../VerticalLoadBar';

describe('VerticalLoadBar', () => {
  it('should render nothing when load is undefined', () => {
    const { container } = render(<VerticalLoadBar />);
    expect(container.firstChild).toBeNull();
  });

  it('should render the bar container', () => {
    render(<VerticalLoadBar load={50} />);

    const container = screen.getByTestId('load-bar-container');
    expect(container).toBeInTheDocument();
  });

  it('should render the fill bar', () => {
    render(<VerticalLoadBar load={50} />);

    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should render with load over 100 (capped)', () => {
    render(<VerticalLoadBar load={150} />);

    // Component should render even with over 100% load
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should render with 0% load', () => {
    render(<VerticalLoadBar load={0} />);

    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });
});
