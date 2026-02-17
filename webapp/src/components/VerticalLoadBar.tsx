/**
 * VerticalLoadBar Component
 *
 * Displays a minimal vertical progress bar for node load.
 * De-emphasized UI element with color coding based on load level.
 */

import { Box } from '@mui/material';

interface VerticalLoadBarProps {
  /** Load percentage (0-100). Undefined renders nothing. */
  load?: number;
}

export function VerticalLoadBar({ load }: VerticalLoadBarProps) {
  if (load === undefined) return null;

  const clampedLoad = Math.min(load, 100);
  const color = clampedLoad < 50
    ? 'success.main'
    : clampedLoad < 80
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
          height: `${clampedLoad}%`,
          bgcolor: color,
          borderRadius: 1,
          transition: 'height 0.3s ease',
        }}
      />
    </Box>
  );
}
