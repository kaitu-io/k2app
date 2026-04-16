import { Box } from '@mui/material';

interface RecommendDotProps {
  /**
   * Canonical recommendation signal in [0, 1]. Higher = better pick.
   * Undefined = no data yet (renders a neutral gray dot).
   */
  score?: number;
}

function dotFor(score: number | undefined): string {
  if (score === undefined) return '⚪';
  if (score >= 0.6) return '🟢';
  if (score >= 0.3) return '🟡';
  return '🔴';
}

/**
 * RecommendDot displays the canonical `recommendScore` as a single emoji dot.
 *
 * Replaces the earlier VerticalLoadBar which mapped a signed budgetScore
 * [-1, +1] onto a percentage with three color stops. The new contract is:
 * the backend produces a [0, 1] score directly (higher = better), and the
 * UI simply reads its sign. Thresholds (0.6, 0.3) correspond to the old bar's
 * 40% and 65% breakpoints — the visual meaning is preserved.
 */
export function RecommendDot({ score }: RecommendDotProps) {
  return (
    <Box
      component="span"
      data-testid="recommend-dot"
      sx={{ fontSize: 16, lineHeight: 1, display: 'inline-flex' }}
    >
      {dotFor(score)}
    </Box>
  );
}
