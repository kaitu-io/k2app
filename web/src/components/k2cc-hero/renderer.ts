import { getEnergyParams } from './energy'
import { waveform, createGlitchState, updateGlitch, isRPeak, rPeakSwell } from './waveform'
import { generateAllArcs, getWordmarkTargets } from './lightning'
import { ParticlePool } from './particles'
import { K2CC_PATH, LIGHTNING, PERF_SAMPLE_FRAMES, PERF_THRESHOLD_MS } from './constants'
import type { GlitchState, BoltSegment, RenderConfig, EnergyParams } from './types'

export class PulseRenderer {
  private ctx: CanvasRenderingContext2D
  private config: RenderConfig

  // Mutable state
  private glitch: GlitchState = createGlitchState()
  private particles: ParticlePool
  private lastArcGenTime = 0
  private currentArcs: BoltSegment[] = []
  private flashIntensity = 0
  private startTime = 0

  // Performance auto-degradation
  private perfSamples: number[] = []
  private degraded = false

  // Aftermath shatter state
  private shatterPoints: Array<{ x: number; y: number; spawned: boolean }> = []

  constructor(ctx: CanvasRenderingContext2D, config: RenderConfig) {
    this.ctx = ctx
    this.config = config
    this.particles = new ParticlePool(config.maxParticles)
  }

  updateConfig(config: RenderConfig): void {
    this.config = config
    // Rebuild particle pool if size changed
    if (config.maxParticles !== this.particles.particles.length) {
      this.particles = new ParticlePool(config.maxParticles)
    }
  }

  tick(timestamp: number, scrollProgress: number): void {
    if (this.startTime === 0) this.startTime = timestamp
    const time = (timestamp - this.startTime) / 1000 // seconds
    const params = getEnergyParams(scrollProgress)
    const { ctx, config } = this
    const { width } = config

    // --- Performance monitoring (first 60 frames) ---
    if (this.perfSamples.length < PERF_SAMPLE_FRAMES) {
      this.perfSamples.push(timestamp)
      if (this.perfSamples.length === PERF_SAMPLE_FRAMES) {
        // Compute average frame time from consecutive timestamp deltas
        let totalDelta = 0
        for (let i = 1; i < this.perfSamples.length; i++) {
          totalDelta += this.perfSamples[i] - this.perfSamples[i - 1]
        }
        const avg = totalDelta / (this.perfSamples.length - 1)
        if (avg > PERF_THRESHOLD_MS) {
          this.degraded = true
          this.config = {
            ...this.config,
            maxArcCount: Math.max(2, Math.floor(this.config.maxArcCount / 2)),
            maxArcDepth: Math.max(3, this.config.maxArcDepth - 2),
            maxParticles: Math.floor(this.config.maxParticles / 2),
            useRadialGlow: false,
          }
          this.particles = new ParticlePool(this.config.maxParticles)
        }
      }
    }

    // --- Clear at identity transform (avoid shake ghosting) ---
    ctx.save()
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height)
    ctx.restore()

    // --- Screen shake ---
    const shake = params.screenShake
    const shakeX = shake > 0 ? (Math.random() - 0.5) * 2 * shake : 0
    const shakeY = shake > 0 ? (Math.random() - 0.5) * 2 * shake : 0
    ctx.save()
    if (shake > 0) ctx.translate(shakeX, shakeY)

    // --- Glow layer ---
    this.renderGlow(params, time)

    // --- Main pulse line (skip during burst) ---
    if (!params.isBurst && params.lineWidth > 0) {
      this.renderPulseLine(params, time)
    }

    // --- Branch lines (buildup) ---
    if (params.branchCount > 0) {
      this.renderBranches(params, time)
    }

    // --- Lightning arcs (burst) ---
    if (params.isBurst) {
      this.renderArcs(params, timestamp)
    }

    // --- Particles ---
    this.particles.update()
    this.particles.render(ctx, params.colorRgb)

    // --- Aftermath sub-phases ---
    if (params.aftermathSubPhase >= 0 && !params.isBurst && scrollProgress > 0.80) {
      this.handleAftermath(params)
    }

    // --- Wordmark ---
    if (params.wordmarkOpacity > 0) {
      this.renderWordmark(params)
    }

    // --- Restore (undo shake) ---
    ctx.restore()

    // --- Flash decay ---
    if (this.flashIntensity > 0) {
      this.flashIntensity = Math.max(0, this.flashIntensity - 0.05) // ~3 frames to decay
    }

