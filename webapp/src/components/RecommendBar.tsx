import { Box } from '@mui/material';

interface RecommendBarProps {
  score: number | undefined;
}

type BandColor = 'success' | 'warning' | 'error';

function bandFor(score: number): BandColor {
  if (score >= 0.6) return 'success';
  if (score >= 0.3) return 'warning';
  return 'error';
}

export function RecommendBar({ score }: RecommendBarProps) {
  if (score === undefined) return null;

  const clamped = Math.max(0, Math.min(1, score));
  const heightPct = Math.round(clamped * 100);
  const band = bandFor(clamped);

  return (
    <Box
      data-testid="recommend-bar"
      sx={{
        width: 4,
        height: 24,
        bgcolor: 'action.hover',
        borderRadius: 1,
        display: 'flex',
        alignItems: 'flex-end',
      }}
    >
      <Box
        data-testid="recommend-bar-fill"
        data-color={band}
        style={{ height: `${heightPct}%` }}
        sx={{
          width: '100%',
          bgcolor: `${band}.main`,
          borderRadius: 1,
          transition: 'height 0.3s ease',
        }}
      />
    </Box>
  );
}
