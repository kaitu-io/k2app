import { LIGHTNING } from './constants'
import type { BoltSegment } from './types'

interface Point {
  x: number
  y: number
}

function midpoint(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
}

function distance(a: Point, b: Point): number {
  const dx = b.x - a.x
  const dy = b.y - a.y
  return Math.sqrt(dx * dx + dy * dy)
}

function angle(a: Point, b: Point): number {
  return Math.atan2(b.y - a.y, b.x - a.x)
}

/**
 * Generate a single lightning bolt path using midpoint displacement.
 * Returns array of line segments with varying lineWidth and brightness.
 */
export function generateBolt(
  start: Point,
  end: Point,
  depth: number,
  displacement: number,
  lineWidth: number,
  brightness: number,
  branchDepth: number,
  segments: BoltSegment[],
): void {
  if (depth <= 0) {
    segments.push({
      x1: start.x, y1: start.y,
      x2: end.x, y2: end.y,
      lineWidth,
      brightness,
      depth: branchDepth,
    })
    return
  }

  const mid = midpoint(start, end)
  mid.x += (Math.random() * 2 - 1) * displacement
  mid.y += (Math.random() * 2 - 1) * displacement

  generateBolt(start, mid, depth - 1, displacement * LIGHTNING.decayFactor, lineWidth, brightness, branchDepth, segments)
  generateBolt(mid, end, depth - 1, displacement * LIGHTNING.decayFactor, lineWidth, brightness, branchDepth, segments)

  // Branching
  if (branchDepth < LIGHTNING.maxBranchDepth) {
    const prob = branchDepth === 0
      ? LIGHTNING.trunkBranchProb
      : branchDepth === 1
        ? LIGHTNING.level1BranchProb
        : LIGHTNING.level2BranchProb

    if (Math.random() < prob) {
      const dir = angle(start, end)
      const branchAngle = dir + (Math.random() > 0.5 ? 1 : -1) *
        ((LIGHTNING.branchAngleMin + Math.random() * (LIGHTNING.branchAngleMax - LIGHTNING.branchAngleMin)) * Math.PI / 180)
      const len = distance(start, end) *
        (LIGHTNING.branchLengthMin + Math.random() * (LIGHTNING.branchLengthMax - LIGHTNING.branchLengthMin))

      const branchEnd: Point = {
        x: mid.x + Math.cos(branchAngle) * len,
        y: mid.y + Math.sin(branchAngle) * len,
      }

      generateBolt(
        mid,
        branchEnd,
        Math.max(depth - 2, 1),
        displacement * 0.5,
        lineWidth * 0.6,
        brightness * 0.7,
        branchDepth + 1,
        segments,
      )
    }
  }
}

/**
 * Generate all arcs for the burst phase.
 * Origins are spread along the pulse line, targets are on the k2cc wordmark.
 */
export function generateAllArcs(
  lineY: number,
  wordmarkY: number,
  viewportWidth: number,
  arcCount: number,
  arcDepth: number,
  wordmarkTargets: Point[],
): BoltSegment[] {
  const segments: BoltSegment[] = []
  const originXStart = viewportWidth * 0.3
  const originXEnd = viewportWidth * 0.7
  const step = arcCount > 1 ? (originXEnd - originXStart) / (arcCount - 1) : 0

  for (let i = 0; i < arcCount; i++) {
    const originX = originXStart + step * i + (Math.random() - 0.5) * 20
    const origin: Point = { x: originX, y: lineY }
    const target = wordmarkTargets[i % wordmarkTargets.length]
    const displacement = 20 + Math.random() * 40

    generateBolt(origin, target, arcDepth, displacement, 2.5, 1.0, 0, segments)
  }

  return segments
}

/**
 * Get sample points along the k2cc wordmark for arc targets.
 * Takes the raw path data and scales/positions it.
 */
export function getWordmarkTargets(
  pathData: Array<[number, number][]>,
  centerX: number,
  centerY: number,
  scale: number,
  count: number,
): Point[] {
  // Flatten all path points, scale, and evenly sample
  const allPoints: Point[] = []
  const totalWidth = 100 * scale // path is in 0-100 space
  const startX = centerX - totalWidth / 2

  for (const charPath of pathData) {
    for (const [px, py] of charPath) {
      allPoints.push({
        x: startX + px * scale,
        y: centerY + (py - 50) * scale, // center vertically
      })
    }
  }

  if (allPoints.length === 0) {
    // Fallback: evenly spaced points in wordmark area
    const targets: Point[] = []
    for (let i = 0; i < count; i++) {
      targets.push({
        x: centerX - totalWidth / 2 + (totalWidth * i) / (count - 1),
        y: centerY,
      })
    }
    return targets
  }

  // Evenly sample from available points
  const targets: Point[] = []
  const sampleStep = Math.max(1, Math.floor(allPoints.length / count))
  for (let i = 0; i < count && i * sampleStep < allPoints.length; i++) {
    targets.push(allPoints[i * sampleStep])
  }
  return targets
}
