'use client'

import { useRef, useEffect } from 'react'
import { BREAKPOINTS } from './constants'
import { PulseRenderer } from './renderer'
import type { RenderConfig } from './types'

function getRenderConfig(width: number, height: number): RenderConfig {
  const isMobile = width < BREAKPOINTS.mobile
  const isTablet = width >= BREAKPOINTS.mobile && width < BREAKPOINTS.tablet
  const dpr = Math.min(window.devicePixelRatio, isMobile ? 2 : window.devicePixelRatio)

  return {
    width,
    height,
    dpr,
    lineY: height * 0.4,
    wordmarkY: height * 0.22,
    wordmarkScale: isMobile ? 0.6 : 1.2,
    visibleCycles: isMobile ? 2 : 3,
    maxArcCount: isMobile ? 2 : isTablet ? 3 : 4,
    maxArcDepth: isMobile ? 4 : 5,
    maxParticles: isMobile ? 10 : isTablet ? 15 : 20,
    useRadialGlow: !isMobile,
  }
}

export function K2ccPulseCanvas() {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rafRef = useRef<number>(0)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    // Reduced motion: static green line
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
      const rect = canvas.getBoundingClientRect()
      const dpr = Math.min(window.devicePixelRatio, 2)
      canvas.width = rect.width * dpr
      canvas.height = rect.height * dpr
      const staticCtx = canvas.getContext('2d')
      if (staticCtx) {
        staticCtx.scale(dpr, dpr)
        const lineY = rect.height * 0.4
        staticCtx.strokeStyle = '#00ff88'
        staticCtx.lineWidth = 1.5
        staticCtx.globalAlpha = 0.6
        staticCtx.beginPath()
        staticCtx.moveTo(0, lineY)
        staticCtx.lineTo(rect.width, lineY)
        staticCtx.stroke()
        staticCtx.shadowColor = '#00ff88'
        staticCtx.shadowBlur = 4
        staticCtx.stroke()
        staticCtx.shadowBlur = 0
        staticCtx.globalAlpha = 1
      }
      return
    }

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const rect = canvas.getBoundingClientRect()
    const config = getRenderConfig(rect.width, rect.height)
    canvas.width = rect.width * config.dpr
    canvas.height = rect.height * config.dpr
    ctx.scale(config.dpr, config.dpr)

    const renderer = new PulseRenderer(ctx, config)

    // ResizeObserver
    let resizeTimer: ReturnType<typeof setTimeout>
    const observer = new ResizeObserver((entries) => {
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(() => {
        const entry = entries[0]
        if (!entry) return
        const { width, height } = entry.contentRect
        const newConfig = getRenderConfig(width, height)
        canvas.width = width * newConfig.dpr
        canvas.height = height * newConfig.dpr
        ctx.setTransform(1, 0, 0, 1, 0, 0)
        ctx.scale(newConfig.dpr, newConfig.dpr)
        renderer.updateConfig(newConfig)
      }, 100)
    })
    observer.observe(canvas)

    // Visibility
    let paused = false
    const handleVisibility = () => { paused = document.hidden }
    document.addEventListener('visibilitychange', handleVisibility)

    // rAF loop — time-driven, no scroll dependency
    const tick = (timestamp: number) => {
      if (!paused) {
        renderer.tick(timestamp, 0)
      }
      rafRef.current = requestAnimationFrame(tick)
    }
    rafRef.current = requestAnimationFrame(tick)

    return () => {
      cancelAnimationFrame(rafRef.current)
      observer.disconnect()
      document.removeEventListener('visibilitychange', handleVisibility)
      clearTimeout(resizeTimer)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      className="fixed inset-0 w-full h-full pointer-events-none"
      style={{ zIndex: 0 }}
      aria-hidden="true"
    />
  )
}
