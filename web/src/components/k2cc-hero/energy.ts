import { BEAT, BEAT_PARAMS, BRAND_GREEN_RGB, DIM_GREEN_RGB, WHITE_RGB } from './constants'
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
  [BRAND_GREEN_RGB, DIM_GREEN_RGB],         // silence: green -> dim
  [DIM_GREEN_RGB, WHITE_RGB],               // buildup: dim -> white
  [WHITE_RGB, WHITE_RGB],                   // burst: solid white
  [WHITE_RGB, BRAND_GREEN_RGB],             // aftermath: white -> green
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

  // Burst-phase wordmark progressive light-up: 0.65->0.75 ramps 0->1
  let wordmarkOpacity = 0
  if (beat === 4) {
    // Within burst (0.65-0.80), light up from 0->1 in first 2/3 of phase
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
