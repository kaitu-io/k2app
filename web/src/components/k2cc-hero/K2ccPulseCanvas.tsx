'use client';

import { useRef, useEffect, useCallback } from 'react';
import { useScrollProgress } from './useScrollProgress';
import { useAudioBurst } from './useAudioBurst';
import { tick, type TickContext } from './renderer';
import { createParticlePool } from './particles';
import { getEnergyParams } from './energy';
import type { RenderState } from './types';
import { PARTICLE_POOL_DESKTOP, PARTICLE_POOL_MOBILE, MAX_DPR_DESKTOP } from './constants';

export default function K2ccPulseCanvas() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rafRef = useRef<number>(0);
  const stateRef = useRef<RenderState | null>(null);
  const particlesRef = useRef(createParticlePool(PARTICLE_POOL_DESKTOP));
  const { getProgress } = useScrollProgress();
  const { play: playSound } = useAudioBurst();
  const lastTimeRef = useRef(0);

  const isMobileRef = useRef(false);
  const reducedMotionRef = useRef(false);

  const setupCanvas = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return null;

    const rect = canvas.getBoundingClientRect();
    const dpr = Math.min(window.devicePixelRatio, MAX_DPR_DESKTOP);
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;

    const ctx = canvas.getContext('2d');
    if (!ctx) return null;
    ctx.scale(dpr, dpr);

    isMobileRef.current = rect.width < 768;
    if (isMobileRef.current) {
      particlesRef.current = createParticlePool(PARTICLE_POOL_MOBILE);
    }

    return { ctx, width: rect.width, height: rect.height };
  }, []);

  useEffect(() => {
    reducedMotionRef.current = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    const setup = setupCanvas();
    if (!setup) return;

    stateRef.current = {
      scrollProgress: 0,
      smoothProgress: 0,
      scrollDirection: 'down',
      time: 0,
      beat: 'rest',
      energy: getEnergyParams(0),
      glitch: { phase: 'idle', framesLeft: 0, cooldownLeft: 0, offset: 0, width: 0 },
      particles: particlesRef.current,
      shakeX: 0,
      shakeY: 0,
      hasPlayedSound: false,
      wordmarkOpacity: 0,
    };

    let resizeTimeout: ReturnType<typeof setTimeout>;
    const observer = new ResizeObserver(() => {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => setupCanvas(), 100);
    });
    if (canvasRef.current) observer.observe(canvasRef.current);

    let visible = true;
    const onVisibility = () => {
      visible = !document.hidden;
      if (visible) lastTimeRef.current = 0;
    };
    document.addEventListener('visibilitychange', onVisibility);

    const loop = (timestamp: number) => {
      if (!visible || !stateRef.current || !canvasRef.current) {
        rafRef.current = requestAnimationFrame(loop);
        return;
      }

      const deltaTime = lastTimeRef.current ? timestamp - lastTimeRef.current : 16;
      lastTimeRef.current = timestamp;

      const progress = getProgress();
      stateRef.current.scrollProgress = progress.raw;
      stateRef.current.smoothProgress = progress.smooth;
      stateRef.current.scrollDirection = progress.direction;

      const canvas = canvasRef.current;
      const ctx = canvas.getContext('2d');
      if (!ctx) {
        rafRef.current = requestAnimationFrame(loop);
        return;
      }

      const rect = canvas.getBoundingClientRect();

      if (reducedMotionRef.current) {
        ctx.save();
        ctx.setTransform(1, 0, 0, 1, 0, 0);
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.restore();
        ctx.strokeStyle = '#00ff88';
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        const y = rect.height * 0.4;
        ctx.moveTo(0, y);
        ctx.lineTo(rect.width, y);
        ctx.stroke();
      } else {
        const tickCtx: TickContext = {
          ctx,
          width: rect.width,
          height: rect.height,
          isMobile: isMobileRef.current,
          particles: particlesRef.current,
          playSound,
        };

        stateRef.current = tick(tickCtx, stateRef.current, deltaTime);
      }

      rafRef.current = requestAnimationFrame(loop);
    };

    rafRef.current = requestAnimationFrame(loop);

    return () => {
      cancelAnimationFrame(rafRef.current);
      clearTimeout(resizeTimeout);
      observer.disconnect();
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [setupCanvas, getProgress, playSound]);

  return (
    <canvas
      ref={canvasRef}
      className="fixed inset-0 w-full h-full pointer-events-none"
      style={{ zIndex: 0 }}
      aria-hidden="true"
    />
  );
}
