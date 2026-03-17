import { BRAND_GREEN_RGB } from './constants'
import { ParticlePool } from './particles'
import type { RenderConfig } from './types'

/**
 * Whip-crack pulse animation:
 * 1. k2cc charges up on the left
 * 2. Cracks a whip — single traveling wave pulse left → right
 * 3. Wave hits right edge → full-screen "dawn flash" (white burst, fades back)
 * 4. Calm baseline → repeat
 *
 * No lightning. The climax is a clean white flash — heartbeat dawn.
 */

const CYCLE_SECONDS = 10

const PHASE = {
  CHARGE_START: 0,
  CHARGE_END: 0.12,
  WAVE_START: 0.12,
  WAVE_END: 0.50,
  FLASH_START: 0.50,   // dawn flash
  FLASH_PEAK: 0.52,    // max brightness
  FLASH_END: 0.70,     // fully faded back to dark
  // 0.70-1.0 = calm baseline before next cycle
}

export class PulseRenderer {
  private ctx: CanvasRenderingContext2D
  private config: RenderConfig
  private particles: ParticlePool
  private startTime = 0

  constructor(ctx: CanvasRenderingContext2D, config: RenderConfig) {
    this.ctx = ctx
    this.config = config
    this.particles = new ParticlePool(config.maxParticles)
  }

  updateConfig(config: RenderConfig): void {
    this.config = config
    if (config.maxParticles !== this.particles.particles.length) {
      this.particles = new ParticlePool(config.maxParticles)
    }
  }

  tick(timestamp: number, _progress: number): void {
    if (this.startTime === 0) this.startTime = timestamp
    const time = (timestamp - this.startTime) / 1000
    const cycleTime = time % CYCLE_SECONDS
    const phase = cycleTime / CYCLE_SECONDS
    const { ctx, config } = this

    // --- Clear ---
    ctx.save()
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height)
    ctx.restore()

    ctx.save()

    // --- Dawn flash (full-screen white burst) ---
    if (phase >= PHASE.FLASH_START && phase < PHASE.FLASH_END) {
      this.renderDawnFlash(phase)
    }

    // --- Baseline ---
    this.renderBaseline(phase)

    // --- k2cc origin ---
    this.renderK2cc(phase)

    // --- Traveling wave ---
    if (phase >= PHASE.WAVE_START && phase < PHASE.FLASH_END) {
      this.renderTravelingWave(phase)
    }

    // --- Particles ---
    this.particles.update()
    this.particles.render(ctx, BRAND_GREEN_RGB)

