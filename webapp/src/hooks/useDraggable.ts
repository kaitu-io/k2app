import { useState, useEffect, useCallback, useRef } from 'react';

interface DraggablePosition {
  x: number;
  y: number;
  side: 'left' | 'right';
}

interface UseDraggableOptions {
  storageKey: string;
  defaultY: number;
  defaultSide: 'left' | 'right';
  edgeMargin: number;
  elementSize: number;
  dragThreshold: number;
  sidebarWidth: number;
  minY?: number;
  bottomClearance?: number;
}

interface UseDraggableReturn {
  position: DraggablePosition;
  isDragging: boolean;
  bindDrag: { onPointerDown: (e: React.PointerEvent) => void };
  elementRef: React.RefObject<HTMLElement | null>;
}

const DESIGN_WIDTH = 430;
const DEFAULT_MIN_Y = 50;
const DEFAULT_BOTTOM_CLEARANCE = 90;
const SNAP_DURATION = 300;

function getBodyScale(): number {
  const t = document.body.style.transform;
  if (!t) return 1;
  const m = t.match(/scale\(([\d.]+)\)/);
  return m ? parseFloat(m[1]) : 1;
}

function getLogicalViewport(): { width: number; height: number } {
  const scale = getBodyScale();
  return {
    width: Math.max(window.innerWidth / scale, DESIGN_WIDTH),
    height: window.innerHeight / scale,
  };
}

function loadPosition(key: string): { y: number; side: 'left' | 'right' } | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (
      typeof parsed.y === 'number' &&
      (parsed.side === 'left' || parsed.side === 'right')
    ) {
      return { y: parsed.y, side: parsed.side };
    }
  } catch { /* ignore corrupt data */ }
  return null;
}

function savePosition(key: string, y: number, side: 'left' | 'right') {
  try {
    localStorage.setItem(key, JSON.stringify({ y, side }));
  } catch { /* storage full — non-critical */ }
}

