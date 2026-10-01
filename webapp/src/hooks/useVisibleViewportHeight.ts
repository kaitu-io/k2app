import { useEffect, useState } from 'react';

function read(): number {
  return window.visualViewport?.height ?? window.innerHeight;
}

/**
 * Height of the part of the page the user can actually see.
 *
 * On iOS (Capacitor WKWebView, no keyboard-resize plugin) the on-screen
 * keyboard does NOT shrink the layout viewport — it just covers the bottom of
 * it. `window.innerHeight` and `100vh` keep reporting the full height, so
 * anything sized against them ends up underneath the keyboard, unreachable.
 * `visualViewport` is the one measurement that follows the keyboard.
 */
export function useVisibleViewportHeight(): number {
  const [height, setHeight] = useState(read);

  useEffect(() => {
    const update = () => setHeight(read());
    const vv = window.visualViewport;
    vv?.addEventListener('resize', update);
    window.addEventListener('resize', update);
    update();
    return () => {
      vv?.removeEventListener('resize', update);
      window.removeEventListener('resize', update);
    };
  }, []);

  return height;
}
