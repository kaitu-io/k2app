/**
 * The kaitu logo that turns into a check mark: the K's chevron rotates 90°
 * counter-clockwise, its upper arm shortens, the stem fades and the tile pops.
 *
 * NOT WIRED IN YET — nothing imports this file, so it is in no bundle. It lives
 * under brands/kaitu because the geometry is kaitu artwork; import it only from
 * kaitu-gated code, never statically from a shared page (that would ship it in
 * the other brand's bundle).
 *
 * Geometry and colours mirror webapp/brand-assets/kaitu/logo.svg (512 box) —
 * change both together.
 */
import { useId } from 'react';
import { Box } from '@mui/material';

const TILE = '#00ff88';
const INK = '#0a0a0f';
const ARM_LENGTH = 419; // 296·√2, vertex → far end of an arm
const SHORT_ARM_OFFSET = 311; // dash offset that leaves the check's short stroke
const CHECK_TRANSFORM = 'translate(-24px, 74px) rotate(-90deg)';

export interface LogoSuccessMarkProps {
  /** Rendered width/height in px. */
  size?: number;
  /** false = the static logo; true = play once and rest on the check mark. */
  success: boolean;
  /** Animation length in ms. */
  durationMs?: number;
}

export function LogoSuccessMark({ size = 96, success, durationMs = 700 }: LogoSuccessMarkProps) {
  const clipId = useId();
  const run = (name: string, easing: string) =>
    success ? `${name} ${durationMs}ms ${easing} forwards` : 'none';

  return (
    <Box
      component="svg"
      viewBox="0 0 512 512"
      role="img"
      aria-hidden
      data-testid="logo-success-mark"
      data-state={success ? 'success' : 'idle'}
      sx={{
        // In sx, not attributes: Box treats width/height props as system props.
        width: size,
        height: size,
        flex: 'none',
        overflow: 'visible',
        '& .pop, & .turn': { transformOrigin: '256px 256px' },
        '& .pop': { animation: run('k2-logo-pop', 'ease-out') },
        '& .turn': { animation: run('k2-logo-turn', 'cubic-bezier(.6, 0, .2, 1)') },
        '& .arm': { animation: run('k2-logo-arm', 'cubic-bezier(.6, 0, .2, 1)') },
        '& .stem': { animation: run('k2-logo-stem', 'ease') },
        '@keyframes k2-logo-turn': {
          '0%': { transform: 'none' },
          '70%, 100%': { transform: CHECK_TRANSFORM },
        },
        '@keyframes k2-logo-arm': {
          '0%': { strokeDashoffset: 0 },
          '70%, 100%': { strokeDashoffset: SHORT_ARM_OFFSET },
        },
        '@keyframes k2-logo-stem': {
          '0%': { opacity: 1 },
          '45%, 100%': { opacity: 0 },
        },
        '@keyframes k2-logo-pop': {
          '0%, 62%': { transform: 'scale(1)' },
          '80%': { transform: 'scale(1.09)' },
          '100%': { transform: 'scale(1)' },
        },
        // No motion: land on the check mark immediately.
        '@media (prefers-reduced-motion: reduce)': {
          '& .pop, & .turn, & .arm, & .stem': { animationDuration: '1ms' },
        },
      }}
    >
      <clipPath id={clipId}>
        <rect width="512" height="512" rx="114" />
      </clipPath>
      <g className="pop">
        <g clipPath={`url(#${clipId})`}>
          <rect width="512" height="512" fill={TILE} />
          <g className="turn" fill="none" stroke={INK} strokeLinecap="round">
            <path className="stem" d="M164 92V420" strokeWidth="92" />
            <path
              className="arm"
              d="M278 256L574 -40"
              strokeWidth="56"
              strokeDasharray={`${ARM_LENGTH} ${ARM_LENGTH}`}
            />
            <path d="M278 256L574 552" strokeWidth="56" />
          </g>
        </g>
      </g>
    </Box>
  );
}
