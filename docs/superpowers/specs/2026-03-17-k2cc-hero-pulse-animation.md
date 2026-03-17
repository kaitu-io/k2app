# k2cc Full-Page Pulse Hero Animation — Design Spec

## Overview

A single glowing pulse line runs as a fixed Canvas background layer across the entire homepage. Driven by scroll progress, it follows a six-beat dramatic energy curve — from a calm ECG heartbeat, through interference and silence, to a full-screen Tesla coil discharge that lights up the k2cc wordmark. The entire page background is "infected" by the pulse's glow, breathing with each heartbeat.

Zero external dependencies. Pure Canvas 2D + Web Audio API. All waveforms, lightning, and sound generated algorithmically.

## Architecture

### Layer Model

```
┌─ Fixed Canvas Layer (z-index: 0) ─────────────────────────────┐
│  position: fixed; inset: 0; pointer-events: none              │
│  100vw × 100vh, devicePixelRatio scaled                       │
│  Body background: #050508                                      │
│                                                                │
│  Renders:                                                      │
│    1. Glow diffusion (screen blend)                            │
│    2. Main pulse line (full width, Y=40vh)                     │
│    3. Branch lines (buildup phase)                             │
│    4. Lightning arcs (burst phase)                             │
│    5. Particles (buildup + aftermath)                          │
│    6. k2cc wordmark (burst + aftermath)                        │
│                                                                │
│  Driver: scrollProgress (0→1) + time (auto-pulsation)         │
└────────────────────────────────────────────────────────────────┘

┌─ Content Layer (z-index: 1, normal document flow) ────────────┐
│  Section 1: Hero          (100vh)                             │
│  Section 2: Feature Cards (auto)                              │
│  Section 3: Comparison    (auto)                              │
│  Section 4: Download/CTA  (auto)                              │
│                                                                │
│  scrollProgress = scrollY / (scrollHeight - viewportHeight)   │
│  Beat boundaries map to absolute scrollProgress values.       │
│  Sections have variable height — energy beats are NOT tied    │
│  to section boundaries, they follow the raw scroll position.  │
│                                                                │
│  All sections: semi-transparent bg + backdrop-blur             │
│  Canvas bleeds through → "infection" preserved                 │
└────────────────────────────────────────────────────────────────┘
```

### Scroll Progress

```
rawProgress = scrollY / (scrollHeight - viewportHeight)
smoothProgress = lerp(currentSmooth, rawProgress, 0.08)  // per-frame interpolation
```

All energy parameters interpolated with `easeInOutCubic` between beats — no hard cuts.

Desktop: scrollProgress drives energy. When user stops scrolling, current phase auto-pulsates.
Mobile: same scrollProgress driver, reduced render quality. On iOS Safari, `window.scrollY` is polled inside the rAF loop (which updates during momentum scroll on modern iOS), rather than relying on scroll events that throttle during inertial scrolling.

### Canvas Resize Handling

Viewport resize (window resize, orientation change, mobile toolbar show/hide) must update canvas internal resolution:

```
ResizeObserver on canvas element (debounced 100ms):
  const rect = canvas.getBoundingClientRect()
  const dpr = Math.min(window.devicePixelRatio, maxDpr)
  canvas.width = rect.width * dpr
  canvas.height = rect.height * dpr
  ctx.scale(dpr, dpr)
  // Recalculate lineY, glowRadius, wordmark position from new dimensions
```

## Energy Curve — Six Beats

Dramatic arc, not linear ramp. Includes a false climax and a silence before the real burst.

```
Energy
  │                                ╱╲  Burst
  │                              ╱    ╲
  │                            ╱       ╲
  │         ╭──╮             ╱          ╲  Aftermath
  │        ╱    ╲          ╱              ╲
  │      ╱ Sense  ╲      ╱  Buildup        ╲
  │    ╱            ╲  ╱                     ╲──── Reset
  │──╱   Rest        ╲╱ Silence
  └──────────────────────────────────────────────
  0.0    0.2   0.35  0.45   0.65   0.80   1.0
```