export function useDraggable(options: UseDraggableOptions): UseDraggableReturn {
  const {
    storageKey,
    defaultY,
    defaultSide,
    edgeMargin,
    elementSize,
    dragThreshold,
    sidebarWidth,
    minY = DEFAULT_MIN_Y,
    bottomClearance = DEFAULT_BOTTOM_CLEARANCE,
  } = options;

  const elementRef = useRef<HTMLElement | null>(null);
  const isDraggingRef = useRef(false);
  const [isDragging, setIsDragging] = useState(false);

  // Refs for document-level listeners (attached during drag, detached on pointerup)
  const onPointerMoveRef = useRef<((e: PointerEvent) => void) | null>(null);
  const onPointerUpRef = useRef<((e: PointerEvent) => void) | null>(null);

  const clampY = useCallback((y: number) => {
    const vh = getLogicalViewport().height;
    const maxY = vh - elementSize - bottomClearance;
    return Math.max(minY, Math.min(maxY, y));
  }, [elementSize, minY, bottomClearance]);

  const getEdgeX = useCallback((side: 'left' | 'right') => {
    const vw = getLogicalViewport().width;
    return side === 'left'
      ? sidebarWidth + edgeMargin
      : vw - elementSize - edgeMargin;
  }, [sidebarWidth, edgeMargin, elementSize]);

  // Compute initial position from localStorage or defaults
  const [position, setPosition] = useState<DraggablePosition>(() => {
    const saved = loadPosition(storageKey);
    const side = saved?.side ?? defaultSide;
    const y = clampY(saved?.y ?? defaultY);
    return { x: getEdgeX(side), y, side };
  });

  // Drag state kept in refs (not reactive — perf critical for pointermove)
  const dragState = useRef({
    startPointerX: 0,
    startPointerY: 0,
    startLeft: 0,
    startTop: 0,
    totalMovement: 0,
    rafId: 0,
    currentX: 0,
    currentY: 0,
    pointerId: -1,
  });

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    if (e.button !== 0) return; // left button only
    const el = elementRef.current;
    if (!el) return;

    // Capture pointer to prevent browser native drag (pointercancel on SVG icon).
    // We do NOT rely on capture for event routing — document listeners handle that.
    try { el.setPointerCapture(e.pointerId); } catch { /* ignore if capture fails */ }

    const scale = getBodyScale();
    const ds = dragState.current;
    ds.startPointerX = e.clientX / scale;
    ds.startPointerY = e.clientY / scale;
    ds.startLeft = position.x;
    ds.startTop = position.y;
    ds.totalMovement = 0;
    ds.rafId = 0;
    ds.currentX = position.x;
    ds.currentY = position.y;
    ds.pointerId = e.pointerId;

    // Remove transition for instant position updates during drag
    el.style.transition = 'none';

    // Attach document-level listeners (immune to MUI ripple releasing pointer capture)
    if (onPointerMoveRef.current) document.addEventListener('pointermove', onPointerMoveRef.current);
    if (onPointerUpRef.current) {
      document.addEventListener('pointerup', onPointerUpRef.current);
      document.addEventListener('pointercancel', onPointerUpRef.current);
    }
  }, [position]);

  // Define pointer move/up handlers in refs (attached to document during drag)
  useEffect(() => {
    const el = elementRef.current;
    if (!el) return;

    onPointerMoveRef.current = (e: PointerEvent) => {
      const ds = dragState.current;
      if (ds.pointerId === -1 || e.pointerId !== ds.pointerId) return;

      const scale = getBodyScale();
      const logicalX = e.clientX / scale;
      const logicalY = e.clientY / scale;

      const dx = logicalX - ds.startPointerX;
      const dy = logicalY - ds.startPointerY;

      const newX = ds.startLeft + dx;
      const newY = clampY(ds.startTop + dy);

      ds.totalMovement += Math.abs(newX - ds.currentX) + Math.abs(newY - ds.currentY);
      ds.currentX = newX;
      ds.currentY = newY;

      if (ds.totalMovement > dragThreshold && !isDraggingRef.current) {
        isDraggingRef.current = true;
        setIsDragging(true);
      }

      if (ds.rafId) cancelAnimationFrame(ds.rafId);
      ds.rafId = requestAnimationFrame(() => {
        el.style.left = `${ds.currentX}px`;
        el.style.top = `${ds.currentY}px`;
      });
    };

    onPointerUpRef.current = (e: PointerEvent) => {
      const ds = dragState.current;
      if (ds.pointerId === -1) return;
      if (e.pointerId !== ds.pointerId) return;

      if (ds.rafId) cancelAnimationFrame(ds.rafId);
      ds.pointerId = -1;

      // Detach document-level listeners
      if (onPointerMoveRef.current) document.removeEventListener('pointermove', onPointerMoveRef.current);
      if (onPointerUpRef.current) {
        document.removeEventListener('pointerup', onPointerUpRef.current);
        document.removeEventListener('pointercancel', onPointerUpRef.current);
      }

      if (ds.totalMovement <= dragThreshold) {
        // Was a click, not a drag — reset
        isDraggingRef.current = false;
        setIsDragging(false);
        return;
      }

      // Snap to nearest edge
      const vw = getLogicalViewport().width;
      const midpoint = vw / 2;
      const side: 'left' | 'right' =
        ds.currentX + elementSize / 2 < midpoint ? 'left' : 'right';

      const snapX = side === 'left'
        ? sidebarWidth + edgeMargin
        : vw - elementSize - edgeMargin;
      const snapY = ds.currentY;

      // Animate snap
      el.style.transition = `left ${SNAP_DURATION}ms cubic-bezier(0.25, 0.8, 0.25, 1)`;
      el.style.left = `${snapX}px`;

      const newPos = { x: snapX, y: snapY, side };
      setPosition(newPos);
      savePosition(storageKey, snapY, side);

      // Clear isDragging after snap animation finishes
      setTimeout(() => {
        isDraggingRef.current = false;
        setIsDragging(false);
      }, SNAP_DURATION);
    };

    // iOS belt-and-suspenders: prevent scroll during drag
    const onTouchMove = (e: TouchEvent) => {
      if (isDraggingRef.current) {
        e.preventDefault();
      }
    };

    el.addEventListener('touchmove', onTouchMove, { passive: false });

    return () => {
      el.removeEventListener('touchmove', onTouchMove);
      // Safety cleanup for document listeners (e.g. unmount during drag)
      if (onPointerMoveRef.current) document.removeEventListener('pointermove', onPointerMoveRef.current);
      if (onPointerUpRef.current) {
        document.removeEventListener('pointerup', onPointerUpRef.current);
        document.removeEventListener('pointercancel', onPointerUpRef.current);
      }
    };
  }, [clampY, dragThreshold, edgeMargin, elementSize, sidebarWidth, storageKey]);

  // Re-clamp on resize / orientation change
  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>;
    const handleResize = () => {
      clearTimeout(timeoutId);
      timeoutId = setTimeout(() => {
        setPosition(prev => ({
          ...prev,
          y: clampY(prev.y),
          x: prev.side === 'left'
            ? sidebarWidth + edgeMargin
            : getLogicalViewport().width - elementSize - edgeMargin,
        }));
      }, 100);
    };
    window.addEventListener('resize', handleResize);
    window.addEventListener('orientationchange', handleResize);
    return () => {
      clearTimeout(timeoutId);
      window.removeEventListener('resize', handleResize);
      window.removeEventListener('orientationchange', handleResize);
    };
  }, [clampY, sidebarWidth, edgeMargin, elementSize]);

  // Sync position when sidebarWidth changes (desktop ↔ mobile layout switch)
  useEffect(() => {
    setPosition(prev => ({
      ...prev,
      x: getEdgeX(prev.side),
    }));
  }, [getEdgeX]);

  return { position, isDragging, bindDrag: { onPointerDown: handlePointerDown }, elementRef };
}
