import type { BoltSegment } from './types';

interface Point { x: number; y: number; }

function midpoint(a: Point, b: Point): Point {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

export function generateBolt(
  a: Point,
  b: Point,
  depth: number,
  displacement: number,
  width: number,
  brightness: number,
): BoltSegment[] {
  if (depth === 0) {
    return [{ x1: a.x, y1: a.y, x2: b.x, y2: b.y, width, brightness, depth: 0 }];
  }

  const mid = midpoint(a, b);
  mid.x += (Math.random() * 2 - 1) * displacement;
  mid.y += (Math.random() * 2 - 1) * displacement;

  const left = generateBolt(a, mid, depth - 1, displacement * 0.55, width, brightness);
  const right = generateBolt(mid, b, depth - 1, displacement * 0.55, width, brightness);

  const segments = [...left, ...right];

  const branchProb = depth >= 5 ? 0.35 : depth >= 3 ? 0.18 : 0.08;
  if (depth > 1 && Math.random() < branchProb) {
    const angle = (Math.atan2(b.y - a.y, b.x - a.x)) + (Math.random() * 60 - 30) * Math.PI / 180;
    const len = Math.sqrt((b.x - a.x) ** 2 + (b.y - a.y) ** 2) * (0.3 + Math.random() * 0.3);
    const branchEnd: Point = {
      x: mid.x + Math.cos(angle) * len,
      y: mid.y + Math.sin(angle) * len,
    };
    const branchSegs = generateBolt(mid, branchEnd, depth - 2, displacement * 0.4, width * 0.6, brightness * 0.7);
    segments.push(...branchSegs);
  }

  return segments;
}

export function generateArcs(
  lineY: number,
  viewportWidth: number,
  wordmarkY: number,
  arcCount: number,
  arcDepth: number,
): BoltSegment[][] {
  const arcs: BoltSegment[][] = [];
  const startX = viewportWidth * 0.3;
  const endX = viewportWidth * 0.7;
  const step = (endX - startX) / Math.max(arcCount - 1, 1);

  for (let i = 0; i < arcCount; i++) {
    const originX = startX + step * i + (Math.random() * 20 - 10);
    const targetX = startX + step * i + (Math.random() * 30 - 15);

    const origin: Point = { x: originX, y: lineY };
    const target: Point = { x: targetX, y: wordmarkY };

    const displacement = 20 + Math.random() * 40;
    const segments = generateBolt(origin, target, arcDepth, displacement, 2, 1);
    arcs.push(segments);
  }

  return arcs;
}
