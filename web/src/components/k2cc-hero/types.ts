export interface Particle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number;
  decay: number;
  size: number;
  brightness: number;
  active: boolean;
}

export interface BoltSegment {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  width: number;
  brightness: number;
  depth: number;
}

export interface EnergyParams {
  amplitude: number;
  frequency: number;
  glowRadius: number;
  color: string;
  noiseIntensity: number;
  lineWidth: number;
}

export interface GlitchState {
  phase: 'idle' | 'triggered' | 'active' | 'cooldown';
  framesLeft: number;
  cooldownLeft: number;
  offset: number;
  width: number;
}

export type BeatName = 'rest' | 'sense' | 'silence' | 'buildup' | 'burst' | 'aftermath';

export interface RenderState {
  scrollProgress: number;
  smoothProgress: number;
  scrollDirection: 'down' | 'up';
  time: number;
  beat: BeatName;
  energy: EnergyParams;
  glitch: GlitchState;
  particles: Particle[];
  shakeX: number;
  shakeY: number;
  hasPlayedSound: boolean;
  wordmarkOpacity: number;
}
