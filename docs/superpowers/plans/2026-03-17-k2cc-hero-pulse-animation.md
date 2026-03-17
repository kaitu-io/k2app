# k2cc Full-Page Pulse Hero Animation — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a full-page Canvas 2D heartbeat animation to the Next.js homepage that progresses from calm ECG to Tesla lightning arcs as the user scrolls, with procedural sound.

**Architecture:** Fixed-position canvas (z-index 0) behind semi-transparent content sections. Pure algorithmic rendering — PQRST waveform, midpoint-displacement lightning, object-pool particles, Web Audio synthesis. Scroll position drives a six-beat energy curve. Zero npm dependencies added.

**Tech Stack:** Next.js 15, React 19, TypeScript, Canvas 2D API, Web Audio API, Tailwind CSS 4

**Spec:** `docs/superpowers/specs/2026-03-17-k2cc-hero-pulse-animation.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `web/src/components/k2cc-hero/types.ts` | Create | Shared interfaces: `Particle`, `BoltSegment`, `EnergyParams`, `GlitchState`, `RenderConfig` |
| `web/src/components/k2cc-hero/constants.ts` | Create | Colors, beat boundaries, k2cc SVG path data, responsive breakpoints, audio params |
| `web/src/components/k2cc-hero/energy.ts` | Create | Six-beat energy curve lookup + parameter interpolation |
| `web/src/components/k2cc-hero/waveform.ts` | Create | PQRST template, Perlin noise, glitch state machine, synthesis pipeline |
| `web/src/components/k2cc-hero/lightning.ts` | Create | Midpoint displacement bolt generation + branching |
| `web/src/components/k2cc-hero/particles.ts` | Create | Object pool, spawn, physics update, render |
| `web/src/components/k2cc-hero/renderer.ts` | Create | Main render loop (tick function), glow, line, arcs, screen shake — React-independent |
| `web/src/components/k2cc-hero/useScrollProgress.ts` | Create | Scroll progress hook with lerp smoothing and direction detection |
| `web/src/components/k2cc-hero/useAudioBurst.ts` | Create | Web Audio procedural sound hook with lazy AudioContext |
| `web/src/components/k2cc-hero/K2ccPulseCanvas.tsx` | Create | React component: canvas element, ResizeObserver, rAF lifecycle, visibility pause |
| `web/src/app/[locale]/HomeClient.tsx` | Modify | Import and render `K2ccPulseCanvas` |
| `web/src/app/[locale]/page.tsx` | Modify | Add `<HomeClient />`, make section backgrounds semi-transparent |

---

## Task 1: Types and Constants

**Files:**
- Create: `web/src/components/k2cc-hero/types.ts`
- Create: `web/src/components/k2cc-hero/constants.ts`

- [ ] **Step 1: Create types.ts**

```typescript
// web/src/components/k2cc-hero/types.ts

export interface Particle {
  x: number
  y: number
  vx: number
  vy: number
  life: number        // 1.0 (born) → 0.0 (dead)
  decay: number       // per-frame decay rate
  size: number        // initial radius px
  brightness: number  // 0-1
  active: boolean
}

export interface BoltSegment {
  x1: number
  y1: number
  x2: number
  y2: number
  lineWidth: number
  brightness: number
  depth: number       // branch depth (0 = trunk)
}

export interface EnergyParams {
  amplitude: number
  frequency: number
  glowRadius: number
  glowIntensity: number
  lineWidth: number
  noiseIntensity: number
  color: string             // hex color for this phase
  colorRgb: [number, number, number]  // parsed for rgba()
  branchCount: number
  arcCount: number
  arcDepth: number
  particleSpawnRate: number
  screenShake: number
  wordmarkOpacity: number
  isBurst: boolean          // true during beat 5
  aftermathSubPhase: number // 0-3: shatter, collapse, dissipate, reset (only valid in beat 6)
  aftermathLocalProgress: number // 0-1 within aftermath phase
}

export interface GlitchState {
  phase: 'idle' | 'active' | 'cooldown'
  startX: number
  width: number
  displacement: number
  framesRemaining: number
  cooldownRemaining: number
}

export interface RenderConfig {
  width: number
  height: number
  dpr: number
  lineY: number           // Y position of pulse line (40vh)
  wordmarkY: number       // Y position of k2cc wordmark (22vh)
  wordmarkScale: number   // scale factor for wordmark path
  visibleCycles: number   // number of PQRST cycles visible on screen
  maxArcCount: number
  maxArcDepth: number
  maxParticles: number
  useRadialGlow: boolean  // false on mobile for perf
}
```

- [ ] **Step 2: Create constants.ts**

```typescript
// web/src/components/k2cc-hero/constants.ts

// --- Colors ---
export const BRAND_GREEN = '#00ff88'
export const BRAND_GREEN_RGB: [number, number, number] = [0, 255, 136]
export const DIM_GREEN = '#005533'
export const DIM_GREEN_RGB: [number, number, number] = [0, 85, 51]
export const WHITE_RGB: [number, number, number] = [255, 255, 255]
export const BG_COLOR = '#050508'
export const BG_GLOW_COLOR = '#0a1a0f'

// --- Beat boundaries (scrollProgress) ---
export const BEAT = {
  REST_END: 0.20,
  SENSE_END: 0.35,
  SILENCE_END: 0.45,
  BUILDUP_END: 0.65,
  BURST_END: 0.80,
  // 0.80 - 1.00 = aftermath → reset
} as const

// --- Beat parameters [rest, sense, silence, buildup, burst, aftermath] ---
export const BEAT_PARAMS = {
  amplitude:      [30,  80,  12,  150, 300, 30],
  frequency:      [0.8, 2.0, 0.4, 3.5, 0,   0.8],
  glowRadius:     [15,  80,  5,   200, 999, 15],   // 999 = fullscreen
  glowIntensity:  [0.03, 0.08, 0.02, 0.12, 0.20, 0.03],
  lineWidth:      [1.5, 2.0, 1.0, 2.5, 0,   1.5], // 0 = no line during burst
  noiseIntensity: [0,   0.3, 0,   0.5, 0,   0],
  branchCount:    [0,   0,   0,   3,   0,   0],
  arcCount:       [0,   0,   0,   0,   8,   0],
  arcDepth:       [0,   0,   0,   0,   7,   0],
  screenShake:    [0,   0,   0,   1,   2,   0],
  wordmarkOpacity:[0,   0,   0,   0,   1,   0],
} as const

// --- Responsive breakpoints ---
export const BREAKPOINTS = {
  mobile: 768,
  tablet: 1024,
} as const

// --- Audio ---
export const AUDIO = {
  masterVolume: 0.12,
  attackDuration: 0.02,       // 20ms
  bodyFreqLow: 60,
  bodyFreqHigh: 120,
  bodyDuration: 0.5,
  crackleMinFreq: 1000,
  crackleMaxFreq: 4000,
  crackleCount: [3, 5] as [number, number],
  subBassFreq: 35,
  subBassDuration: 0.4,
  triggerThreshold: 0.70,
  resetThreshold: 0.30,
} as const

// --- Lightning ---
export const LIGHTNING = {
  decayFactor: 0.55,
  trunkBranchProb: 0.35,
  level1BranchProb: 0.18,
  level2BranchProb: 0.08,
  maxBranchDepth: 3,
  branchAngleMin: 20,   // degrees
  branchAngleMax: 50,
  branchLengthMin: 0.3, // fraction of parent
  branchLengthMax: 0.6,
  regenerateIntervalMs: 100,
} as const

// --- k2cc wordmark SVG path ---
// Extracted from monospace font, normalized to 0-100 coordinate space.
// Will be scaled by renderConfig.wordmarkScale at render time.
// Placeholder — extract real path data during implementation.
export const K2CC_PATH: Array<[number, number][]> = [
  // 'k' character stroke points
  [],
  // '2' character stroke points
  [],
  // first 'c' character stroke points
  [],
  // second 'c' character stroke points
  [],
]

