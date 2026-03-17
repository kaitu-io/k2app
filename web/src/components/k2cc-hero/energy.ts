import type { EnergyParams, BeatName } from './types';
import {
  BEAT_REST_END, BEAT_SENSE_END, BEAT_SILENCE_END,
  BEAT_BUILDUP_END, BEAT_BURST_END,
  COLOR_PRIMARY, COLOR_SILENCE, COLOR_WHITE,
} from './constants';

function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

function lerpColor(a: string, b: string, t: number): string {
  const parseHex = (h: string) => [
    parseInt(h.slice(1, 3), 16),
    parseInt(h.slice(3, 5), 16),
    parseInt(h.slice(5, 7), 16),
  ];
  const [ar, ag, ab] = parseHex(a);
  const [br, bg, bb] = parseHex(b);
  const r = Math.round(lerp(ar, br, t));
  const g = Math.round(lerp(ag, bg, t));
  const bv = Math.round(lerp(ab, bb, t));
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${bv.toString(16).padStart(2, '0')}`;
}

interface BeatDef {
  name: BeatName;
  start: number;
  end: number;
  params: EnergyParams;
}

const BEATS: BeatDef[] = [
  {
    name: 'rest', start: 0, end: BEAT_REST_END,
    params: { amplitude: 30, frequency: 0.8, glowRadius: 15, color: COLOR_PRIMARY, noiseIntensity: 0, lineWidth: 1.5 },
  },
  {
    name: 'sense', start: BEAT_REST_END, end: BEAT_SENSE_END,
    params: { amplitude: 80, frequency: 2.0, glowRadius: 80, color: COLOR_PRIMARY, noiseIntensity: 0.6, lineWidth: 2 },
  },
  {
    name: 'silence', start: BEAT_SENSE_END, end: BEAT_SILENCE_END,
    params: { amplitude: 12, frequency: 0.4, glowRadius: 5, color: COLOR_SILENCE, noiseIntensity: 0, lineWidth: 1 },
  },
  {
    name: 'buildup', start: BEAT_SILENCE_END, end: BEAT_BUILDUP_END,
    params: { amplitude: 150, frequency: 3.5, glowRadius: 200, color: COLOR_WHITE, noiseIntensity: 0.3, lineWidth: 2.5 },
  },
  {
    name: 'burst', start: BEAT_BUILDUP_END, end: BEAT_BURST_END,
    params: { amplitude: 0, frequency: 0, glowRadius: 9999, color: COLOR_WHITE, noiseIntensity: 0, lineWidth: 0 },
  },
  {
    name: 'aftermath', start: BEAT_BURST_END, end: 1.0,
    params: { amplitude: 30, frequency: 0.8, glowRadius: 15, color: COLOR_PRIMARY, noiseIntensity: 0, lineWidth: 1.5 },
  },
];

export function getBeat(progress: number): BeatName {
  for (const beat of BEATS) {
    if (progress < beat.end) return beat.name;
  }
  return 'aftermath';
}

export function getEnergyParams(progress: number): EnergyParams {
  const clampedProgress = Math.max(0, Math.min(1, progress));

  let currentIdx = 0;
  for (let i = 0; i < BEATS.length; i++) {
    if (clampedProgress < BEATS[i].end) {
      currentIdx = i;
      break;
    }
    if (i === BEATS.length - 1) currentIdx = i;
  }

  const current = BEATS[currentIdx];
  const next = BEATS[Math.min(currentIdx + 1, BEATS.length - 1)];

  const beatRange = current.end - current.start;
  const beatProgress = beatRange > 0 ? (clampedProgress - current.start) / beatRange : 0;

  const blendFactor = beatProgress > 0.5 ? (beatProgress - 0.5) * 2 : 0;
  const blendEased = easeInOutCubic(blendFactor);

  return {
    amplitude: lerp(current.params.amplitude, next.params.amplitude, blendEased),
    frequency: lerp(current.params.frequency, next.params.frequency, blendEased),
    glowRadius: lerp(current.params.glowRadius, next.params.glowRadius, blendEased),
    color: lerpColor(current.params.color, next.params.color, blendEased),
    noiseIntensity: lerp(current.params.noiseIntensity, next.params.noiseIntensity, blendEased),
    lineWidth: lerp(current.params.lineWidth, next.params.lineWidth, blendEased),
  };
}
