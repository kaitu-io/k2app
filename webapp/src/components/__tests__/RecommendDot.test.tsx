import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { RecommendDot } from '../RecommendDot';

describe('RecommendDot', () => {
  it('renders a neutral dot when score is undefined', () => {
    render(<RecommendDot />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('⚪');
  });

  it('renders green for score ≥ 0.6 (recommended)', () => {
    render(<RecommendDot score={0.7} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🟢');
  });

  it('renders green at the lower boundary 0.6', () => {
    render(<RecommendDot score={0.6} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🟢');
  });

  it('renders yellow for 0.3 ≤ score < 0.6 (caution)', () => {
    render(<RecommendDot score={0.45} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🟡');
  });

  it('renders yellow at the lower boundary 0.3', () => {
    render(<RecommendDot score={0.3} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🟡');
  });

  it('renders red for score < 0.3 (over budget)', () => {
    render(<RecommendDot score={0.2} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🔴');
  });

  it('renders red for score = 0', () => {
    render(<RecommendDot score={0} />);
    expect(screen.getByTestId('recommend-dot')).toHaveTextContent('🔴');
  });
});
