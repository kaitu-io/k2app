import type { GlitchState } from './types';

function fade(t: number): number { return t * t * t * (t * (t * 6 - 15) + 10); }
function grad(hash: number, x: number): number { return (hash & 1) === 0 ? x : -x; }

const PERM = new Uint8Array(512);
(function initPerm() {
  const p = new Uint8Array(256);
  for (let i = 0; i < 256; i++) p[i] = i;
  let seed = 42;
  const seededRandom = () => {
    seed = (seed * 16807 + 0) % 2147483647;
    return (seed - 1) / 2147483646;
  };
  for (let i = 255; i > 0; i--) {
    const j = Math.floor(seededRandom() * (i + 1));
    [p[i], p[j]] = [p[j], p[i]];
  }
  for (let i = 0; i < 512; i++) PERM[i] = p[i & 255];
})();

export function perlin1d(x: number): number {
  const xi = Math.floor(x) & 255;
  const xf = x - Math.floor(x);
  const u = fade(xf);
  return grad(PERM[xi], xf) * (1 - u) + grad(PERM[xi + 1], xf - 1) * u;
}

function gaussian(x: number, center: number, width: number): number {
  const d = (x - center) / width;
  return Math.exp(-0.5 * d * d);
}

export function pqrst(t: number): number {
  const tn = ((t % 1) + 1) % 1;
  if (tn < 0.12) return 0.15 * Math.sin(Math.PI * tn / 0.12);
  if (tn < 0.20) return 0;
  if (tn < 0.24) return -0.1 * gaussian(tn, 0.22, 0.01);
  if (tn < 0.32) return 1.2 * gaussian(tn, 0.28, 0.015);
  if (tn < 0.36) return -0.15 * gaussian(tn, 0.33, 0.01);
  if (tn < 0.50) return 0;
  if (tn < 0.68) return 0.3 * Math.sin(Math.PI * (tn - 0.50) / 0.18);
  return 0;
}

export function rPeakSwell(phase: number): number {
  const smoothstep = (edge0: number, edge1: number, x: number) => {
    const t = Math.max(0, Math.min(1, (x - edge0) / (edge1 - edge0)));
    return t * t * (3 - 2 * t);
  };
  return smoothstep(0.26, 0.28, phase) * smoothstep(0.30, 0.28, phase);
}

export function updateGlitch(state: GlitchState, scrollProgress: number): GlitchState {
  const inGlitchRange = scrollProgress >= 0.20 && scrollProgress <= 0.65;
  switch (state.phase) {
    case 'idle':
      if (inGlitchRange && Math.random() < 0.003) {
        return {
          phase: 'active',
          framesLeft: 2 + Math.floor(Math.random() * 2),
          cooldownLeft: 0,
          offset: (Math.random() * 2 - 1) * 15,
          width: 80 + Math.random() * 120,
        };
      }
      return state;
    case 'active':
      if (state.framesLeft <= 0) {
        return { ...state, phase: 'cooldown', cooldownLeft: 60 + Math.floor(Math.random() * 60) };
      }
      return { ...state, framesLeft: state.framesLeft - 1 };
    case 'cooldown':
      if (state.cooldownLeft <= 0) {
        return { phase: 'idle', framesLeft: 0, cooldownLeft: 0, offset: 0, width: 0 };
      }
      return { ...state, cooldownLeft: state.cooldownLeft - 1 };
    default:
      return state;
  }
}

export function waveform(
  x: number,
  time: number,
  amplitude: number,
  frequency: number,
  noiseIntensity: number,
  viewportWidth: number,
  visibleCycles: number,
): number {
  const wavelength = viewportWidth / visibleCycles;
  const phase = ((x / wavelength + time * frequency) % 1 + 1) % 1;
  let y = pqrst(phase) * amplitude;
  if (noiseIntensity > 0) {
    y += perlin1d(x * 0.01 + time * 2.0) * noiseIntensity * amplitude * 0.5;
  }
  return y;
}