### Parameter Table

| Beat | scrollProgress | Name | Amplitude | Freq | Glow Radius | Color | Special |
|------|---------------|------|-----------|------|-------------|-------|---------|
| 1 | 0.00–0.20 | Rest | 30px | 0.8Hz | 15px | #00ff88 | None |
| 2 | 0.20–0.35 | Sense | 80px | 2.0Hz | 80px | #00ff88 | Perlin noise, glitch |
| 3 | 0.35–0.45 | Silence | 12px | 0.4Hz | 5px | #005533 | Darker than rest |
| 4 | 0.45–0.65 | Buildup | 150px | 3.5Hz | 200px | #00ff88→white | 2-4 branches, particles |
| 5 | 0.65–0.80 | Burst | fullscreen | — | fullscreen | #ffffff | Recursive arcs, sound, shake |
| 6 | 0.80–1.00 | Aftermath→Reset | decaying | decaying | decaying | white→#00ff88 | Arc shatter→particles→silence |

### Beat Details

**Beat 1 — Rest (0.00–0.20)**
- 1.5px line, classic PQRST ECG waveform, period ~1.2s
- `#00ff88` + 4px gaussian blur glow
- Glow diffuses 15px up/down, background brightens with each peak
- R-peak micro-swell: lineWidth 1.5→2.5→1.5px over 2 frames ("thump" feel)

**Beat 2 — Sense (0.20–0.35)**
- Amplitude doubles, frequency increases
- Perlin noise layered onto waveform — no longer smooth
- Random glitch: 0.3% chance per frame, 80-200px horizontal tear, ±5-15px displacement, 2-3 frames, 60-120 frame cooldown
- Glow radius expands to 80px

**Beat 3 — Silence (0.35–0.45)**
- Amplitude and color drop **below rest level** — the calm before the storm
- 12px amplitude, 0.4Hz, color dims to `#005533`
- Glow nearly invisible (5px, opacity 0.02)
- User briefly thinks the animation is winding down

**Beat 4 — Buildup (0.45–0.65)**
- Amplitude surges to 150px
- Branch lines split from wave peaks: 0.5px, dimmer, short-lived
- Color transitions from brand green toward white
- Small particles emit from branch endpoints (see Particle System)
- Canvas micro-displacement ±1px (low-frequency tremor)

**Beat 5 — Burst (0.65–0.80)**
- Main pulse line disappears, replaced by recursive fractal lightning arcs
- Arcs fire **upward** from pulse line, striking k2cc wordmark above
- k2cc lights up progressively: sparse hits at 0.65 → fully lit at 0.75
- Full-screen glow, background flashes to `#0a1a0f`
- Screen shake ±2px, 200ms
- Sound triggers when scrollProgress first crosses 0.70 while increasing (user scrolling down)

**Beat 6 — Aftermath→Reset (0.80–1.00)**
- 0.80–0.88: Arc trunks shatter into 3-5 segments, spark particles at break points
- 0.88–0.93: Segments collapse to points, each bursts into 5-8 particles with damped motion
- 0.93–0.96: Particles dissipate, k2cc opacity 1.0→0.0
- 0.96–1.00: Back to 1.5px green rest line, mathematically continuous with Beat 1 waveform

## Waveform Generation

All waveforms computed in real-time from math functions. No pre-recorded data.

### PQRST Template

```
Normalized t ∈ [0, 1] per cycle:

P wave:  0.15 × sin(π × t / 0.12)                    t ∈ [0.00, 0.12]
PQ seg:  0                                            t ∈ [0.12, 0.20]
QRS:     Simplified visual approximation (not clinical):
         -0.1 × gaussian(t, 0.22, 0.01)               t ∈ [0.20, 0.24]  Q dip
         + 1.2 × gaussian(t, 0.28, 0.015)             t ∈ [0.24, 0.32]  R spike
         - 0.15 × gaussian(t, 0.33, 0.01)             t ∈ [0.32, 0.36]  S dip
ST seg:  0                                            t ∈ [0.36, 0.50]
T wave:  0.3 × sin(π × (t-0.50) / 0.18)              t ∈ [0.50, 0.68]
Baseline: 0                                           t ∈ [0.68, 1.00]
```