// --- Particle physics ---
export const PARTICLE = {
  gravity: 0.02,
  damping: 0.98,
  buildupLife: [30, 60] as [number, number],
  buildupSpeed: [1, 3] as [number, number],
  buildupSize: [1, 2] as [number, number],
  aftermathLife: [40, 80] as [number, number],
  aftermathSpeed: [2, 5] as [number, number],
  aftermathSize: [2, 3] as [number, number],
} as const

// --- Render ---
export const SCROLL_LERP = 0.08
export const PERF_SAMPLE_FRAMES = 60
export const PERF_THRESHOLD_MS = 20
```

- [ ] **Step 3: Commit**

```bash
cd web && git add src/components/k2cc-hero/types.ts src/components/k2cc-hero/constants.ts
git commit -m "feat(k2cc-hero): add types and constants for pulse animation"
```

---

## Task 2: Energy Curve System

**Files:**
- Create: `web/src/components/k2cc-hero/energy.ts`

- [ ] **Step 1: Implement energy curve lookup and interpolation**

The energy module takes a `scrollProgress` (0-1) and returns fully interpolated `EnergyParams`. It handles:
- Determining which beat we're in based on scroll boundaries
- Lerping between beat start/end parameters using `easeInOutCubic`
- Color interpolation (green → dim green → green → white → green)
- Special flags (isBurst)

```typescript
// web/src/components/k2cc-hero/energy.ts
import { BEAT, BEAT_PARAMS, BRAND_GREEN, BRAND_GREEN_RGB, DIM_GREEN_RGB, WHITE_RGB } from './constants'
import type { EnergyParams } from './types'

function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2
}

function lerpNum(a: number, b: number, t: number): number {
  return a + (b - a) * t
}

function lerpColor(a: [number, number, number], b: [number, number, number], t: number): [number, number, number] {
  return [
    Math.round(lerpNum(a[0], b[0], t)),
    Math.round(lerpNum(a[1], b[1], t)),
    Math.round(lerpNum(a[2], b[2], t)),
  ]
}

function rgbToHex(rgb: [number, number, number]): string {
  return '#' + rgb.map(c => c.toString(16).padStart(2, '0')).join('')
}

/** Beat index: 0=rest, 1=sense, 2=silence, 3=buildup, 4=burst, 5=aftermath */
function getBeatAndProgress(sp: number): [number, number] {
  const boundaries = [0, BEAT.REST_END, BEAT.SENSE_END, BEAT.SILENCE_END, BEAT.BUILDUP_END, BEAT.BURST_END, 1.0]
  for (let i = 0; i < 6; i++) {
    if (sp <= boundaries[i + 1]) {
      const range = boundaries[i + 1] - boundaries[i]
      const local = range > 0 ? (sp - boundaries[i]) / range : 0
      return [i, local]
    }
  }
  return [5, 1.0]
}

// Color keyframes per beat: [startColor, endColor]
const COLOR_KEYFRAMES: Array<[[number, number, number], [number, number, number]]> = [
  [BRAND_GREEN_RGB, BRAND_GREEN_RGB],       // rest: solid green
  [BRAND_GREEN_RGB, BRAND_GREEN_RGB],       // sense: solid green
  [BRAND_GREEN_RGB, DIM_GREEN_RGB],         // silence: green → dim
  [DIM_GREEN_RGB, WHITE_RGB],               // buildup: dim → white
  [WHITE_RGB, WHITE_RGB],                   // burst: solid white
  [WHITE_RGB, BRAND_GREEN_RGB],             // aftermath: white → green
]

