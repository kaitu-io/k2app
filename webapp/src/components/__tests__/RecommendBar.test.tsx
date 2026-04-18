import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { RecommendBar } from '../RecommendBar';

describe('RecommendBar', () => {
  it('renders nothing when score is undefined', () => {
    const { container } = render(<RecommendBar score={undefined} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders a 90%-tall success-colored fill for score 0.9', () => {
    render(<RecommendBar score={0.9} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('90%');
    expect(fill.getAttribute('data-color')).toBe('success');
  });

  it('renders success color at lower boundary 0.6', () => {
    render(<RecommendBar score={0.6} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('60%');
    expect(fill.getAttribute('data-color')).toBe('success');
  });

  it('renders warning color for 0.3 ≤ score < 0.6', () => {
    render(<RecommendBar score={0.45} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('45%');
    expect(fill.getAttribute('data-color')).toBe('warning');
  });

  it('renders warning color at lower boundary 0.3', () => {
    render(<RecommendBar score={0.3} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('30%');
    expect(fill.getAttribute('data-color')).toBe('warning');
  });

  it('renders error color for score < 0.3', () => {
    render(<RecommendBar score={0.15} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('15%');
    expect(fill.getAttribute('data-color')).toBe('error');
  });

  it('renders error color for score 0 with 0% height', () => {
    render(<RecommendBar score={0} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('0%');
    expect(fill.getAttribute('data-color')).toBe('error');
  });

  it('clamps score above 1 to 100%', () => {
    render(<RecommendBar score={1.5} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('100%');
  });

  it('clamps negative score to 0%', () => {
    render(<RecommendBar score={-0.5} />);
    const fill = screen.getByTestId('recommend-bar-fill') as HTMLElement;
    expect(fill.style.height).toBe('0%');
  });
});
