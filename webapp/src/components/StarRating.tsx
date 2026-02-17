/**
 * StarRating Component
 *
 * Displays a star rating (1-5 stars) based on route quality score.
 * Used in route diagnosis to show recommendation level without technical jargon.
 */

import { Box } from '@mui/material';
import StarIcon from '@mui/icons-material/Star';

interface StarRatingProps {
  /** Rating value from 0-5. 0 renders nothing. */
  value: number;
}

export function StarRating({ value }: StarRatingProps) {
  // Don't render anything for 0 (no diagnosis result)
  if (value === 0) return null;

  return (
    <Box sx={{ display: 'flex', gap: 0.25 }}>
      {[1, 2, 3, 4, 5].map((star) => {
        const isFilled = star <= value;
        return (
          <StarIcon
            key={star}
            data-testid={isFilled ? 'star-filled' : 'star-empty'}
            sx={{
              fontSize: 14,
              color: isFilled ? 'warning.main' : 'action.disabled',
            }}
          />
        );
      })}
    </Box>
  );
}
