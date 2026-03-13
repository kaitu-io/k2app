/**
 * Onboarding visual tokens — single source of truth.
 *
 * Derived from theme.ts / colors.ts:
 *   background.default = #0F0F13
 *   background.paper   = #1A1A1D
 *   primary.main       = #42A5F5 (lightBlue[400])
 *   glow base           = #00d4ff
 */

export const ONBOARDING = {
  // ── Overlay ──
  overlayColor: 'rgba(0,0,0,0.7)',

  // ── StepCard ──
  card: {
    bg: '#1A2332',
    border: '1px solid rgba(0,212,255,0.3)',
    shadow: '0 4px 16px rgba(0,0,0,0.4), 0 12px 40px rgba(0,0,0,0.6)',
    radius: 12,
    padding: '20px 22px',
    maxWidth: 300,
  },

  // ── Typography ──
  // title vs #1A2332: 17:1 (AAA)
  title: { fontSize: 15, fontWeight: 700, color: '#fff' },
  // body vs #1A2332: 12.6:1 (AAA)
  body: {
    fontSize: 13,
    lineHeight: 1.85,
    letterSpacing: '0.03em',
    color: 'rgba(255,255,255,0.85)',
  },
  // hint vs #1A2332: 5.4:1 (AA)
  hint: { fontSize: 12, color: 'rgba(255,255,255,0.5)' },
  // nextButton vs #1A2332: 8.5:1 (AAA)
  nextButton: { fontSize: 13, fontWeight: 600, color: '#4fc3f7' },

  // ── Glow ──
  glow: {
    color: 'rgba(0,212,255,{a})',
    ringWidth: [3, 5] as const,
    spreadRadius: [20, 30] as const,
  },

  // ── Arrow ──
  arrow: {
    size: 20,
    height: 10,
    color: '#4fc3f7',
    bounceDistance: 6,
    duration: '1.2s',
  },

  // ── Popper offset ──
  popperOffset: [0, 18] as const,

  // ── Z-index layers ──
  z: {
    overlay: 1300,
    glow: 1310,
    arrow: 1315,
    card: 1320,
  },
} as const;
