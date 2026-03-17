import { useRef, useCallback } from 'react'
import { SCROLL_LERP } from './constants'

interface ScrollState {
  current: number    // smoothed 0-1
  raw: number        // unsmoothed 0-1
  direction: number  // 1 = scrolling down, -1 = scrolling up
  prevRaw: number
}

/**
 * Returns a ref-based scroll progress tracker.
 * Call getProgress() inside rAF to get lerp-smoothed value.
 * Does NOT use useState — avoids React re-renders on every frame.
 */
export function useScrollProgress() {
  const state = useRef<ScrollState>({
    current: 0,
    raw: 0,
    direction: 1,
    prevRaw: 0,
  })

  const getProgress = useCallback((): ScrollState => {
    const s = state.current
    // Poll scrollY directly (works during iOS momentum scroll)
    const scrollHeight = document.documentElement.scrollHeight
    const viewportHeight = window.innerHeight
    const maxScroll = scrollHeight - viewportHeight
    s.raw = maxScroll > 0 ? Math.max(0, Math.min(1, window.scrollY / maxScroll)) : 0

    // Direction
    s.direction = s.raw >= s.prevRaw ? 1 : -1
    s.prevRaw = s.raw

    // Lerp smooth
    s.current += (s.raw - s.current) * SCROLL_LERP

    return s
  }, [])

  return { getProgress }
}
