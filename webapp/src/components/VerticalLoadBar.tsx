/**
 * VerticalLoadBar Component
 *
 * Displays a minimal vertical progress bar for traffic budget status.
 * budgetScore: [-1, +1]. Negative = under budget (green), positive = over budget (red).
 */

import { Box } from '@mui/material';

interface VerticalLoadBarProps {
  /** Budget score (-1 to +1). Undefined renders nothing. */
  budgetScore?: number;
}

export function VerticalLoadBar({ budgetScore }: VerticalLoadBarProps) {
  if (budgetScore === undefined) return null;

  // Map [-1, +1] → [0, 100]
  const percentage = Math.max(0, Math.min(100, (budgetScore + 1) * 50));

  const color = percentage < 40
    ? 'success.main'
    : percentage < 65
      ? 'warning.main'
      : 'error.main';

  return (
    <Box
      data-testid="load-bar-container"
      sx={{
        width: 4,
        height: 24,
        bgcolor: 'action.hover',
        borderRadius: 1,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'flex-end',
      }}
    >
      <Box
        data-testid="load-bar-fill"
        sx={{
          width: '100%',
          height: `${percentage}%`,
          bgcolor: color,
          borderRadius: 1,
          transition: 'height 0.3s ease',
        }}
      />
    </Box>
  );
}
