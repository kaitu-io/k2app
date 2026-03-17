import type { GlitchState } from './types'

// --- Gaussian helper ---
function gaussian(x: number, mean: number, sigma: number): number {
  const d = x - mean
  return Math.exp(-(d * d) / (2 * sigma * sigma))
}

// --- PQRST template (normalized t in [0, 1]) ---
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
