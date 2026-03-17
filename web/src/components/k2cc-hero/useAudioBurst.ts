import { useRef, useCallback } from 'react'
import { AUDIO } from './constants'

/**
 * Procedural burst sound using Web Audio API.
 * AudioContext created lazily on first user interaction.
 * Every trigger generates slightly different sound via randomization.
 */
export function useAudioBurst() {
  const ctxRef = useRef<AudioContext | null>(null)
  const hasPlayedRef = useRef(false)
  const activatedRef = useRef(false)

  // Lazy init — call on first click/scroll/touch
  const ensureContext = useCallback(() => {
    if (ctxRef.current || activatedRef.current) return
    if (typeof window === 'undefined') return
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return

    activatedRef.current = true
    try {
      ctxRef.current = new AudioContext()
    } catch {
      // Web Audio not available
    }
  }, [])

  const play = useCallback(() => {
    const ctx = ctxRef.current
    if (!ctx) return
    if (ctx.state === 'suspended') {
      ctx.resume()
    }

    const now = ctx.currentTime
    const master = ctx.createGain()
    master.gain.value = AUDIO.masterVolume
    master.connect(ctx.destination)

    // Layer 1: Attack (white noise burst 20ms)
    const noiseBuffer = ctx.createBuffer(1, Math.round(ctx.sampleRate * AUDIO.attackDuration), ctx.sampleRate)
    const noiseData = noiseBuffer.getChannelData(0)
    for (let i = 0; i < noiseData.length; i++) {
      noiseData[i] = Math.random() * 2 - 1
    }
    const noiseSource = ctx.createBufferSource()
    noiseSource.buffer = noiseBuffer
    const noiseFilter = ctx.createBiquadFilter()
    noiseFilter.type = 'highpass'
    noiseFilter.frequency.value = 2000
    const noiseGain = ctx.createGain()
    noiseGain.gain.setValueAtTime(0, now)
    noiseGain.gain.linearRampToValueAtTime(0.6, now + 0.002)
    noiseGain.gain.linearRampToValueAtTime(0, now + AUDIO.attackDuration)
    noiseSource.connect(noiseFilter).connect(noiseGain).connect(master)
    noiseSource.start(now)
    noiseSource.stop(now + AUDIO.attackDuration + 0.01)

    // Layer 2: Body (60Hz + 120Hz sine)
    const body1 = ctx.createOscillator()
    body1.frequency.value = AUDIO.bodyFreqLow
    const body2 = ctx.createOscillator()
    body2.frequency.value = AUDIO.bodyFreqHigh
    const bodyGain1 = ctx.createGain()
    bodyGain1.gain.setValueAtTime(0.3, now)
    bodyGain1.gain.linearRampToValueAtTime(0.2, now + 0.2)
    bodyGain1.gain.linearRampToValueAtTime(0, now + AUDIO.bodyDuration)
    const bodyGain2 = ctx.createGain()
    bodyGain2.gain.setValueAtTime(0.12, now) // 0.4 x 0.3
    bodyGain2.gain.linearRampToValueAtTime(0.08, now + 0.2)
    bodyGain2.gain.linearRampToValueAtTime(0, now + AUDIO.bodyDuration)
    body1.connect(bodyGain1).connect(master)
    body2.connect(bodyGain2).connect(master)
    body1.start(now); body1.stop(now + AUDIO.bodyDuration + 0.05)
    body2.start(now); body2.stop(now + AUDIO.bodyDuration + 0.05)

    // Layer 3: Crackle (random square pulses)
    const crackleCount = AUDIO.crackleCount[0] + Math.floor(Math.random() * (AUDIO.crackleCount[1] - AUDIO.crackleCount[0] + 1))
    const usedTimes: number[] = []
    for (let i = 0; i < crackleCount; i++) {
      let t: number
      do {
        t = 0.05 + Math.random() * 0.2 // 50-250ms
      } while (usedTimes.some(ut => Math.abs(ut - t) < 0.03)) // min 30ms spacing
      usedTimes.push(t)

      const osc = ctx.createOscillator()
      osc.type = 'square'
      osc.frequency.value = AUDIO.crackleMinFreq + Math.random() * (AUDIO.crackleMaxFreq - AUDIO.crackleMinFreq)
      const dur = 0.005 + Math.random() * 0.01 // 5-15ms
      const g = ctx.createGain()
      g.gain.setValueAtTime(0.15 + Math.random() * 0.1, now + t)
      g.gain.linearRampToValueAtTime(0, now + t + dur)
      osc.connect(g).connect(master)
      osc.start(now + t)
      osc.stop(now + t + dur + 0.01)
    }

    // Layer 4: Sub bass (35Hz)
    const sub = ctx.createOscillator()
    sub.frequency.value = AUDIO.subBassFreq
    const subGain = ctx.createGain()
    subGain.gain.setValueAtTime(0.3 + Math.random() * 0.2, now)
    subGain.gain.linearRampToValueAtTime(0.2, now + 0.1)
    subGain.gain.linearRampToValueAtTime(0, now + AUDIO.subBassDuration)
    sub.connect(subGain).connect(master)
    sub.start(now)
    sub.stop(now + AUDIO.subBassDuration + 0.05)
  }, [])

  /**
   * Call every frame with current scrollProgress.
   * Handles trigger logic: play once at 0.70 crossing, reset at 0.30.
   */
  const checkTrigger = useCallback((scrollProgress: number, prevProgress: number) => {
    if (scrollProgress >= AUDIO.triggerThreshold && prevProgress < AUDIO.triggerThreshold && !hasPlayedRef.current) {
      play()
      hasPlayedRef.current = true
    }
    if (scrollProgress < AUDIO.resetThreshold) {
      hasPlayedRef.current = false
    }
  }, [play])

  return { ensureContext, checkTrigger }
}
