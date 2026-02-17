/**
 * StarRating Component Tests
 *
 * Tests for the star rating display component used in route diagnosis.
 * Shows 1-5 stars based on route quality score.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { StarRating } from '../StarRating';

describe('StarRating', () => {
  it('should render nothing when value is 0', () => {
    const { container } = render(<StarRating value={0} />);
    expect(container.firstChild).toBeNull();
  });

  it('should render 5 filled stars for value of 5', () => {
    render(<StarRating value={5} />);

    // All 5 stars should be filled
    const filledStars = screen.getAllByTestId('star-filled');
    expect(filledStars).toHaveLength(5);

    // No empty stars
    const emptyStars = screen.queryAllByTestId('star-empty');
    expect(emptyStars).toHaveLength(0);
  });

  it('should render 3 filled stars and 2 empty stars for value of 3', () => {
    render(<StarRating value={3} />);

    const filledStars = screen.getAllByTestId('star-filled');
    expect(filledStars).toHaveLength(3);

    const emptyStars = screen.getAllByTestId('star-empty');
    expect(emptyStars).toHaveLength(2);
  });

  it('should render 1 filled star for minimum value of 1', () => {
    render(<StarRating value={1} />);

    const filledStars = screen.getAllByTestId('star-filled');
    expect(filledStars).toHaveLength(1);

    const emptyStars = screen.getAllByTestId('star-empty');
    expect(emptyStars).toHaveLength(4);
  });
});