    ctx.restore()
  }

  // ========== PRIVATE ==========

  /**
   * Full-screen "dawn flash" — white burst from the right side impact point.
   * Rises fast (0.5→0.52), fades slow (0.52→0.70) with easing.
   */
  private renderDawnFlash(phase: number): void {
    const { ctx, config } = this
    const { width, height } = config

    let intensity: number

    if (phase < PHASE.FLASH_PEAK) {
      // Rise: fast ramp to peak
      const t = (phase - PHASE.FLASH_START) / (PHASE.FLASH_PEAK - PHASE.FLASH_START)
      intensity = t * t // easeIn — sudden punch
    } else {
      // Fade: slow decay back to dark
      const t = (phase - PHASE.FLASH_PEAK) / (PHASE.FLASH_END - PHASE.FLASH_PEAK)
      intensity = 1 - t * t // easeOut — lingers slightly then drops
    }

    if (intensity <= 0) return

    // The flash originates from the right side (wave impact point)
    // Use a radial gradient: bright at right-center, dimmer toward left
    const impactX = width * 0.98
    const impactY = config.lineY
    const flashRadius = Math.max(width, height) * 1.2

    ctx.globalCompositeOperation = 'screen'

    // Layer 1: White core from impact point
    const gradient = ctx.createRadialGradient(
      impactX, impactY, 0,
      impactX, impactY, flashRadius,
    )
    const coreAlpha = intensity * 0.35
    const edgeAlpha = intensity * 0.08
    gradient.addColorStop(0, `rgba(255,255,255,${coreAlpha})`)
    gradient.addColorStop(0.3, `rgba(200,255,220,${edgeAlpha})`)
    gradient.addColorStop(1, `rgba(0,255,136,0)`)
    ctx.fillStyle = gradient
    ctx.fillRect(0, 0, width, height)

    // Layer 2: Green tint wash (brand color bleeds through)
    if (intensity > 0.3) {
      const greenAlpha = (intensity - 0.3) * 0.1
      ctx.fillStyle = `rgba(0,255,136,${greenAlpha})`
      ctx.fillRect(0, 0, width, height)
    }

    ctx.globalCompositeOperation = 'source-over'
  }

  private renderBaseline(phase: number): void {
    const { ctx, config } = this
    const lineStartX = config.width * 0.12

    let opacity = 0.2

    if (phase < PHASE.CHARGE_END) {
      const t = phase / PHASE.CHARGE_END
      opacity = 0.2 + t * 0.15
    }
    // During flash, baseline dims
    if (phase >= PHASE.FLASH_START && phase < PHASE.FLASH_END) {
      const t = (phase - PHASE.FLASH_START) / (PHASE.FLASH_END - PHASE.FLASH_START)
      opacity = t < 0.3 ? 0.05 : 0.05 + (t - 0.3) / 0.7 * 0.15
    }

    ctx.beginPath()
    ctx.strokeStyle = `rgba(0,255,136,${opacity})`
    ctx.lineWidth = 1
    ctx.moveTo(lineStartX, config.lineY)
    ctx.lineTo(config.width, config.lineY)
    ctx.stroke()
  }

  private renderK2cc(phase: number): void {
    const { ctx, config } = this
    const x = config.width * 0.06
    const y = config.lineY
    const fontSize = config.width < 768 ? 22 : 32

    let opacity = 0.35
    let glowBlur = 4

    if (phase < PHASE.CHARGE_END) {
      const t = phase / PHASE.CHARGE_END
      opacity = 0.35 + t * 0.55
      glowBlur = 4 + t * 16
    } else if (phase < PHASE.WAVE_START + 0.03) {
      opacity = 0.9
      glowBlur = 20
    } else if (phase >= PHASE.FLASH_START && phase < PHASE.FLASH_PEAK + 0.05) {
      // During flash, k2cc also brightens briefly
      opacity = 0.7
      glowBlur = 12
    } else {
      opacity = 0.4
      glowBlur = 4
    }

    ctx.save()
    ctx.globalAlpha = opacity
    ctx.font = `bold ${fontSize}px monospace`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'

    ctx.shadowColor = '#00ff88'
    ctx.shadowBlur = glowBlur
    ctx.fillStyle = '#00ff88'
    ctx.fillText('k2cc', x, y)

    if (phase < PHASE.CHARGE_END + 0.03) {
      ctx.globalAlpha = opacity * 0.3
      ctx.shadowBlur = glowBlur * 2
      ctx.fillText('k2cc', x, y)
    }

    ctx.shadowBlur = 0
    ctx.globalAlpha = 1
    ctx.restore()
  }

  private renderTravelingWave(phase: number): void {
    const { ctx, config } = this
    const lineStartX = config.width * 0.12
    const lineEndX = config.width
    const totalLineWidth = lineEndX - lineStartX

    let wavePos: number
    if (phase < PHASE.WAVE_END) {
      const t = (phase - PHASE.WAVE_START) / (PHASE.WAVE_END - PHASE.WAVE_START)
      wavePos = 1 - Math.pow(1 - t, 2) // easeOutQuad — whip crack feel
    } else {
      wavePos = 1
    }

    const waveCenterX = lineStartX + wavePos * totalLineWidth
    const envelopeWidth = 40 + wavePos * 60
    const baseAmplitude = 45 + (1 - wavePos) * 35

    // Fade out after flash
    let waveOpacity = 1.0
    if (phase >= PHASE.FLASH_START) {
      const fadeT = (phase - PHASE.FLASH_START) / (PHASE.FLASH_END - PHASE.FLASH_START)
      waveOpacity = Math.max(0, 1 - fadeT * 2)
    }
    if (waveOpacity <= 0) return

    ctx.beginPath()
    ctx.strokeStyle = `rgba(0,255,136,${waveOpacity})`
    ctx.lineWidth = 2
    ctx.shadowColor = '#00ff88'
    ctx.shadowBlur = 6

    const step = 2
    for (let x = lineStartX; x <= lineEndX; x += step) {
      const distFromCenter = x - waveCenterX
      const envelope = Math.exp(-(distFromCenter * distFromCenter) / (2 * envelopeWidth * envelopeWidth))
      const wave = Math.sin(distFromCenter * 0.08) * envelope * baseAmplitude
      const y = config.lineY + wave

      if (x === lineStartX) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()
    ctx.shadowBlur = 0

    // Glow at wave center
    if (waveOpacity > 0.3) {
      ctx.globalCompositeOperation = 'screen'
      const glowR = 30 + wavePos * 40
      const gradient = ctx.createRadialGradient(waveCenterX, config.lineY, 0, waveCenterX, config.lineY, glowR)
      gradient.addColorStop(0, `rgba(0,255,136,${waveOpacity * 0.12})`)
      gradient.addColorStop(1, 'rgba(0,255,136,0)')
      ctx.fillStyle = gradient
      ctx.fillRect(waveCenterX - glowR, config.lineY - glowR, glowR * 2, glowR * 2)
      ctx.globalCompositeOperation = 'source-over'
    }

    // Spawn particles at wave front as it approaches right edge
    if (wavePos > 0.85 && wavePos < 1.0 && Math.random() < 0.3) {
      this.particles.spawnBuildup(waveCenterX, config.lineY, -Math.PI / 2 + Math.random() * Math.PI)
    }
  }
}
