import { useRef, useCallback } from 'react';
import { SCROLL_LERP_FACTOR } from './constants';

export function useScrollProgress() {
  const smoothRef = useRef(0);
  const directionRef = useRef<'down' | 'up'>('down');
  const prevRawRef = useRef(0);

  const getProgress = useCallback(() => {
    const scrollY = window.scrollY || window.pageYOffset;
    const maxScroll = document.documentElement.scrollHeight - window.innerHeight;
    const rawProgress = maxScroll > 0 ? scrollY / maxScroll : 0;

    directionRef.current = rawProgress >= prevRawRef.current ? 'down' : 'up';
    prevRawRef.current = rawProgress;

    smoothRef.current += (rawProgress - smoothRef.current) * SCROLL_LERP_FACTOR;

    return {
      raw: rawProgress,
      smooth: smoothRef.current,
      direction: directionRef.current,
    };
  }, []);

  return { getProgress };
}
