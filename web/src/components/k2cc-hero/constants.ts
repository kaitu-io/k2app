export const BRAND_GREEN = '#00ff88'
export const BRAND_GREEN_RGB: [number, number, number] = [0, 255, 136]
export const DIM_GREEN = '#005533'
export const DIM_GREEN_RGB: [number, number, number] = [0, 85, 51]
export const WHITE_RGB: [number, number, number] = [255, 255, 255]
export const BG_COLOR = '#050508'
export const BG_GLOW_COLOR = '#0a1a0f'

export const BEAT = {
  REST_END: 0.20,
  SENSE_END: 0.35,
  SILENCE_END: 0.45,
  BUILDUP_END: 0.65,
  BURST_END: 0.80,
} as const

export const BEAT_PARAMS = {
  amplitude:      [30,  80,  12,  150, 300, 30],
  frequency:      [0.8, 2.0, 0.4, 3.5, 0,   0.8],
  glowRadius:     [15,  80,  5,   200, 999, 15],
  glowIntensity:  [0.03, 0.08, 0.02, 0.12, 0.20, 0.03],
  lineWidth:      [1.5, 2.0, 1.0, 2.5, 0,   1.5],
  noiseIntensity: [0,   0.3, 0,   0.5, 0,   0],
  branchCount:    [0,   0,   0,   3,   0,   0],
  arcCount:       [0,   0,   0,   0,   8,   0],
  arcDepth:       [0,   0,   0,   0,   7,   0],
  screenShake:    [0,   0,   0,   1,   2,   0],
  wordmarkOpacity:[0,   0,   0,   0,   1,   0],
} as const

export const BREAKPOINTS = {
  mobile: 768,
  tablet: 1024,
} as const

export const AUDIO = {
  masterVolume: 0.12,
  attackDuration: 0.02,
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

export const LIGHTNING = {
  decayFactor: 0.55,
  trunkBranchProb: 0.35,
  level1BranchProb: 0.18,
  level2BranchProb: 0.08,
  maxBranchDepth: 3,
  branchAngleMin: 20,
  branchAngleMax: 50,
  branchLengthMin: 0.3,
  branchLengthMax: 0.6,
  regenerateIntervalMs: 100,
} as const

export const K2CC_PATH: Array<[number, number][]> = [
  [],
  [],
  [],
  [],
]

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

export const SCROLL_LERP = 0.08
export const PERF_SAMPLE_FRAMES = 60
export const PERF_THRESHOLD_MS = 20