    // --- Glitch update ---
    const canGlitch = scrollProgress >= 0.20 && scrollProgress <= 0.65
    this.glitch = updateGlitch(this.glitch, canGlitch, width)
  }

  // ========== PRIVATE RENDER METHODS ==========

  private renderGlow(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, height, lineY } = config
    const [r, g, b] = params.colorRgb

    ctx.globalCompositeOperation = 'screen'

    if (params.isBurst) {
      // Full-screen flash on arc regeneration
      if (this.flashIntensity > 0) {
        ctx.fillStyle = `rgba(${r},${g},${b},${this.flashIntensity})`
        ctx.fillRect(0, 0, width, height)
      }
    } else if (config.useRadialGlow && params.glowRadius > 5) {
      // Per-peak radial gradient glow
      const wavelength = width / config.visibleCycles
      const freq = params.frequency || 0.8
      for (let cycle = 0; cycle < config.visibleCycles + 1; cycle++) {
        // R-peak is at phase 0.28 within each cycle
        const peakX = (0.28 + cycle) * wavelength - ((time * freq * wavelength) % wavelength)
        if (peakX < -params.glowRadius || peakX > width + params.glowRadius) continue

        const gradient = ctx.createRadialGradient(peakX, lineY, 0, peakX, lineY, params.glowRadius)
        gradient.addColorStop(0, `rgba(${r},${g},${b},${params.glowIntensity})`)
        gradient.addColorStop(0.4, `rgba(${r},${g},${b},${params.glowIntensity * 0.3})`)
        gradient.addColorStop(1, `rgba(${r},${g},${b},0)`)
        ctx.fillStyle = gradient
        ctx.fillRect(
          peakX - params.glowRadius, lineY - params.glowRadius,
          params.glowRadius * 2, params.glowRadius * 2,
        )
      }
    } else if (params.glowIntensity > 0.01) {
      // Simplified glow (mobile): single rect at lineY
      ctx.fillStyle = `rgba(${r},${g},${b},${params.glowIntensity * 0.5})`
      const h = params.glowRadius * 2
      ctx.fillRect(0, lineY - h / 2, width, h)
    }

    ctx.globalCompositeOperation = 'source-over'
  }

  private renderPulseLine(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, lineY, visibleCycles } = config
    const wavelength = width / visibleCycles

    ctx.beginPath()
    ctx.strokeStyle = params.color
    ctx.lineWidth = params.lineWidth
    ctx.shadowColor = params.color
    ctx.shadowBlur = 4

    let prevPhase = 0
    for (let x = 0; x <= width; x++) {
      const y = lineY + waveform(
        x, time, params.amplitude, params.frequency,
        params.noiseIntensity, wavelength, this.glitch,
      )

      if (x === 0) {
        ctx.moveTo(x, y)
      } else {
        ctx.lineTo(x, y)
      }

      // R-peak micro-swell: vary lineWidth at R peak
      const phase = ((x / wavelength + time * params.frequency) % 1 + 1) % 1
      if (isRPeak(phase) && !isRPeak(prevPhase)) {
        // Flush current path and start new segment with wider line
        ctx.stroke()
        ctx.beginPath()
        ctx.moveTo(x, y)
        ctx.lineWidth = rPeakSwell(phase, params.lineWidth)
      } else if (!isRPeak(phase) && isRPeak(prevPhase)) {
        // Return to normal width
        ctx.stroke()
        ctx.beginPath()
        ctx.moveTo(x, y)
        ctx.lineWidth = params.lineWidth
      }
      prevPhase = phase
    }
    ctx.stroke()
    ctx.shadowBlur = 0
  }

  private renderBranches(params: EnergyParams, time: number): void {
    const { ctx, config } = this
    const { width, lineY, visibleCycles } = config
    const wavelength = width / visibleCycles

    // Find wave peaks (R-peak locations) and spawn branches from them
    for (let cycle = 0; cycle < visibleCycles + 1; cycle++) {
      const peakX = (0.28 + cycle) * wavelength - ((time * params.frequency * wavelength) % wavelength)
      if (peakX < 0 || peakX > width) continue

      const peakY = lineY + waveform(
        peakX, time, params.amplitude, params.frequency,
        params.noiseIntensity, wavelength, this.glitch,
      )

      // Draw 1-2 short branch lines from this peak
      for (let b = 0; b < Math.min(2, params.branchCount); b++) {
        const angle = -Math.PI / 2 + (Math.random() - 0.5) * Math.PI * 0.8 // mostly upward
        const len = 20 + Math.random() * 40
        const endX = peakX + Math.cos(angle) * len
        const endY = peakY + Math.sin(angle) * len

        ctx.beginPath()
        ctx.strokeStyle = `rgba(${params.colorRgb[0]},${params.colorRgb[1]},${params.colorRgb[2]},0.4)`
        ctx.lineWidth = 0.5
        ctx.moveTo(peakX, peakY)
        ctx.lineTo(endX, endY)
        ctx.stroke()

        // Spawn particles at branch endpoint
        if (Math.random() < params.particleSpawnRate) {
          this.particles.spawnBuildup(endX, endY, angle)
        }
      }
    }
  }

  private renderArcs(params: EnergyParams, timestamp: number): void {
    const { ctx, config } = this

    // Regenerate arcs periodically
    if (timestamp - this.lastArcGenTime > LIGHTNING.regenerateIntervalMs) {
      this.lastArcGenTime = timestamp

      const targets = getWordmarkTargets(
        K2CC_PATH,
        config.width / 2,
        config.wordmarkY,
        config.wordmarkScale,
        config.maxArcCount,
      )

      this.currentArcs = generateAllArcs(
        config.lineY,
        config.wordmarkY,
        config.width,
        Math.min(params.arcCount, config.maxArcCount),
        Math.min(params.arcDepth, config.maxArcDepth),
        targets,
      )

      // Trigger flash
      this.flashIntensity = 0.05 + Math.random() * 0.10
    }

    // Render current arcs with per-frame opacity jitter
    for (const seg of this.currentArcs) {
      const opacity = seg.brightness * (0.7 + Math.random() * 0.3)
      ctx.beginPath()
      ctx.strokeStyle = `rgba(255,255,255,${opacity})`
      ctx.lineWidth = seg.lineWidth
      ctx.shadowColor = '#00ff88'
      ctx.shadowBlur = seg.depth === 0 ? 8 : 4
      ctx.moveTo(seg.x1, seg.y1)
      ctx.lineTo(seg.x2, seg.y2)
      ctx.stroke()
    }
    ctx.shadowBlur = 0
  }

  private handleAftermath(params: EnergyParams): void {
    const { aftermathSubPhase, aftermathLocalProgress } = params

    if (aftermathSubPhase === 0 && this.currentArcs.length > 0) {
      // Shatter: break arcs into segments, spawn shatter points
      if (this.shatterPoints.length === 0) {
        // Pick 3-5 random arc segment positions as shatter points
        const count = 3 + Math.floor(Math.random() * 3)
        for (let i = 0; i < count && i < this.currentArcs.length; i++) {
          const seg = this.currentArcs[Math.floor(Math.random() * this.currentArcs.length)]
          this.shatterPoints.push({
            x: (seg.x1 + seg.x2) / 2,
            y: (seg.y1 + seg.y2) / 2,
            spawned: false,
          })
        }
      }
      // Render remaining arc fragments with decreasing opacity
      const opacity = 1 - aftermathLocalProgress
      for (const seg of this.currentArcs) {
        this.ctx.beginPath()
        this.ctx.strokeStyle = `rgba(255,255,255,${opacity * seg.brightness * 0.5})`
        this.ctx.lineWidth = seg.lineWidth * 0.5
        this.ctx.moveTo(seg.x1, seg.y1)
        this.ctx.lineTo(seg.x2, seg.y2)
        this.ctx.stroke()
      }
    } else if (aftermathSubPhase === 1) {
      // Collapse: shatter points spawn particles
      for (const sp of this.shatterPoints) {
        if (!sp.spawned) {
          this.particles.spawnAftermath(sp.x, sp.y)
          sp.spawned = true
        }
      }
      // Clear arcs
      this.currentArcs = []
    } else if (aftermathSubPhase >= 2) {
      // Dissipate + reset: particles continue under physics (handled by pool.update)
      this.currentArcs = []
      this.shatterPoints = []
    }
  }

  private renderWordmark(params: EnergyParams): void {
    const { ctx, config } = this
    const [r, g, b] = params.colorRgb
    const opacity = params.wordmarkOpacity

    // Render k2cc text using Canvas text API as a simple fallback
    // (Path data rendering is used when K2CC_PATH has real data)
    ctx.save()
    ctx.globalAlpha = opacity
    ctx.font = `bold ${config.wordmarkScale * 80}px monospace`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'

    // Glow
    ctx.shadowColor = `rgba(0,255,136,${opacity})`
    ctx.shadowBlur = 6
    ctx.strokeStyle = `rgba(${r},${g},${b},${opacity})`
    ctx.lineWidth = 2
    ctx.strokeText('k2cc', config.width / 2, config.wordmarkY)

    // Fill
    ctx.fillStyle = `rgba(255,255,255,${opacity})`
    ctx.fillText('k2cc', config.width / 2, config.wordmarkY)

    ctx.shadowBlur = 0
    ctx.globalAlpha = 1
    ctx.restore()
  }
}