### Synthesis Pipeline

```
waveform(x, time, scrollProgress) =
    pqrst(phase) × amplitude(scrollProgress)
  + perlinNoise(x, time) × noiseIntensity(scrollProgress)
  + glitch(x, time, scrollProgress)

phase = (x / wavelength + time × frequency) mod 1.0
wavelength = viewportWidth / visibleCycles  // 2-3 visible cycles on screen
```

### Perlin Noise

Inline simplified 1D Perlin (no library). Input: `x × 0.01 + time × 2.0`. Output: `[-1, 1]`.

### Glitch System

State machine: `idle → triggered → active → cooldown → idle`. Trigger: 0.3% per frame when scrollProgress ∈ [0.20, 0.65]. Duration: 2-3 frames. Effect: horizontal displacement ±5-20px over 80-200px width. Cooldown: 60-120 frames.

### R-Peak Micro-Swell

At phase ≈ 0.28 (R peak): `lineWidth = baseWidth + 1.0 × smoothstep(0.26, 0.28, phase) × smoothstep(0.30, 0.28, phase)`. Produces a 2-frame "thump" on every heartbeat across all beats.

## Lightning Arc Algorithm (Tesla Burst)

### Midpoint Displacement

```
function generateBolt(A, B, depth, displacement):
    if depth === 0: return [A, B]
    mid = midpoint(A, B)
    mid.x += random(-1, 1) × displacement
    mid.y += random(-1, 1) × displacement
    left  = generateBolt(A, mid, depth-1, displacement × 0.55)
    right = generateBolt(mid, B, depth-1, displacement × 0.55)
    return [...left, ...right]
```

Decay factor 0.55 (not 0.5) — sub-branches more erratic than parent, more realistic.

### Branching

At each midpoint, probability to spawn a child arc:
- Trunk: 35%
- Level 1: 18%
- Level 2: 8%
- Max branch depth: 3

Branch direction: parent direction ± 20°-50° (random). Branch length: parent × 0.3-0.6. Branch lineWidth: parent × 0.6. Branch brightness: parent × 0.7.

**Paths regenerated every frame** — critical for lightning instability feel. Trunk endpoints stay fixed (directional stability).

### Arc Layout

```
         k 2 c c              ← wordmark sample points (predefined)
        ↗↑  ↑  ↑↖
       ⚡ ⚡  ⚡  ⚡ ⚡           ← 6-10 trunk arcs
      ↗  ↑  ↑  ↑  ↖
─────╱───────────────────     ← pulse line Y=40vh

Origins: 6-10 points evenly along pulse line (x ∈ 30%-70% viewport)
Targets: sample points on k2cc wordmark outline
```

Per-trunk arc: displacement 20-60px, depth 5-7, regenerate every 80-150ms, per-frame opacity jitter 0.7-1.0.

### k2cc Wordmark

Source: monospace font "k2cc" extracted as SVG path data. Hardcoded in `constants.ts` (no runtime font parsing).

Position: 20-25vh from top, horizontally centered. Size: desktop ~120px, mobile ~60px.

Light-up: 0.65 sparse arc hits → 0.70 partial → 0.75 full. Remains fully lit at opacity 1.0 from 0.75 through 0.93, then fades to 0 during 0.93-0.96. When lit: 2px #ffffff stroke + 6px #00ff88 glow. Small arcs continuously discharge along outline.

### Performance Budget

