import { Box, Skeleton, Tooltip, Typography } from '@mui/material';
import type { ProbeResult } from '../services/api-types';

const LOSS_WARN_THRESHOLD = 0.05;

interface ProbeChipProps {
  result: ProbeResult | null;
  loading: boolean;
}

/**
 * Compact measurement indicator, rendered alongside RecommendDot in
 * CloudTunnelList. Five visual states:
 *
 *  1. loading                  → MUI Skeleton (40x18 rounded)
 *  2. reachable + supported    → "42 ms" + optional "12%" when lossRate >= 0.05
 *  3. reachable + unsupported  → "?" (old k2s — no score available)
 *  4. unreachable              → "—"
 *  5. result=null, !loading    → nothing (tunnel hasn't been probed)
 */
export function ProbeChip({ result, loading }: ProbeChipProps) {
  if (loading) {
    return <Skeleton variant="rounded" width={40} height={18} />;
  }
  if (!result) return null;

  if (!result.reachable) {
    return (
      <Tooltip title="Unreachable (QUIC handshake failed)">
        <Typography variant="caption" sx={{ color: 'text.disabled', fontSize: '0.72rem' }}>
          —
        </Typography>
      </Tooltip>
    );
  }

  if (!result.echoSupported) {
    return (
      <Tooltip title="Server doesn't report quality">
        <Typography variant="caption" sx={{ color: 'text.disabled', fontSize: '0.72rem' }}>
          ?
        </Typography>
      </Tooltip>
    );
  }

  const rtt = Math.round(result.avgRttMs);
  const lossPct = Math.round(result.lossRate * 100);
  const showLoss = result.lossRate >= LOSS_WARN_THRESHOLD;

  return (
    <Tooltip title={`RTT ${rtt} ms · jitter ${Math.round(result.jitterMs)} ms · loss ${lossPct}%`}>
      <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}>
        <Typography variant="caption" sx={{ fontSize: '0.72rem', fontVariantNumeric: 'tabular-nums' }}>
          {rtt} ms
        </Typography>
        {showLoss && (
          <Typography variant="caption" sx={{ color: 'warning.main', fontSize: '0.72rem' }}>
            {lossPct}%
          </Typography>
        )}
      </Box>
    </Tooltip>
  );
}
