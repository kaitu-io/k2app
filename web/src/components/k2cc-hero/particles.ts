import { PARTICLE } from './constants'
import type { Particle } from './types'

export class ParticlePool {
  particles: Particle[]
  activeCount: number

  constructor(maxSize: number) {
    this.particles = Array.from({ length: maxSize }, () => ({
      x: 0, y: 0, vx: 0, vy: 0,
      life: 0, decay: 0, size: 0, brightness: 0, active: false,
    }))
    this.activeCount = 0
  }

  spawn(x: number, y: number, vx: number, vy: number, size: number, life: number, brightness: number): void {
    // Find inactive slot
    for (const p of this.particles) {
      if (!p.active) {
        p.x = x; p.y = y
        p.vx = vx; p.vy = vy
        p.size = size
        p.life = 1.0
        p.decay = 1.0 / life  // die after `life` frames
        p.brightness = brightness
        p.active = true
        this.activeCount++
        return
      }
    }
    // Pool full — skip
  }

  spawnBuildup(x: number, y: number, dirAngle: number): void {
    const count = 2 + Math.floor(Math.random() * 3)
    for (let i = 0; i < count; i++) {
      const angle = dirAngle + (Math.random() - 0.5) * (Math.PI / 3) // +/-30deg
      const speed = PARTICLE.buildupSpeed[0] + Math.random() * (PARTICLE.buildupSpeed[1] - PARTICLE.buildupSpeed[0])
      const size = PARTICLE.buildupSize[0] + Math.random() * (PARTICLE.buildupSize[1] - PARTICLE.buildupSize[0])
      const life = PARTICLE.buildupLife[0] + Math.random() * (PARTICLE.buildupLife[1] - PARTICLE.buildupLife[0])
      this.spawn(x, y, Math.cos(angle) * speed, Math.sin(angle) * speed, size, life, 0.8)
    }
  }

  spawnAftermath(x: number, y: number): void {
    const count = 5 + Math.floor(Math.random() * 4)
    for (let i = 0; i < count; i++) {
      const angle = Math.random() * Math.PI * 2
      const speed = PARTICLE.aftermathSpeed[0] + Math.random() * (PARTICLE.aftermathSpeed[1] - PARTICLE.aftermathSpeed[0])
      const size = PARTICLE.aftermathSize[0] + Math.random() * (PARTICLE.aftermathSize[1] - PARTICLE.aftermathSize[0])
      const life = PARTICLE.aftermathLife[0] + Math.random() * (PARTICLE.aftermathLife[1] - PARTICLE.aftermathLife[0])
      this.spawn(x, y, Math.cos(angle) * speed, Math.sin(angle) * speed, size, life, 1.0)
    }
  }

  update(): void {
    for (const p of this.particles) {
      if (!p.active) continue

      p.x += p.vx
      p.y += p.vy
      p.vy += PARTICLE.gravity
      p.vx *= PARTICLE.damping
      p.vy *= PARTICLE.damping
      p.life -= p.decay

      if (p.life <= 0) {
        p.active = false
        this.activeCount--
      }
    }
  }

  render(ctx: CanvasRenderingContext2D, colorRgb: [number, number, number]): void {
    ctx.globalCompositeOperation = 'screen'
    for (const p of this.particles) {
      if (!p.active) continue
      const radius = p.size * p.life
      if (radius < 0.1) continue

      ctx.beginPath()
      ctx.arc(p.x, p.y, radius, 0, Math.PI * 2)
      ctx.fillStyle = `rgba(${colorRgb[0]},${colorRgb[1]},${colorRgb[2]},${p.life * p.brightness})`
      ctx.fill()
    }
    ctx.globalCompositeOperation = 'source-over'
  }

  clear(): void {
    for (const p of this.particles) {
      p.active = false
    }
    this.activeCount = 0
  }
}