```
10 trunks × 7 depth × 1.5 avg branches = ~1900 segments/frame
Canvas lineTo ×1900 ≈ 0.3ms — well within 16ms budget

Degraded: 4 trunks × 5 depth = ~400 segments/frame
```

## Glow Diffusion System (Background "Infection")

### Light Source Model

Each PQRST R-peak = point light source. Intensity = amplitude × energyLevel. Color = current phase color.

### Rendering

Per visible R-peak (usually 2-3 on screen):

```
ctx.globalCompositeOperation = 'screen'
gradient = ctx.createRadialGradient(peakX, lineY, 0, peakX, lineY, glowRadius)
gradient.addColorStop(0, rgba(color, intensity))        // center
gradient.addColorStop(0.4, rgba(color, intensity × 0.3))
gradient.addColorStop(1, rgba(color, 0))                // edge
ctx.fillStyle = gradient
ctx.fillRect(peakX - glowRadius, lineY - glowRadius, glowRadius × 2, glowRadius × 2)
```

Intensity range: 0.03 (rest) → 0.20 (burst).

### Burst Full-Screen Flash

During burst (0.65-0.80), each arc regeneration triggers:

```
ctx.globalCompositeOperation = 'screen'
ctx.fillStyle = rgba(#00ff88, random(0.05, 0.15))
ctx.fillRect(0, 0, width, height)
// Decays to 0 over next 3 frames (50ms)
```

### Aftermath Glow Contraction

scrollProgress 0.80-1.00: glow shrinks from fullscreen to 15px using `easeOutCubic`:

```
progress = (sp - 0.80) / 0.20
eased = 1 - pow(1 - progress, 3)   // easeOutCubic
currentGlow = lerp(fullScreenRadius, 15, eased)
```

Fast contraction for first 70%, slow settle for last 30% — energy drawn back into the line.

## Particle System

Particles serve narrative: buildup (foreshadowing) and aftermath (dissipation). Not decoration.

### Data Structure

```typescript
interface Particle {
  x: number; y: number
  vx: number; vy: number
  life: number       // 1.0 (born) → 0.0 (dead)
  decay: number      // per-frame decay (0.01-0.04)
  size: number       // radius px
  brightness: number // 0-1
}
```

### Buildup Particles (scrollProgress 0.45-0.65)

Source: branch line endpoints. Spawn: 2-4 per branch death. Direction: branch direction ± 30°. Speed: 1-3 px/frame. Size: 1-2px. Life: 30-60 frames. Pool: desktop 20, mobile 10.

### Aftermath Particles (scrollProgress 0.80-0.93)

Source: arc shatter points. Each shattered segment collapses to a point over 200ms, then bursts into 5-8 particles. Direction: 360° random from shatter point. Speed: 2-5 px/frame. Damping: ×0.96/frame. Size: 2-3px → shrinks with life. Color: #ffffff → #00ff88 → transparent. Life: 40-80 frames. Pool: desktop 50, mobile 20.

### Update Loop

```
per particle per frame:
  x += vx; y += vy
  vy += 0.02          // micro-gravity (downward drift)
  vx *= 0.98; vy *= 0.98  // damping
  life -= decay
  size = initialSize × life
  // render as circle with screen blend
  // reclaim when life ≤ 0
```

### Object Pool

Pre-allocated fixed array. Buildup and aftermath phases don't overlap, so a single shared pool works. Desktop: 50 slots (max of the two phases), mobile: 20. activeCount pointer. Dead particles marked inactive, slots reused. No GC pressure.

## Sound Design (Web Audio API, Algorithmic)

All sound generated procedurally. Zero audio files. Every trigger sounds slightly different due to randomization.

### Structure (800ms total)

```
0ms         100ms        300ms        500ms        800ms
│            │            │            │            │
├─ Attack ───┤            │            │            │
│ White noise │            │            │            │
│ burst 20ms  │            │            │            │
│            ├─ Crackle ──┤            │            │
│            │ 3-5 random  │            │            │
│            │ square pulses│           │            │
├──────────── Body (60Hz+120Hz sine) ──┤            │
│                                      ├── Tail ───┤
├──────── Sub Bass (35Hz sine) ────────┤  fade out  │
```

