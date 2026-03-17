import type { RenderState } from './types';
import { getEnergyParams, getBeat } from './energy';
import { waveform, updateGlitch } from './waveform';
import { generateArcs } from './lightning';
import { updateParticles, renderParticles, spawnParticle } from './particles';
import type { Particle } from './types';
import {
  COLOR_PRIMARY, COLOR_WHITE,
  LINE_Y_RATIO, VISIBLE_CYCLES_DESKTOP, VISIBLE_CYCLES_MOBILE,
  ARC_COUNT_DESKTOP, ARC_COUNT_MOBILE, ARC_DEPTH_DESKTOP, ARC_DEPTH_MOBILE,
  WORDMARK_Y_RATIO, SOUND_TRIGGER_PROGRESS, SOUND_RESET_PROGRESS,
} from './constants';

function hexToRgba(hex: string, alpha: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

export interface TickContext {
  ctx: CanvasRenderingContext2D;
  width: number;
  height: number;
  isMobile: boolean;
  particles: Particle[];
  playSound: () => void;
}

export function tick(
  tickCtx: TickContext,
  state: RenderState,
  deltaTime: number,
): RenderState {
  const { ctx, width, height, isMobile, particles, playSound } = tickCtx;
  const { smoothProgress, time } = state;

  const energy = getEnergyParams(smoothProgress);
  const beat = getBeat(smoothProgress);

  // Clear at identity (avoid shake ghosting)
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  ctx.restore();

  const lineY = height * LINE_Y_RATIO;
  const visibleCycles = isMobile ? VISIBLE_CYCLES_MOBILE : VISIBLE_CYCLES_DESKTOP;

  // Screen shake
  let shakeX = 0, shakeY = 0;
  if (beat === 'burst') {
    shakeX = (Math.random() * 2 - 1) * 2;
    shakeY = (Math.random() * 2 - 1) * 2;
  } else if (beat === 'buildup' && smoothProgress > 0.55) {
    shakeX = (Math.random() * 2 - 1) * 1;
    shakeY = (Math.random() * 2 - 1) * 1;
  }
  ctx.save();
  ctx.translate(shakeX, shakeY);

  // Glow layer
  if (beat !== 'burst') {
    ctx.globalCompositeOperation = 'screen';
    const intensity = beat === 'silence' ? 0.02 : beat === 'buildup' ? 0.15 : 0.03;
    const glowR = Math.min(energy.glowRadius, Math.max(width, height));
    const gradient = ctx.createRadialGradient(width / 2, lineY, 0, width / 2, lineY, glowR);
    gradient.addColorStop(0, hexToRgba(energy.color, intensity));
    gradient.addColorStop(0.4, hexToRgba(energy.color, intensity * 0.3));
    gradient.addColorStop(1, hexToRgba(energy.color, 0));
    ctx.fillStyle = gradient;
    ctx.fillRect(width / 2 - glowR, lineY - glowR, glowR * 2, glowR * 2);
    ctx.globalCompositeOperation = 'source-over';
  } else {
    ctx.globalCompositeOperation = 'screen';
    ctx.fillStyle = hexToRgba(COLOR_PRIMARY, 0.05 + Math.random() * 0.1);
    ctx.fillRect(0, 0, width, height);
    ctx.globalCompositeOperation = 'source-over';
  }

  // Main pulse line (not during burst)
  if (beat !== 'burst') {
    const glitch = updateGlitch(state.glitch, smoothProgress);

    ctx.beginPath();
    ctx.strokeStyle = energy.color;
    ctx.lineWidth = energy.lineWidth;
    ctx.shadowColor = energy.color;
    ctx.shadowBlur = 4;

    for (let x = 0; x <= width; x++) {
      let drawX = x;
      if (glitch.phase === 'active' && Math.abs(x - width / 2) < glitch.width / 2) {
        drawX += glitch.offset;
      }
      const y = lineY + waveform(x, time, energy.amplitude, energy.frequency, energy.noiseIntensity, width, visibleCycles);

      if (x === 0) ctx.moveTo(drawX, y);
      else ctx.lineTo(drawX, y);
    }
    ctx.stroke();
    ctx.shadowBlur = 0;

    // Buildup: spawn particles from wave peaks
    if (beat === 'buildup' && Math.random() < 0.1) {
      const peakX = Math.random() * width;
      const peakY = lineY + waveform(peakX, time, energy.amplitude, energy.frequency, energy.noiseIntensity, width, visibleCycles);
      spawnParticle(particles, peakX, peakY, 1 + Math.random() * 2, 1 + Math.random(), 0.02 + Math.random() * 0.02);
    }

    // Update and render particles
    updateParticles(particles);
    renderParticles(ctx, particles, COLOR_PRIMARY);

    ctx.restore();

    return {
      ...state,
      time: time + deltaTime * 0.001,
      beat,
      energy,
      glitch,
      shakeX,
      shakeY,
    };
  }

  // Lightning arcs (burst phase)
  const wordmarkY = height * WORDMARK_Y_RATIO;
  const arcCount = isMobile ? ARC_COUNT_MOBILE : ARC_COUNT_DESKTOP;
  const arcDepth = isMobile ? ARC_DEPTH_MOBILE : ARC_DEPTH_DESKTOP;
  const arcs = generateArcs(lineY, width, wordmarkY, arcCount, arcDepth);

  for (const arc of arcs) {
    for (const seg of arc) {
      ctx.beginPath();
      ctx.moveTo(seg.x1, seg.y1);
      ctx.lineTo(seg.x2, seg.y2);
      ctx.strokeStyle = hexToRgba(COLOR_WHITE, seg.brightness * (0.7 + Math.random() * 0.3));
      ctx.lineWidth = seg.width;
      ctx.shadowColor = COLOR_PRIMARY;
      ctx.shadowBlur = 6;
      ctx.stroke();
    }
  }
  ctx.shadowBlur = 0;

  // k2cc wordmark during burst
  const burstProgress = (smoothProgress - 0.65) / 0.15;
  let wordmarkOpacity = Math.min(1, Math.max(0, burstProgress));

  // Aftermath: wordmark fade out
  if (smoothProgress > 0.93) {
    wordmarkOpacity = Math.max(0, 1 - (smoothProgress - 0.93) / 0.03);
  }

  if (wordmarkOpacity > 0) {
    ctx.font = `bold ${isMobile ? 60 : 120}px monospace`;
    ctx.textAlign = 'center';
    ctx.fillStyle = hexToRgba(COLOR_WHITE, wordmarkOpacity);
    ctx.shadowColor = COLOR_PRIMARY;
    ctx.shadowBlur = 6;
    ctx.fillText('k2cc', width / 2, wordmarkY + (isMobile ? 20 : 40));
    ctx.shadowBlur = 0;
  }

  // Aftermath particles from arc endpoints
  if (smoothProgress > 0.80 && Math.random() < 0.3) {
    const arcSet = arcs[Math.floor(Math.random() * arcs.length)];
    if (arcSet && arcSet.length > 0) {
      const seg = arcSet[Math.floor(Math.random() * arcSet.length)];
      for (let i = 0; i < 3; i++) {
        spawnParticle(particles, seg.x2, seg.y2, 2 + Math.random() * 3, 2 + Math.random(), 0.015 + Math.random() * 0.025);
      }
    }
  }

  // Sound trigger — only when scrolling DOWN past threshold
  let hasPlayedSound = state.hasPlayedSound;
  if (smoothProgress >= SOUND_TRIGGER_PROGRESS && !hasPlayedSound && state.scrollDirection === 'down') {
    playSound();
    hasPlayedSound = true;
  }
  if (smoothProgress < SOUND_RESET_PROGRESS) {
    hasPlayedSound = false;
  }

  // Update and render particles
  updateParticles(particles);
  renderParticles(ctx, particles, beat === 'burst' || beat === 'aftermath' ? COLOR_WHITE : COLOR_PRIMARY);

  ctx.restore();

  return {
    ...state,
    time: time + deltaTime * 0.001,
    beat,
    energy,
    shakeX,
    shakeY,
    hasPlayedSound,
    wordmarkOpacity,
  };
}
