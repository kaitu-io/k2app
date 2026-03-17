import { useRef, useCallback } from 'react';
import { MASTER_GAIN } from './constants';

export function useAudioBurst() {
  const ctxRef = useRef<AudioContext | null>(null);

  const ensureContext = useCallback(() => {
    if (typeof window === 'undefined') return null;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return null;

    if (!ctxRef.current) {
      ctxRef.current = new AudioContext();
    }
    if (ctxRef.current.state === 'suspended') {
      ctxRef.current.resume();
    }
    return ctxRef.current;
  }, []);

  const play = useCallback(() => {
    const ctx = ensureContext();
    if (!ctx) return;

    const master = ctx.createGain();
    master.gain.value = MASTER_GAIN;
    master.connect(ctx.destination);
    const now = ctx.currentTime;

    // Layer 1: Attack — white noise burst 20ms
    const bufferSize = Math.floor(ctx.sampleRate * 0.02);
    const buffer = ctx.createBuffer(1, bufferSize, ctx.sampleRate);
    const data = buffer.getChannelData(0);
    for (let i = 0; i < bufferSize; i++) data[i] = Math.random() * 2 - 1;
    const noise = ctx.createBufferSource();
    noise.buffer = buffer;
    const hpf = ctx.createBiquadFilter();
    hpf.type = 'highpass';
    hpf.frequency.value = 2000;
    const noiseGain = ctx.createGain();
    noiseGain.gain.setValueAtTime(0, now);
    noiseGain.gain.linearRampToValueAtTime(0.6, now + 0.002);
    noiseGain.gain.linearRampToValueAtTime(0, now + 0.02);
    noise.connect(hpf).connect(noiseGain).connect(master);
    noise.start(now);
    noise.stop(now + 0.02);

    // Layer 2: Body — 60Hz + 120Hz sine
    const body60 = ctx.createOscillator();
    body60.frequency.value = 60;
    const body120 = ctx.createOscillator();
    body120.frequency.value = 120;
    const bodyGain = ctx.createGain();
    bodyGain.gain.setValueAtTime(0.3, now);
    bodyGain.gain.linearRampToValueAtTime(0.2, now + 0.2);
    bodyGain.gain.linearRampToValueAtTime(0, now + 0.5);
    const body120Gain = ctx.createGain();
    body120Gain.gain.value = 0.4;
    body60.connect(bodyGain).connect(master);
    body120.connect(body120Gain).connect(bodyGain);
    body60.start(now);
    body120.start(now);
    body60.stop(now + 0.5);
    body120.stop(now + 0.5);

    // Layer 3: Crackle — 3-5 micro square pulses
    const crackleCount = 3 + Math.floor(Math.random() * 3);
    for (let i = 0; i < crackleCount; i++) {
      const delay = 0.05 + Math.random() * 0.2;
      const dur = 0.005 + Math.random() * 0.01;
      const freq = 1000 + Math.random() * 3000;
      const osc = ctx.createOscillator();
      osc.type = 'square';
      osc.frequency.value = freq;
      const g = ctx.createGain();
      g.gain.setValueAtTime(0.15 + Math.random() * 0.1, now + delay);
      g.gain.linearRampToValueAtTime(0, now + delay + dur);
      osc.connect(g).connect(master);
      osc.start(now + delay);
      osc.stop(now + delay + dur);
    }

    // Layer 4: Sub bass — 35Hz
    const sub = ctx.createOscillator();
    sub.frequency.value = 35;
    const subGain = ctx.createGain();
    subGain.gain.setValueAtTime(0.4, now);
    subGain.gain.linearRampToValueAtTime(0.2, now + 0.1);
    subGain.gain.linearRampToValueAtTime(0, now + 0.4);
    sub.connect(subGain).connect(master);
    sub.start(now);
    sub.stop(now + 0.4);
  }, [ensureContext]);

  return { play };
}