### Four Layers

**Layer 1 — Attack:** AudioBuffer (882 samples of `random(-1,1)` at 44.1kHz = 20ms) → BiquadFilter (highpass 2000Hz) → GainNode. Envelope: 0→0.6 in 2ms, 0.6→0 in 18ms. Regenerated each trigger (different noise every time).

**Layer 2 — Body:** Two OscillatorNodes (60Hz + 120Hz sine, 120Hz at 0.4× volume) → GainNode. Envelope: 0.3→0.2 over 200ms→0 at 500ms.

**Layer 3 — Crackle:** 3-5 independent micro-pulses. Each: OscillatorNode (random 1000-4000Hz, square wave), 5-15ms duration, gain 0.15-0.25. Randomly distributed across 50-250ms window, minimum 30ms spacing.

**Layer 4 — Sub Bass:** OscillatorNode (35Hz sine) → GainNode. Nearly inaudible but physically felt. Envelope: 0.4→0.2 at 100ms→0 at 400ms.

### Master Output

All layers → masterGain (value: 0.12) → destination. Extremely quiet — felt, not heard.

### Trigger Logic

```
if (scrollProgress >= 0.70 && prevProgress < 0.70 && !hasPlayed) {
  playBurstSound()
  hasPlayed = true
}
if (scrollProgress < 0.30) {
  hasPlayed = false  // reset when user scrolls back to rest phase, allowing re-trigger
}
```

### Activation Policy

- No AudioContext created on page load (browser autoplay policy)
- Created on first user interaction (click/scroll)
- iOS Safari: `ctx.resume()` on first touchstart
- `prefers-reduced-motion`: never created
- No UI volume control (volume too low to warrant one)

## Responsive Adaptation

### Desktop (≥1024px)

Full quality. Canvas at native resolution × devicePixelRatio. 6-10 arcs, 50-particle pool, radialGradient glow per peak, backdrop-filter blur on content cards. k2cc wordmark 120px. 3 visible waveform cycles.

### Tablet (768–1023px)

4-6 arcs, 30-particle pool. Otherwise same as desktop.

### Mobile (<768px)

- Canvas resolution capped at `× min(devicePixelRatio, 2)`
- 3-4 arcs, recursion depth 5 (desktop: 7), 20-particle pool
- Glow: single full-width semi-transparent rect (no per-peak radialGradient)
- Content cards: `rgba(5,5,8,0.85)` solid bg, no backdrop-filter
- k2cc wordmark 60px
- 2 visible waveform cycles

### Auto-Degradation

First 60 frames measure actual frame time. If `avgFrameTime > 20ms`: halve particles, disable glow gradients, reduce arc recursion by 2 levels.

### Reduced Motion

`prefers-reduced-motion: reduce` → static mode: single still green line + k2cc wordmark with faint glow. No animation, no sound.

### Visibility

`document.hidden` or IntersectionObserver → pause rAF loop. Resume on visibility restore.

## Render Pipeline (Per-Frame Order)

```
function tick(timestamp):
  1. Compute deltaTime, scrollProgress (lerp smoothed)
  2. Look up energy curve → current beat parameters

  3. Clear canvas at identity transform (avoids shake ghosting):
     ctx.save()
     ctx.setTransform(1, 0, 0, 1, 0, 0)
     ctx.clearRect(0, 0, canvas.width, canvas.height)
     ctx.restore()

  4. Apply screen shake: ctx.save(); ctx.translate(shakeX, shakeY)

  5. Render glow layer (screen blend)
     - Rest→Buildup: per-peak radial gradients
     - Burst: full-screen flash overlay

  6. Render main pulse line
     - Iterate x=0→viewportWidth, step 1px
     - y = lineY + waveform(x, time, scrollProgress)
     - ctx.lineTo(x, y); ctx.stroke()

  7. Render branch lines (buildup phase)
     - Short polylines from wave peaks
     - Spawn particles at branch endpoints

  8. Render lightning arcs (burst phase)
     - generateBolt() × N trunks
     - Independent stroke per trunk + branches
     - k2cc wordmark path overlaid with arc discharges

  9. Render particles
     - Update physics → draw → reclaim dead

  10. Render k2cc wordmark (burst + aftermath)
      - Stroke wordmark path with lineWidth + glow

  11. ctx.restore() (undo screen shake)

  12. Check sound trigger condition

  13. requestAnimationFrame(tick)
```

