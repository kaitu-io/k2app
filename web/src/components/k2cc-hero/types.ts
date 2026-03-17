export interface Particle {
  x: number
  y: number
  vx: number
  vy: number
  life: number
  decay: number
  size: number
  brightness: number
  active: boolean
}

export interface BoltSegment {
  x1: number
  y1: number
  x2: number
  y2: number
  lineWidth: number
  brightness: number
  depth: number
}

export interface EnergyParams {
  amplitude: number
  frequency: number
  glowRadius: number
  glowIntensity: number
  lineWidth: number
  noiseIntensity: number
  color: string
  colorRgb: [number, number, number]
  branchCount: number
  arcCount: number
  arcDepth: number
  particleSpawnRate: number
  screenShake: number
  wordmarkOpacity: number
  isBurst: boolean
  aftermathSubPhase: number
  aftermathLocalProgress: number
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
  lineY: number
  wordmarkY: number
  wordmarkScale: number
  visibleCycles: number
  maxArcCount: number
  maxArcDepth: number
  maxParticles: number
  useRadialGlow: boolean
}
