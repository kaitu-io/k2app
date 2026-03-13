import { useState, useEffect, useRef } from 'react';

export interface TargetRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

/**
 * Tracks a DOM element's viewport-coordinate bounding rect every animation frame.
 * Returns null when selector matches nothing (also handles lazy-loaded elements).
 */
export function useTargetRect(selector: string | null): {
  rect: TargetRect | null;
  element: HTMLElement | null;
} {
  const [rect, setRect] = useState<TargetRect | null>(null);
  const [element, setElement] = useState<HTMLElement | null>(null);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    if (!selector) {
      setRect(null);
      setElement(null);
      return;
    }

    let prevTop = -1;
    let prevLeft = -1;
    let prevWidth = -1;
    let prevHeight = -1;
    let prevEl: HTMLElement | null = null;

    const tick = () => {
      const el = document.querySelector<HTMLElement>(selector);

      if (!el) {
        if (prevEl !== null) {
          prevEl = null;
          setRect(null);
          setElement(null);
        }
        rafRef.current = requestAnimationFrame(tick);
        return;
      }

      if (el !== prevEl) {
        prevEl = el;
        setElement(el);
      }

      const r = el.getBoundingClientRect();
      // Only update state when values actually change (avoid re-renders)
      if (
        r.top !== prevTop ||
        r.left !== prevLeft ||
        r.width !== prevWidth ||
        r.height !== prevHeight
      ) {
        prevTop = r.top;
        prevLeft = r.left;
        prevWidth = r.width;
        prevHeight = r.height;
        setRect({ top: r.top, left: r.left, width: r.width, height: r.height });
      }

      rafRef.current = requestAnimationFrame(tick);
    };

    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [selector]);

  return { rect, element };
}