## File Structure

```
web/src/components/k2cc-hero/
  K2ccPulseCanvas.tsx        # Main Canvas component (fixed layer, 'use client')
  useScrollProgress.ts       # Scroll progress hook (lerp, direction detection)
  useAudioBurst.ts           # Web Audio sound hook (lazy AudioContext)
  renderer.ts                # Main render loop (tick function, React-independent)
  waveform.ts                # PQRST template + Perlin noise + glitch
  lightning.ts               # Midpoint displacement + branching
  particles.ts               # Object pool + physics update
  energy.ts                  # Six-beat energy curve + parameter interpolation
  constants.ts               # Colors, thresholds, k2cc SVG path data, breakpoints
  types.ts                   # Particle, BoltSegment, EnergyParams interfaces
```

## Component Integration

### Integration Steps

1. **`page.tsx`** must import and render `<HomeClient />` as the first child inside its root `<div>`, before all existing sections. Currently `HomeClient` exists but is not referenced in `page.tsx`.

2. **`HomeClient.tsx`** renders the fixed Canvas:

```tsx
// web/src/app/[locale]/HomeClient.tsx
'use client'
import { K2ccPulseCanvas } from '@/components/k2cc-hero/K2ccPulseCanvas'

export default function HomeClient() {
  return <K2ccPulseCanvas />
}
```

3. **K2ccPulseCanvas** renders a single `<canvas>` element:

```tsx
<canvas
  ref={canvasRef}
  className="fixed inset-0 w-full h-full pointer-events-none"
  style={{ zIndex: 0 }}
/>
```

4. **Existing sections** in `page.tsx` need their background changed from opaque to semi-transparent. This affects the Hero text area, Feature Cards grid, Comparison table container, and Download cards. Each wrapping `<div>` gets the `.hero-content-card` class (or equivalent Tailwind utilities).

### Content Layer Styling

Existing homepage sections need semi-transparent backgrounds to let the canvas bleed through while keeping text readable:

```css
.hero-content-card {
  background: rgba(5, 5, 8, 0.6);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(0, 255, 136, 0.08);
}

@media (max-width: 767px) {
  .hero-content-card {
    background: rgba(5, 5, 8, 0.85);
    backdrop-filter: none;
  }
}
```

## Color System

Single color trajectory — brand green throughout, white only at burst peak:

| scrollProgress | Color | Usage |
|---|---|---|
| 0.0 | #00ff88 | Brand green — rest |
| 0.35 | #005533 | Dimmed green — silence |
| 0.45 | #00ff88 | Brand green — buildup start |
| 0.65 | #00ff88→#ffffff | Transition to white-hot — buildup end |
| 0.75 | #ffffff core + #00ff88 glow edge | Burst peak |
| 0.90 | #ffffff→#00ff88 | Return to brand — aftermath |
| 1.0 | #00ff88 | Brand green — reset |

Background: static #050508. Glow-infected: shifts toward #0a1a0f. Burst flash: momentary #1a3a2a, 100ms decay.

## Dependencies

None. Zero npm packages added.

- Canvas 2D API (built-in)
- Web Audio API (built-in)
- requestAnimationFrame (built-in)
- IntersectionObserver (built-in)
- CSS backdrop-filter (supported in all target browsers)