export function getEnergyParams(scrollProgress: number): EnergyParams {
  const sp = Math.max(0, Math.min(1, scrollProgress))
  const [beat, localProgress] = getBeatAndProgress(sp)
  const t = easeInOutCubic(localProgress)

  // Interpolate from current beat's value toward next beat's value across the
  // full beat range. This ensures smooth transitions with no hard jumps at
  // beat boundaries (spec: "no hard cuts").
  const nextBeat = Math.min(beat + 1, 5)

  function param(key: keyof typeof BEAT_PARAMS): number {
    const current = BEAT_PARAMS[key][beat]
    const next = BEAT_PARAMS[key][nextBeat]
    return lerpNum(current, next, t)
  }

  const [colorStart, colorEnd] = COLOR_KEYFRAMES[beat]
  const colorRgb = lerpColor(colorStart, colorEnd, t)

  // Burst-phase wordmark progressive light-up: 0.65→0.75 ramps 0→1
  let wordmarkOpacity = 0
  if (beat === 4) {
    // Within burst (0.65-0.80), light up from 0→1 in first 2/3 of phase
    wordmarkOpacity = Math.min(1, localProgress / 0.67)
  } else if (beat === 5) {
    // Aftermath: stays lit until 0.93 (sub-phase localProgress ~0.65), then fades
    const fadeStart = 0.65 // maps to scrollProgress ~0.93
    const fadeEnd = 0.80   // maps to scrollProgress ~0.96
    if (localProgress < fadeStart) {
      wordmarkOpacity = 1
    } else if (localProgress < fadeEnd) {
      wordmarkOpacity = 1 - (localProgress - fadeStart) / (fadeEnd - fadeStart)
    } else {
      wordmarkOpacity = 0
    }
  }

  // Aftermath sub-phase tracking (spec: shatter/collapse/dissipate/reset)
  let aftermathSubPhase = 0
  let aftermathLocalProgress = 0
  if (beat === 5) {
    // 0.80-0.88 = shatter (localProgress 0-0.40)
    // 0.88-0.93 = collapse (localProgress 0.40-0.65)
    // 0.93-0.96 = dissipate (localProgress 0.65-0.80)
    // 0.96-1.00 = reset (localProgress 0.80-1.00)
    if (localProgress < 0.40) {
      aftermathSubPhase = 0
      aftermathLocalProgress = localProgress / 0.40
    } else if (localProgress < 0.65) {
      aftermathSubPhase = 1
      aftermathLocalProgress = (localProgress - 0.40) / 0.25
    } else if (localProgress < 0.80) {
      aftermathSubPhase = 2
      aftermathLocalProgress = (localProgress - 0.65) / 0.15
    } else {
      aftermathSubPhase = 3
      aftermathLocalProgress = (localProgress - 0.80) / 0.20
    }
  }

  return {
    amplitude: param('amplitude'),
    frequency: param('frequency'),
    glowRadius: param('glowRadius'),
    glowIntensity: param('glowIntensity'),
    lineWidth: param('lineWidth'),
    noiseIntensity: param('noiseIntensity'),
    color: rgbToHex(colorRgb),
    colorRgb,
    branchCount: Math.round(param('branchCount')),
    arcCount: Math.round(param('arcCount')),
    arcDepth: Math.round(param('arcDepth')),
    particleSpawnRate: param('branchCount') > 0 ? 0.3 : 0,
    screenShake: param('screenShake'),
    wordmarkOpacity,
    isBurst: beat === 4,
    aftermathSubPhase,
    aftermathLocalProgress,
  }
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/energy.ts
git commit -m "feat(k2cc-hero): six-beat energy curve with parameter interpolation"
```

---

## Task 3: Waveform Generator

**Files:**
- Create: `web/src/components/k2cc-hero/waveform.ts`

- [ ] **Step 1: Implement PQRST template + Perlin noise + glitch**

```typescript
// web/src/components/k2cc-hero/waveform.ts
import type { GlitchState } from './types'

// --- Gaussian helper ---
function gaussian(x: number, mean: number, sigma: number): number {
  const d = x - mean
  return Math.exp(-(d * d) / (2 * sigma * sigma))
}

// --- PQRST template (normalized t ∈ [0, 1]) ---
export function pqrst(t: number): number {
  t = ((t % 1) + 1) % 1 // wrap to [0, 1)

  if (t < 0.12) {
    // P wave
    return 0.15 * Math.sin(Math.PI * t / 0.12)
  } else if (t < 0.20) {
    // PQ segment
    return 0
  } else if (t < 0.36) {
    // QRS complex: Q dip + R spike + S dip
    return (
      -0.1 * gaussian(t, 0.22, 0.01) +
       1.2 * gaussian(t, 0.28, 0.015) +
      -0.15 * gaussian(t, 0.33, 0.01)
    )
  } else if (t < 0.50) {
    // ST segment
    return 0
  } else if (t < 0.68) {
    // T wave
    return 0.3 * Math.sin(Math.PI * (t - 0.50) / 0.18)
  } else {
    // Baseline
    return 0
  }
}

// --- Simplified 1D Perlin noise ---
// Hash-based with cosine interpolation, no library needed.
const PERLIN_SIZE = 256
const perlinTable: number[] = []
for (let i = 0; i < PERLIN_SIZE; i++) {
  perlinTable[i] = Math.random() * 2 - 1
}

function perlinHash(i: number): number {
  return perlinTable[((i % PERLIN_SIZE) + PERLIN_SIZE) % PERLIN_SIZE]
}

export function perlinNoise1D(x: number): number {
  const xi = Math.floor(x)
  const xf = x - xi
  const u = xf * xf * (3 - 2 * xf) // smoothstep
  return perlinHash(xi) * (1 - u) + perlinHash(xi + 1) * u
}

// --- Glitch state machine ---
export function createGlitchState(): GlitchState {
  return {
    phase: 'idle',
    startX: 0,
    width: 0,
    displacement: 0,
    framesRemaining: 0,
    cooldownRemaining: 0,
  }
}

export function updateGlitch(state: GlitchState, canTrigger: boolean, viewportWidth: number): GlitchState {
  switch (state.phase) {
    case 'idle':
      if (canTrigger && Math.random() < 0.003) {
        return {
          phase: 'active',
          startX: Math.random() * viewportWidth * 0.8,
          width: 80 + Math.random() * 120,
          displacement: (Math.random() > 0.5 ? 1 : -1) * (5 + Math.random() * 15),
          framesRemaining: 2 + Math.floor(Math.random() * 2),
          cooldownRemaining: 0,
        }
      }
      return state
    case 'active':
      if (state.framesRemaining <= 0) {
        return { ...state, phase: 'cooldown', cooldownRemaining: 60 + Math.floor(Math.random() * 60) }
      }
      return { ...state, framesRemaining: state.framesRemaining - 1 }
    case 'cooldown':
      if (state.cooldownRemaining <= 0) {
        return { ...state, phase: 'idle' }
      }
      return { ...state, cooldownRemaining: state.cooldownRemaining - 1 }
    default:
      return state
  }
}

// --- Full waveform synthesis ---
export function waveform(
  x: number,
  time: number,
  amplitude: number,
  frequency: number,
  noiseIntensity: number,
  wavelength: number,
  glitch: GlitchState,
): number {
  // Apply glitch displacement
  let effectiveX = x
  if (glitch.phase === 'active' && x >= glitch.startX && x <= glitch.startX + glitch.width) {
    effectiveX += glitch.displacement
  }

  const phase = ((effectiveX / wavelength + time * frequency) % 1 + 1) % 1
  let y = pqrst(phase) * amplitude

  // Add Perlin noise
  if (noiseIntensity > 0) {
    y += perlinNoise1D(x * 0.01 + time * 2.0) * noiseIntensity * amplitude * 0.3
  }

  return y
}

// --- R-peak detection for glow and micro-swell ---
export function isRPeak(phase: number): boolean {
  return phase > 0.26 && phase < 0.30
}

export function rPeakSwell(phase: number, baseWidth: number): number {
  if (phase < 0.26 || phase > 0.30) return baseWidth
  // smoothstep up from 0.26 to 0.28, then down from 0.28 to 0.30
  const up = smoothstep(0.26, 0.28, phase)
  const down = smoothstep(0.30, 0.28, phase)
  return baseWidth + 1.0 * up * down
}

function smoothstep(edge0: number, edge1: number, x: number): number {
  const t = Math.max(0, Math.min(1, (x - edge0) / (edge1 - edge0)))
  return t * t * (3 - 2 * t)
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/waveform.ts
git commit -m "feat(k2cc-hero): PQRST waveform generator with Perlin noise and glitch system"
```

---

## Task 4: Lightning Arc Generator

**Files:**
- Create: `web/src/components/k2cc-hero/lightning.ts`

- [ ] **Step 1: Implement midpoint displacement + branching**

```typescript
// web/src/components/k2cc-hero/lightning.ts
import { LIGHTNING } from './constants'
import type { BoltSegment } from './types'

interface Point {
  x: number
  y: number
}

function midpoint(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
}

function distance(a: Point, b: Point): number {
  const dx = b.x - a.x
  const dy = b.y - a.y
  return Math.sqrt(dx * dx + dy * dy)
}

function angle(a: Point, b: Point): number {
  return Math.atan2(b.y - a.y, b.x - a.x)
}

/**
 * Generate a single lightning bolt path using midpoint displacement.
 * Returns array of line segments with varying lineWidth and brightness.
 */
export function generateBolt(
  start: Point,
  end: Point,
  depth: number,
  displacement: number,
  lineWidth: number,
  brightness: number,
  branchDepth: number,
  segments: BoltSegment[],
): void {
  if (depth <= 0) {
    segments.push({
      x1: start.x, y1: start.y,
      x2: end.x, y2: end.y,
      lineWidth,
      brightness,
      depth: branchDepth,
    })
    return
  }

  const mid = midpoint(start, end)
  mid.x += (Math.random() * 2 - 1) * displacement
  mid.y += (Math.random() * 2 - 1) * displacement

  generateBolt(start, mid, depth - 1, displacement * LIGHTNING.decayFactor, lineWidth, brightness, branchDepth, segments)
  generateBolt(mid, end, depth - 1, displacement * LIGHTNING.decayFactor, lineWidth, brightness, branchDepth, segments)

  // Branching
  if (branchDepth < LIGHTNING.maxBranchDepth) {
    const prob = branchDepth === 0
      ? LIGHTNING.trunkBranchProb
      : branchDepth === 1
        ? LIGHTNING.level1BranchProb
        : LIGHTNING.level2BranchProb

    if (Math.random() < prob) {
      const dir = angle(start, end)
      const branchAngle = dir + (Math.random() > 0.5 ? 1 : -1) *
        ((LIGHTNING.branchAngleMin + Math.random() * (LIGHTNING.branchAngleMax - LIGHTNING.branchAngleMin)) * Math.PI / 180)
      const len = distance(start, end) *
        (LIGHTNING.branchLengthMin + Math.random() * (LIGHTNING.branchLengthMax - LIGHTNING.branchLengthMin))

      const branchEnd: Point = {
        x: mid.x + Math.cos(branchAngle) * len,
        y: mid.y + Math.sin(branchAngle) * len,
      }

      generateBolt(
        mid,
        branchEnd,
        Math.max(depth - 2, 1),
        displacement * 0.5,
        lineWidth * 0.6,
        brightness * 0.7,
        branchDepth + 1,
        segments,
      )
    }
  }
}

/**
 * Generate all arcs for the burst phase.
 * Origins are spread along the pulse line, targets are on the k2cc wordmark.
 */
export function generateAllArcs(
  lineY: number,
  wordmarkY: number,
  viewportWidth: number,
  arcCount: number,
  arcDepth: number,
  wordmarkTargets: Point[],
): BoltSegment[] {
  const segments: BoltSegment[] = []
  const originXStart = viewportWidth * 0.3
  const originXEnd = viewportWidth * 0.7
  const step = arcCount > 1 ? (originXEnd - originXStart) / (arcCount - 1) : 0

  for (let i = 0; i < arcCount; i++) {
    const originX = originXStart + step * i + (Math.random() - 0.5) * 20
    const origin: Point = { x: originX, y: lineY }
    const target = wordmarkTargets[i % wordmarkTargets.length]
    const dist = distance(origin, target)
    const displacement = 20 + Math.random() * 40

    generateBolt(origin, target, arcDepth, displacement, 2.5, 1.0, 0, segments)
  }

  return segments
}

/**
 * Get sample points along the k2cc wordmark for arc targets.
 * Takes the raw path data and scales/positions it.
 */
export function getWordmarkTargets(
  pathData: Array<[number, number][]>,
  centerX: number,
  centerY: number,
  scale: number,
  count: number,
): Point[] {
  // Flatten all path points, scale, and evenly sample
  const allPoints: Point[] = []
  const totalWidth = 100 * scale // path is in 0-100 space
  const startX = centerX - totalWidth / 2

  for (const charPath of pathData) {
    for (const [px, py] of charPath) {
      allPoints.push({
        x: startX + px * scale,
        y: centerY + (py - 50) * scale, // center vertically
      })
    }
  }

  if (allPoints.length === 0) {
    // Fallback: evenly spaced points in wordmark area
    const targets: Point[] = []
    for (let i = 0; i < count; i++) {
      targets.push({
        x: centerX - totalWidth / 2 + (totalWidth * i) / (count - 1),
        y: centerY,
      })
    }
    return targets
  }

  // Evenly sample from available points
  const targets: Point[] = []
  const step = Math.max(1, Math.floor(allPoints.length / count))
  for (let i = 0; i < count && i * step < allPoints.length; i++) {
    targets.push(allPoints[i * step])
  }
  return targets
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/lightning.ts
git commit -m "feat(k2cc-hero): midpoint displacement lightning arc generator with branching"
```

---

## Task 5: Particle System

**Files:**
- Create: `web/src/components/k2cc-hero/particles.ts`

- [ ] **Step 1: Implement object pool + physics**

```typescript
// web/src/components/k2cc-hero/particles.ts
import { PARTICLE } from './constants'
import type { Particle } from './types'

export class ParticlePool {
  particles: Particle[]
  activeCount: number

  constructor(maxSize: number) {
    this.particles = Array.from({ length: maxSize }, () => ({
      x: 0, y: 0, vx: 0, vy: 0,
      life: 0, decay: 0, size: 0, brightness: 0, active: false,
    }))
    this.activeCount = 0
  }

  spawn(x: number, y: number, vx: number, vy: number, size: number, life: number, brightness: number): void {
    // Find inactive slot
    for (const p of this.particles) {
      if (!p.active) {
        p.x = x; p.y = y
        p.vx = vx; p.vy = vy
        p.size = size
        p.life = 1.0
        p.decay = 1.0 / life  // die after `life` frames
        p.brightness = brightness
        p.active = true
        this.activeCount++
        return
      }
    }
    // Pool full — skip
  }

  spawnBuildup(x: number, y: number, dirAngle: number): void {
    const count = 2 + Math.floor(Math.random() * 3)
    for (let i = 0; i < count; i++) {
      const angle = dirAngle + (Math.random() - 0.5) * (Math.PI / 3) // ±30°
      const speed = PARTICLE.buildupSpeed[0] + Math.random() * (PARTICLE.buildupSpeed[1] - PARTICLE.buildupSpeed[0])
      const size = PARTICLE.buildupSize[0] + Math.random() * (PARTICLE.buildupSize[1] - PARTICLE.buildupSize[0])
      const life = PARTICLE.buildupLife[0] + Math.random() * (PARTICLE.buildupLife[1] - PARTICLE.buildupLife[0])
      this.spawn(x, y, Math.cos(angle) * speed, Math.sin(angle) * speed, size, life, 0.8)
    }
  }

  spawnAftermath(x: number, y: number): void {
    const count = 5 + Math.floor(Math.random() * 4)
    for (let i = 0; i < count; i++) {
      const angle = Math.random() * Math.PI * 2
      const speed = PARTICLE.aftermathSpeed[0] + Math.random() * (PARTICLE.aftermathSpeed[1] - PARTICLE.aftermathSpeed[0])
      const size = PARTICLE.aftermathSize[0] + Math.random() * (PARTICLE.aftermathSize[1] - PARTICLE.aftermathSize[0])
      const life = PARTICLE.aftermathLife[0] + Math.random() * (PARTICLE.aftermathLife[1] - PARTICLE.aftermathLife[0])
      this.spawn(x, y, Math.cos(angle) * speed, Math.sin(angle) * speed, size, life, 1.0)
    }
  }

  update(): void {
    for (const p of this.particles) {
      if (!p.active) continue

      p.x += p.vx
      p.y += p.vy
      p.vy += PARTICLE.gravity
      p.vx *= PARTICLE.damping
      p.vy *= PARTICLE.damping
      p.life -= p.decay

      if (p.life <= 0) {
        p.active = false
        this.activeCount--
      }
    }
  }

  render(ctx: CanvasRenderingContext2D, colorRgb: [number, number, number]): void {
    ctx.globalCompositeOperation = 'screen'
    for (const p of this.particles) {
      if (!p.active) continue
      const radius = p.size * p.life
      if (radius < 0.1) continue

      ctx.beginPath()
      ctx.arc(p.x, p.y, radius, 0, Math.PI * 2)
      ctx.fillStyle = `rgba(${colorRgb[0]},${colorRgb[1]},${colorRgb[2]},${p.life * p.brightness})`
      ctx.fill()
    }
    ctx.globalCompositeOperation = 'source-over'
  }

  clear(): void {
    for (const p of this.particles) {
      p.active = false
    }
    this.activeCount = 0
  }
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/particles.ts
git commit -m "feat(k2cc-hero): particle object pool with buildup and aftermath spawning"
```

---

## Task 6: k2cc Wordmark Path Data

**Files:**
- Modify: `web/src/components/k2cc-hero/constants.ts`

Must be done before the renderer so arc targets are available during development.

- [ ] **Step 1: Generate k2cc SVG path data**

Replace the empty `K2CC_PATH` arrays in `constants.ts` with actual character stroke points. Use simplified geometric strokes (not full glyph outlines) — these are lightning arc targets.

Approach: Create clean geometric strokes in a normalized 0-100 coordinate space. Characters spaced evenly:
- 'k': x [0, 22], vertical stroke + two diagonal strokes
- '2': x [26, 48], curved top + diagonal + horizontal base
- 'c': x [52, 70], open arc
- 'c': x [74, 92], open arc

Each character defined as an array of `[x, y]` points tracing the stroke path. Y range 20-80 (vertically centered at 50).

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/constants.ts
git commit -m "feat(k2cc-hero): add k2cc wordmark path data for lightning targets"
```

---

## Task 7: Main Renderer

**Files:**
- Create: `web/src/components/k2cc-hero/renderer.ts`

This is the centerpiece. It orchestrates the per-frame render pipeline.

- [ ] **Step 1: Implement the renderer**

```typescript
// web/src/components/k2cc-hero/renderer.ts
import { getEnergyParams } from './energy'
import { waveform, createGlitchState, updateGlitch, isRPeak, rPeakSwell } from './waveform'
import { generateAllArcs, getWordmarkTargets } from './lightning'
import { ParticlePool } from './particles'
import { K2CC_PATH, LIGHTNING, BRAND_GREEN_RGB, PERF_SAMPLE_FRAMES, PERF_THRESHOLD_MS } from './constants'
import type { GlitchState, BoltSegment, RenderConfig, EnergyParams } from './types'

export class PulseRenderer {
  private ctx: CanvasRenderingContext2D
  private config: RenderConfig

  // Mutable state
  private glitch: GlitchState = createGlitchState()
  private particles: ParticlePool
  private lastArcGenTime = 0
  private currentArcs: BoltSegment[] = []
  private flashIntensity = 0
  private startTime = 0

  // Performance auto-degradation
  private perfSamples: number[] = []
  private degraded = false

  // Aftermath shatter state
  private shatterPoints: Array<{ x: number; y: number; spawned: boolean }> = []

  constructor(ctx: CanvasRenderingContext2D, config: RenderConfig) {
    this.ctx = ctx
    this.config = config
    this.particles = new ParticlePool(config.maxParticles)
  }

  updateConfig(config: RenderConfig): void {
    this.config = config
    // Rebuild particle pool if size changed
    if (config.maxParticles !== this.particles.particles.length) {
      this.particles = new ParticlePool(config.maxParticles)
    }
  }

  tick(timestamp: number, scrollProgress: number): void {
    if (this.startTime === 0) this.startTime = timestamp
    const time = (timestamp - this.startTime) / 1000 // seconds
    const params = getEnergyParams(scrollProgress)
    const { ctx, config } = this
    const { width, height } = config

    // --- Performance monitoring (first 60 frames) ---
    if (this.perfSamples.length < PERF_SAMPLE_FRAMES) {
      this.perfSamples.push(timestamp)
      if (this.perfSamples.length === PERF_SAMPLE_FRAMES) {
        // Compute average frame time from consecutive timestamp deltas
        let totalDelta = 0
        for (let i = 1; i < this.perfSamples.length; i++) {
          totalDelta += this.perfSamples[i] - this.perfSamples[i - 1]
        }
        const avg = totalDelta / (this.perfSamples.length - 1)
        if (avg > PERF_THRESHOLD_MS) {
          this.degraded = true
          this.config = {
            ...this.config,
            maxArcCount: Math.max(2, Math.floor(this.config.maxArcCount / 2)),
            maxArcDepth: Math.max(3, this.config.maxArcDepth - 2),
            maxParticles: Math.floor(this.config.maxParticles / 2),
            useRadialGlow: false,
          }
          this.particles = new ParticlePool(this.config.maxParticles)
        }
      }
    }

    // --- Step 3: Clear at identity transform ---
    ctx.save()
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height)
    ctx.restore()

    // --- Step 4: Screen shake ---
    const shake = params.screenShake
    const shakeX = shake > 0 ? (Math.random() - 0.5) * 2 * shake : 0
    const shakeY = shake > 0 ? (Math.random() - 0.5) * 2 * shake : 0
    ctx.save()
    if (shake > 0) ctx.translate(shakeX, shakeY)

    // --- Step 5: Glow layer ---
    this.renderGlow(params, time)

    // --- Step 6: Main pulse line (skip during burst) ---
    if (!params.isBurst && params.lineWidth > 0) {
      this.renderPulseLine(params, time)
    }

    // --- Step 7: Branch lines (buildup) ---
    if (params.branchCount > 0) {
      this.renderBranches(params, time)
    }

    // --- Step 8: Lightning arcs (burst) ---
    if (params.isBurst) {
      this.renderArcs(params, timestamp)
    }

    // --- Step 9: Particles ---
    this.particles.update()
    this.particles.render(ctx, params.colorRgb)

    // --- Aftermath sub-phases ---
    if (params.aftermathSubPhase >= 0 && !params.isBurst && scrollProgress > 0.80) {
      this.handleAftermath(params)
    }

    // --- Step 10: Wordmark ---
    if (params.wordmarkOpacity > 0) {
      this.renderWordmark(params)
    }

    // --- Step 11: Restore (undo shake) ---
    ctx.restore()

    // --- Flash decay ---
    if (this.flashIntensity > 0) {
      this.flashIntensity = Math.max(0, this.flashIntensity - 0.05) // ~3 frames to decay
    }

    // --- Glitch update ---
    const canGlitch = scrollProgress >= 0.20 && scrollProgress <= 0.65
    this.glitch = updateGlitch(this.glitch, canGlitch, width)
  }

  // ========== PRIVATE RENDER METHODS ==========

  private renderGlow(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, height, lineY } = config
    const [r, g, b] = params.colorRgb

    ctx.globalCompositeOperation = 'screen'

    if (params.isBurst) {
      // Full-screen flash on arc regeneration
      if (this.flashIntensity > 0) {
        ctx.fillStyle = `rgba(${r},${g},${b},${this.flashIntensity})`
        ctx.fillRect(0, 0, width, height)
      }
    } else if (config.useRadialGlow && params.glowRadius > 5) {
      // Per-peak radial gradient glow
      const wavelength = width / config.visibleCycles
      const freq = params.frequency || 0.8
      for (let cycle = 0; cycle < config.visibleCycles + 1; cycle++) {
        // R-peak is at phase 0.28 within each cycle
        const peakX = (0.28 + cycle) * wavelength - ((time * freq * wavelength) % wavelength)
        if (peakX < -params.glowRadius || peakX > width + params.glowRadius) continue

        const gradient = ctx.createRadialGradient(peakX, lineY, 0, peakX, lineY, params.glowRadius)
        gradient.addColorStop(0, `rgba(${r},${g},${b},${params.glowIntensity})`)
        gradient.addColorStop(0.4, `rgba(${r},${g},${b},${params.glowIntensity * 0.3})`)
        gradient.addColorStop(1, `rgba(${r},${g},${b},0)`)
        ctx.fillStyle = gradient
        ctx.fillRect(
          peakX - params.glowRadius, lineY - params.glowRadius,
          params.glowRadius * 2, params.glowRadius * 2,
        )
      }
    } else if (params.glowIntensity > 0.01) {
      // Simplified glow (mobile): single rect at lineY
      ctx.fillStyle = `rgba(${r},${g},${b},${params.glowIntensity * 0.5})`
      const h = params.glowRadius * 2
      ctx.fillRect(0, lineY - h / 2, width, h)
    }

    ctx.globalCompositeOperation = 'source-over'
  }

  private renderPulseLine(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, lineY, visibleCycles } = config
    const wavelength = width / visibleCycles

    ctx.beginPath()
    ctx.strokeStyle = params.color
    ctx.lineWidth = params.lineWidth
    ctx.shadowColor = params.color
    ctx.shadowBlur = 4

    let prevPhase = 0
    for (let x = 0; x <= width; x++) {
      const y = lineY + waveform(
        x, time, params.amplitude, params.frequency,
        params.noiseIntensity, wavelength, this.glitch,
      )

      if (x === 0) {
        ctx.moveTo(x, y)
      } else {
        ctx.lineTo(x, y)
      }

      // R-peak micro-swell: vary lineWidth at R peak
      const phase = ((x / wavelength + time * params.frequency) % 1 + 1) % 1
      if (isRPeak(phase) && !isRPeak(prevPhase)) {
        // Flush current path and start new segment with wider line
        ctx.stroke()
        ctx.beginPath()
        ctx.moveTo(x, y)
        ctx.lineWidth = rPeakSwell(phase, params.lineWidth)
      } else if (!isRPeak(phase) && isRPeak(prevPhase)) {
        // Return to normal width
        ctx.stroke()
        ctx.beginPath()
        ctx.moveTo(x, y)
        ctx.lineWidth = params.lineWidth
      }
      prevPhase = phase
    }
    ctx.stroke()
    ctx.shadowBlur = 0
  }

  private renderBranches(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, lineY, visibleCycles } = config
    const wavelength = width / visibleCycles

    // Find wave peaks (R-peak locations) and spawn branches from them
    for (let cycle = 0; cycle < visibleCycles + 1; cycle++) {
      const peakX = (0.28 + cycle) * wavelength - ((time * params.frequency * wavelength) % wavelength)
      if (peakX < 0 || peakX > width) continue

      const peakY = lineY + waveform(
        peakX, time, params.amplitude, params.frequency,
        params.noiseIntensity, wavelength, this.glitch,
      )

      // Draw 1-2 short branch lines from this peak
      for (let b = 0; b < Math.min(2, params.branchCount); b++) {
        const angle = -Math.PI / 2 + (Math.random() - 0.5) * Math.PI * 0.8 // mostly upward
        const len = 20 + Math.random() * 40
        const endX = peakX + Math.cos(angle) * len
        const endY = peakY + Math.sin(angle) * len

        ctx.beginPath()
        ctx.strokeStyle = `rgba(${params.colorRgb[0]},${params.colorRgb[1]},${params.colorRgb[2]},0.4)`
        ctx.lineWidth = 0.5
        ctx.moveTo(peakX, peakY)
        ctx.lineTo(endX, endY)
        ctx.stroke()

        // Spawn particles at branch endpoint
        if (Math.random() < params.particleSpawnRate) {
          this.particles.spawnBuildup(endX, endY, angle)
        }
      }
    }
  }

  private renderArcs(params: EnergyParams, timestamp: number): void {
    const { ctx, config } = this

    // Regenerate arcs periodically
    if (timestamp - this.lastArcGenTime > LIGHTNING.regenerateIntervalMs) {
      this.lastArcGenTime = timestamp

      const targets = getWordmarkTargets(
        K2CC_PATH,
        config.width / 2,
        config.wordmarkY,
        config.wordmarkScale,
        config.maxArcCount,
      )

      this.currentArcs = generateAllArcs(
        config.lineY,
        config.wordmarkY,
        config.width,
        Math.min(params.arcCount, config.maxArcCount),
        Math.min(params.arcDepth, config.maxArcDepth),
        targets,
      )

      // Trigger flash
      this.flashIntensity = 0.05 + Math.random() * 0.10
    }

    // Render current arcs with per-frame opacity jitter
    for (const seg of this.currentArcs) {
      const opacity = seg.brightness * (0.7 + Math.random() * 0.3)
      ctx.beginPath()
      ctx.strokeStyle = `rgba(255,255,255,${opacity})`
      ctx.lineWidth = seg.lineWidth
      ctx.shadowColor = '#00ff88'
      ctx.shadowBlur = seg.depth === 0 ? 8 : 4
      ctx.moveTo(seg.x1, seg.y1)
      ctx.lineTo(seg.x2, seg.y2)
      ctx.stroke()
    }
    ctx.shadowBlur = 0
  }

  private handleAftermath(params: EnergyParams): void {
    const { aftermathSubPhase, aftermathLocalProgress } = params

    if (aftermathSubPhase === 0 && this.currentArcs.length > 0) {
      // Shatter: break arcs into segments, spawn shatter points
      if (this.shatterPoints.length === 0) {
        // Pick 3-5 random arc segment positions as shatter points
        const count = 3 + Math.floor(Math.random() * 3)
        for (let i = 0; i < count && i < this.currentArcs.length; i++) {
          const seg = this.currentArcs[Math.floor(Math.random() * this.currentArcs.length)]
          this.shatterPoints.push({
            x: (seg.x1 + seg.x2) / 2,
            y: (seg.y1 + seg.y2) / 2,
            spawned: false,
          })
        }
      }
      // Render remaining arc fragments with decreasing opacity
      const opacity = 1 - aftermathLocalProgress
      for (const seg of this.currentArcs) {
        this.ctx.beginPath()
        this.ctx.strokeStyle = `rgba(255,255,255,${opacity * seg.brightness * 0.5})`
        this.ctx.lineWidth = seg.lineWidth * 0.5
        this.ctx.moveTo(seg.x1, seg.y1)
        this.ctx.lineTo(seg.x2, seg.y2)
        this.ctx.stroke()
      }
    } else if (aftermathSubPhase === 1) {
      // Collapse: shatter points spawn particles
      for (const sp of this.shatterPoints) {
        if (!sp.spawned) {
          this.particles.spawnAftermath(sp.x, sp.y)
          sp.spawned = true
        }
      }
      // Clear arcs
      this.currentArcs = []
    } else if (aftermathSubPhase >= 2) {
      // Dissipate + reset: particles continue under physics (handled by pool.update)
      this.currentArcs = []
      this.shatterPoints = []
    }
  }

  private renderWordmark(params: EnergyParams): void {
    const { ctx, config } = this
    const [r, g, b] = params.colorRgb
    const opacity = params.wordmarkOpacity

    // Render k2cc text using Canvas text API as a simple fallback
    // (Path data rendering is used when K2CC_PATH has real data)
    ctx.save()
    ctx.globalAlpha = opacity
    ctx.font = `bold ${config.wordmarkScale * 80}px monospace`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'

    // Glow
    ctx.shadowColor = `rgba(0,255,136,${opacity})`
    ctx.shadowBlur = 6
    ctx.strokeStyle = `rgba(${r},${g},${b},${opacity})`
    ctx.lineWidth = 2
    ctx.strokeText('k2cc', config.width / 2, config.wordmarkY)

    // Fill
    ctx.fillStyle = `rgba(255,255,255,${opacity})`
    ctx.fillText('k2cc', config.width / 2, config.wordmarkY)

    ctx.shadowBlur = 0
    ctx.globalAlpha = 1
    ctx.restore()
  }
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/renderer.ts
git commit -m "feat(k2cc-hero): full render pipeline with glow, pulse, arcs, aftermath, and wordmark"
```

---

## Task 8: Scroll Progress Hook

**Files:**
- Create: `web/src/components/k2cc-hero/useScrollProgress.ts`

- [ ] **Step 1: Implement the hook**

```typescript
// web/src/components/k2cc-hero/useScrollProgress.ts
import { useRef, useCallback } from 'react'
import { SCROLL_LERP } from './constants'

interface ScrollState {
  current: number    // smoothed 0-1
  raw: number        // unsmoothed 0-1
  direction: number  // 1 = scrolling down, -1 = scrolling up
  prevRaw: number
}

/**
 * Returns a ref-based scroll progress tracker.
 * Call getProgress() inside rAF to get lerp-smoothed value.
 * Does NOT use useState — avoids React re-renders on every frame.
 */
export function useScrollProgress() {
  const state = useRef<ScrollState>({
    current: 0,
    raw: 0,
    direction: 1,
    prevRaw: 0,
  })

  const getProgress = useCallback((): ScrollState => {
    const s = state.current
    // Poll scrollY directly (works during iOS momentum scroll)
    const scrollHeight = document.documentElement.scrollHeight
    const viewportHeight = window.innerHeight
    const maxScroll = scrollHeight - viewportHeight
    s.raw = maxScroll > 0 ? Math.max(0, Math.min(1, window.scrollY / maxScroll)) : 0

    // Direction
    s.direction = s.raw >= s.prevRaw ? 1 : -1
    s.prevRaw = s.raw

    // Lerp smooth
    s.current += (s.raw - s.current) * SCROLL_LERP

    return s
  }, [])

  return { getProgress }
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/useScrollProgress.ts
git commit -m "feat(k2cc-hero): scroll progress hook with lerp smoothing"
```

---

## Task 9: Audio Burst Hook

**Files:**
- Create: `web/src/components/k2cc-hero/useAudioBurst.ts`

- [ ] **Step 1: Implement Web Audio procedural sound**

```typescript
// web/src/components/k2cc-hero/useAudioBurst.ts
import { useRef, useCallback } from 'react'
import { AUDIO } from './constants'

/**
 * Procedural burst sound using Web Audio API.
 * AudioContext created lazily on first user interaction.
 * Every trigger generates slightly different sound via randomization.
 */
export function useAudioBurst() {
  const ctxRef = useRef<AudioContext | null>(null)
  const hasPlayedRef = useRef(false)
  const activatedRef = useRef(false)

  // Lazy init — call on first click/scroll/touch
  const ensureContext = useCallback(() => {
    if (ctxRef.current || activatedRef.current) return
    if (typeof window === 'undefined') return
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return

    activatedRef.current = true
    try {
      ctxRef.current = new AudioContext()
    } catch {
      // Web Audio not available
    }
  }, [])

  const play = useCallback(() => {
    const ctx = ctxRef.current
    if (!ctx) return
    if (ctx.state === 'suspended') {
      ctx.resume()
    }

    const now = ctx.currentTime
    const master = ctx.createGain()
    master.gain.value = AUDIO.masterVolume
    master.connect(ctx.destination)

    // Layer 1: Attack (white noise burst 20ms)
    const noiseBuffer = ctx.createBuffer(1, Math.round(ctx.sampleRate * AUDIO.attackDuration), ctx.sampleRate)
    const noiseData = noiseBuffer.getChannelData(0)
    for (let i = 0; i < noiseData.length; i++) {
      noiseData[i] = Math.random() * 2 - 1
    }
    const noiseSource = ctx.createBufferSource()
    noiseSource.buffer = noiseBuffer
    const noiseFilter = ctx.createBiquadFilter()
    noiseFilter.type = 'highpass'
    noiseFilter.frequency.value = 2000
    const noiseGain = ctx.createGain()
    noiseGain.gain.setValueAtTime(0, now)
    noiseGain.gain.linearRampToValueAtTime(0.6, now + 0.002)
    noiseGain.gain.linearRampToValueAtTime(0, now + AUDIO.attackDuration)
    noiseSource.connect(noiseFilter).connect(noiseGain).connect(master)
    noiseSource.start(now)
    noiseSource.stop(now + AUDIO.attackDuration + 0.01)

    // Layer 2: Body (60Hz + 120Hz sine)
    const body1 = ctx.createOscillator()
    body1.frequency.value = AUDIO.bodyFreqLow
    const body2 = ctx.createOscillator()
    body2.frequency.value = AUDIO.bodyFreqHigh
    const bodyGain1 = ctx.createGain()
    bodyGain1.gain.setValueAtTime(0.3, now)
    bodyGain1.gain.linearRampToValueAtTime(0.2, now + 0.2)
    bodyGain1.gain.linearRampToValueAtTime(0, now + AUDIO.bodyDuration)
    const bodyGain2 = ctx.createGain()
    bodyGain2.gain.setValueAtTime(0.12, now) // 0.4 × 0.3
    bodyGain2.gain.linearRampToValueAtTime(0.08, now + 0.2)
    bodyGain2.gain.linearRampToValueAtTime(0, now + AUDIO.bodyDuration)
    body1.connect(bodyGain1).connect(master)
    body2.connect(bodyGain2).connect(master)
    body1.start(now); body1.stop(now + AUDIO.bodyDuration + 0.05)
    body2.start(now); body2.stop(now + AUDIO.bodyDuration + 0.05)

    // Layer 3: Crackle (random square pulses)
    const crackleCount = AUDIO.crackleCount[0] + Math.floor(Math.random() * (AUDIO.crackleCount[1] - AUDIO.crackleCount[0] + 1))
    const usedTimes: number[] = []
    for (let i = 0; i < crackleCount; i++) {
      let t: number
      do {
        t = 0.05 + Math.random() * 0.2 // 50-250ms
      } while (usedTimes.some(ut => Math.abs(ut - t) < 0.03)) // min 30ms spacing
      usedTimes.push(t)

      const osc = ctx.createOscillator()
      osc.type = 'square'
      osc.frequency.value = AUDIO.crackleMinFreq + Math.random() * (AUDIO.crackleMaxFreq - AUDIO.crackleMinFreq)
      const dur = 0.005 + Math.random() * 0.01 // 5-15ms
      const g = ctx.createGain()
      g.gain.setValueAtTime(0.15 + Math.random() * 0.1, now + t)
      g.gain.linearRampToValueAtTime(0, now + t + dur)
      osc.connect(g).connect(master)
      osc.start(now + t)
      osc.stop(now + t + dur + 0.01)
    }

    // Layer 4: Sub bass (35Hz)
    const sub = ctx.createOscillator()
    sub.frequency.value = AUDIO.subBassFreq
    const subGain = ctx.createGain()
    subGain.gain.setValueAtTime(0.3 + Math.random() * 0.2, now)
    subGain.gain.linearRampToValueAtTime(0.2, now + 0.1)
    subGain.gain.linearRampToValueAtTime(0, now + AUDIO.subBassDuration)
    sub.connect(subGain).connect(master)
    sub.start(now)
    sub.stop(now + AUDIO.subBassDuration + 0.05)
  }, [])

  /**
   * Call every frame with current scrollProgress.
   * Handles trigger logic: play once at 0.70 crossing, reset at 0.30.
   */
  const checkTrigger = useCallback((scrollProgress: number, prevProgress: number) => {
    if (scrollProgress >= AUDIO.triggerThreshold && prevProgress < AUDIO.triggerThreshold && !hasPlayedRef.current) {
      play()
      hasPlayedRef.current = true
    }
    if (scrollProgress < AUDIO.resetThreshold) {
      hasPlayedRef.current = false
    }
  }, [play])

  return { ensureContext, checkTrigger }
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/useAudioBurst.ts
git commit -m "feat(k2cc-hero): procedural Web Audio burst sound with 4-layer synthesis"
```

---

## Task 10: React Canvas Component

**Files:**
- Create: `web/src/components/k2cc-hero/K2ccPulseCanvas.tsx`

- [ ] **Step 1: Implement K2ccPulseCanvas**

This component:
1. Renders a fixed `<canvas>` element
2. Handles ResizeObserver for canvas resolution updates
3. Creates `PulseRenderer` instance
4. Runs rAF loop, calling `renderer.tick()` with scroll progress
5. Pauses on `document.hidden`
6. Handles `prefers-reduced-motion` (static fallback)
7. Calls `ensureContext()` on first user interaction for audio
8. Computes `RenderConfig` based on viewport size

```typescript
// web/src/components/k2cc-hero/K2ccPulseCanvas.tsx
'use client'

import { useRef, useEffect, useCallback } from 'react'
import { BREAKPOINTS } from './constants'
import { useScrollProgress } from './useScrollProgress'
import { useAudioBurst } from './useAudioBurst'
import { PulseRenderer } from './renderer'
import type { RenderConfig } from './types'

function getRenderConfig(width: number, height: number): RenderConfig {
  const isMobile = width < BREAKPOINTS.mobile
  const isTablet = width >= BREAKPOINTS.mobile && width < BREAKPOINTS.tablet
  const dpr = Math.min(window.devicePixelRatio, isMobile ? 2 : window.devicePixelRatio)

  return {
    width,
    height,
    dpr,
    lineY: height * 0.4,
    wordmarkY: height * 0.22,
    wordmarkScale: isMobile ? 0.6 : 1.2,
    visibleCycles: isMobile ? 2 : 3,
    maxArcCount: isMobile ? 4 : isTablet ? 6 : 8,
    maxArcDepth: isMobile ? 5 : 7,
    maxParticles: isMobile ? 20 : isTablet ? 30 : 50,
    useRadialGlow: !isMobile,
  }
}

export function K2ccPulseCanvas() {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rendererRef = useRef<PulseRenderer | null>(null)
  const rafRef = useRef<number>(0)
  const { getProgress } = useScrollProgress()
  const { ensureContext, checkTrigger } = useAudioBurst()

  // Activate audio on first interaction
  const handleInteraction = useCallback(() => {
    ensureContext()
    // Listeners registered with { once: true } auto-remove after firing.
    // No manual removal needed.
  }, [ensureContext])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    // Reduced motion: render a single static green line + k2cc wordmark, no animation
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
      const rect = canvas.getBoundingClientRect()
      const dpr = Math.min(window.devicePixelRatio, 2)
      canvas.width = rect.width * dpr
      canvas.height = rect.height * dpr
      const staticCtx = canvas.getContext('2d')
      if (staticCtx) {
        staticCtx.scale(dpr, dpr)
        const lineY = rect.height * 0.4
        // Static green line
        staticCtx.strokeStyle = '#00ff88'
        staticCtx.lineWidth = 1.5
        staticCtx.globalAlpha = 0.6
        staticCtx.beginPath()
        staticCtx.moveTo(0, lineY)
        staticCtx.lineTo(rect.width, lineY)
        staticCtx.stroke()
        // Faint glow
        staticCtx.shadowColor = '#00ff88'
        staticCtx.shadowBlur = 4
        staticCtx.stroke()
        staticCtx.shadowBlur = 0
        staticCtx.globalAlpha = 1
      }
      return
    }

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    // Initial sizing
    const rect = canvas.getBoundingClientRect()
    const config = getRenderConfig(rect.width, rect.height)
    canvas.width = rect.width * config.dpr
    canvas.height = rect.height * config.dpr
    ctx.scale(config.dpr, config.dpr)

    const renderer = new PulseRenderer(ctx, config)
    rendererRef.current = renderer

    // ResizeObserver
    let resizeTimer: ReturnType<typeof setTimeout>
    const observer = new ResizeObserver((entries) => {
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(() => {
        const entry = entries[0]
        if (!entry) return
        const { width, height } = entry.contentRect
        const newConfig = getRenderConfig(width, height)
        canvas.width = width * newConfig.dpr
        canvas.height = height * newConfig.dpr
        ctx.setTransform(1, 0, 0, 1, 0, 0)
        ctx.scale(newConfig.dpr, newConfig.dpr)
        renderer.updateConfig(newConfig)
      }, 100)
    })
    observer.observe(canvas)

    // Visibility
    let paused = false
    const handleVisibility = () => {
      paused = document.hidden
    }
    document.addEventListener('visibilitychange', handleVisibility)

    // Audio activation
    window.addEventListener('click', handleInteraction, { once: true })
    window.addEventListener('scroll', handleInteraction, { once: true, passive: true })
    window.addEventListener('touchstart', handleInteraction, { once: true })

    // rAF loop
    let prevScrollProgress = 0
    const tick = (timestamp: number) => {
      if (!paused) {
        const scrollState = getProgress()
        renderer.tick(timestamp, scrollState.current)
        checkTrigger(scrollState.current, prevScrollProgress)
        prevScrollProgress = scrollState.current
      }
      rafRef.current = requestAnimationFrame(tick)
    }
    rafRef.current = requestAnimationFrame(tick)

    return () => {
      cancelAnimationFrame(rafRef.current)
      observer.disconnect()
      document.removeEventListener('visibilitychange', handleVisibility)
      clearTimeout(resizeTimer)
    }
  }, [getProgress, checkTrigger, handleInteraction])

  return (
    <canvas
      ref={canvasRef}
      className="fixed inset-0 w-full h-full pointer-events-none"
      style={{ zIndex: 0 }}
      aria-hidden="true"
    />
  )
}
```

- [ ] **Step 2: Commit**

```bash
cd web && git add src/components/k2cc-hero/K2ccPulseCanvas.tsx
git commit -m "feat(k2cc-hero): React canvas component with resize, visibility, and audio lifecycle"
```

---

## Task 11: Homepage Integration

**Files:**
- Modify: `web/src/app/[locale]/HomeClient.tsx`
- Modify: `web/src/app/[locale]/page.tsx`

- [ ] **Step 1: Update HomeClient.tsx**

Replace the empty placeholder with the canvas import:

```typescript
// web/src/app/[locale]/HomeClient.tsx
"use client";

import { K2ccPulseCanvas } from '@/components/k2cc-hero/K2ccPulseCanvas'

export default function HomeClient(): React.ReactElement {
  return <K2ccPulseCanvas />
}
```

- [ ] **Step 2: Add HomeClient to page.tsx**

In `web/src/app/[locale]/page.tsx`, add the import at the top:

```typescript
import HomeClient from './HomeClient';
```

Then inside the returned JSX, add `<HomeClient />` as the first child after `<Header />`:

```tsx
<Header />
<HomeClient />
{/* rest of sections... */}
```

- [ ] **Step 3: Make section backgrounds semi-transparent**

Update each section's background in `page.tsx` to be semi-transparent so the canvas shows through:

1. **Hero section** (`<section className="py-20 ...">`) — remove any background color, add:
   - The section itself: no background (text floats on canvas)
   - The terminal preview card: keep its existing dark background (already has opacity via `var(--card)`)

2. **Feature Cards section** — change `backgroundColor: 'rgba(17,17,24,0.5)'` to:
   ```
   backgroundColor: 'rgba(5,5,8,0.6)', backdropFilter: 'blur(8px)'
   ```

3. **Comparison section** — add to the section:
   ```
   style={{ backgroundColor: 'rgba(5,5,8,0.4)', backdropFilter: 'blur(8px)' }}
   ```

4. **Download section** — add:
   ```
   style={{ backgroundColor: 'rgba(5,5,8,0.4)', backdropFilter: 'blur(8px)' }}
   ```

5. **Body background** — ensure the root `<div>` background allows canvas to show. Change from `var(--background)` to `#050508` (matching spec's BG_COLOR).

- [ ] **Step 4: Commit**

```bash
cd web && git add src/app/\\[locale\\]/HomeClient.tsx src/app/\\[locale\\]/page.tsx
git commit -m "feat(k2cc-hero): integrate pulse canvas into homepage with semi-transparent sections"
```

---

## Task 12: Visual Testing and Tuning

**Files:**
- No new files — tuning existing parameters

- [ ] **Step 1: Run the dev server and verify**

```bash
cd web && yarn dev
```

Open `http://localhost:3000` and verify:
1. Canvas renders behind content, fixed position
2. Pulse line visible at rest (green, PQRST shape)
3. Scrolling progresses through beats: rest → sense → silence → buildup → burst → aftermath
4. Lightning arcs fire upward during burst phase
5. Sound triggers at ~70% scroll progress
6. Content text remains readable over canvas
7. No performance issues (check DevTools FPS counter)

- [ ] **Step 2: Test responsive breakpoints**

1. Resize to < 768px — verify mobile degradation (fewer arcs, simpler glow)
2. Resize to 768-1024px — verify tablet settings
3. Check DevTools mobile emulation for touch scrolling behavior

- [ ] **Step 3: Test edge cases**

1. Fast scroll to bottom and back — animation should be smooth
2. Switch tabs and back — animation should pause and resume
3. Rapid scroll oscillation around burst threshold — sound should play max once per cycle
4. Very slow scroll through each beat — transitions should be smooth, no jumps

- [ ] **Step 4: Tune parameters if needed**

Based on visual testing, adjust values in `constants.ts`:
- Amplitude values
- Glow intensity
- Lightning displacement and branching probabilities
- Sound volume and timing
- Scroll-to-beat mapping

- [ ] **Step 5: Final commit**

```bash
cd web && git add -A
git commit -m "feat(k2cc-hero): visual tuning and parameter adjustments"
```
