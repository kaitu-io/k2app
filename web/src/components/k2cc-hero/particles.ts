import type { Particle } from './types';

export function createParticlePool(size: number): Particle[] {
  return Array.from({ length: size }, () => ({
    x: 0, y: 0, vx: 0, vy: 0,
    life: 0, decay: 0, size: 0, brightness: 0,
    active: false,
  }));
}

export function spawnParticle(
  pool: Particle[],
  x: number,
  y: number,
  speed: number,
  size: number,
  decay: number,
): boolean {
  const slot = pool.find((p) => !p.active);
  if (!slot) return false;

  const angle = Math.random() * Math.PI * 2;
  slot.x = x;
  slot.y = y;
  slot.vx = Math.cos(angle) * speed;
  slot.vy = Math.sin(angle) * speed;
  slot.life = 1.0;
  slot.decay = decay;
  slot.size = size;
  slot.brightness = 1.0;
  slot.active = true;
  return true;
}

export function updateParticles(pool: Particle[]): void {
  for (const p of pool) {
    if (!p.active) continue;
    p.x += p.vx;
    p.y += p.vy;
    p.vy += 0.02;
    p.vx *= 0.98;
    p.vy *= 0.98;
    p.life -= p.decay;
    p.brightness = p.life;
    if (p.life <= 0) {
      p.active = false;
    }
  }
}

function hexToRgb(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `${r},${g},${b}`;
}

export function renderParticles(
  ctx: CanvasRenderingContext2D,
  pool: Particle[],
  color: string,
): void {
  ctx.globalCompositeOperation = 'screen';
  for (const p of pool) {
    if (!p.active) continue;
    const r = p.size * p.life;
    if (r < 0.1) continue;
    ctx.beginPath();
    ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
    ctx.fillStyle = `rgba(${hexToRgb(color)}, ${p.brightness * 0.8})`;
    ctx.fill();
  }
  ctx.globalCompositeOperation = 'source-over';
}
