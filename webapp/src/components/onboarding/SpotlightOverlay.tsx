import React from 'react';
import type { TargetRect } from './useTargetRect';
import { ONBOARDING } from './tokens';

interface SpotlightOverlayProps {
  rect: TargetRect;
  padding?: number;
  borderRadius?: number;
}

/**
 * Full-viewport SVG overlay with a rounded-rect cutout (spotlight hole).
 * position:fixed — immune to CSS body zoom.
 * evenodd fill rule: outer rect + inner rect = transparent hole.
 * pointerEvents on path blocks clicks on dark area; cutout allows clicks through.
 */
const SpotlightOverlay: React.FC<SpotlightOverlayProps> = ({
  rect,
  padding = 8,
  borderRadius = 12,
}) => {
  const x = rect.left - padding;
  const y = rect.top - padding;
  const w = rect.width + padding * 2;
  const h = rect.height + padding * 2;
  const r = Math.min(borderRadius, w / 2, h / 2);

  return (
    <svg
      style={{
        position: 'fixed',
        inset: 0,
        width: '100vw',
        height: '100vh',
        zIndex: 1300,
        pointerEvents: 'none',
      }}
    >
      <path
        fillRule="evenodd"
        fill={ONBOARDING.overlayColor}
        pointerEvents="auto"
        d={`M0,0 H${window.innerWidth} V${window.innerHeight} H0 Z M${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${y + h - r} Q${x + w},${y + h} ${x + w - r},${y + h} H${x + r} Q${x},${y + h} ${x},${y + h - r} V${y + r} Q${x},${y} ${x + r},${y} Z`}
      />
    </svg>
  );
};

export default SpotlightOverlay;
