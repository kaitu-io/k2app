import { describe, it, expect, beforeEach, vi } from 'vitest';

type MediaListener = (e: MediaQueryListEvent) => void;

function setViewport(w: number, h: number): void {
  Object.defineProperty(window, 'innerWidth', { value: w, writable: true, configurable: true });
  Object.defineProperty(window, 'innerHeight', { value: h, writable: true, configurable: true });
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (query: string) => {
      const minW = /\(min-width:\s*(\d+)px\)/.exec(query)?.[1];
      const minH = /\(min-height:\s*(\d+)px\)/.exec(query)?.[1];
      const matches =
        (!minW || window.innerWidth >= parseInt(minW, 10)) &&
        (!minH || window.innerHeight >= parseInt(minH, 10));
      return {
        matches,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
      };
    },
  });
}

async function loadStoreFresh() {
  vi.resetModules();
  return await import('../layout.store');
}

beforeEach(() => {
  vi.resetModules();
});

describe('layout.store breakpoint', () => {
  describe('phone form factor', () => {
    it('Xiaomi M2012K11C portrait (392x872): mobile', async () => {
      setViewport(392, 872);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      const s = useLayoutStore.getState();
      expect(s.isMobile).toBe(true);
      expect(s.isDesktop).toBe(false);
    });

    it('Xiaomi M2012K11C landscape (872x392): still mobile (no rotation flip)', async () => {
      setViewport(872, 392);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      const s = useLayoutStore.getState();
      expect(s.isMobile).toBe(true);
      expect(s.isDesktop).toBe(false);
    });

    it('iPhone 15 Pro Max landscape (932x430): still mobile', async () => {
      setViewport(932, 430);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isMobile).toBe(true);
    });
  });

  describe('tablet form factor', () => {
    it('iPad mini portrait (744x1133): desktop', async () => {
      setViewport(744, 1133);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isDesktop).toBe(true);
    });

    it('iPad mini landscape (1133x744): desktop', async () => {
      setViewport(1133, 744);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isDesktop).toBe(true);
    });

    it('iPad Pro 12.9 portrait (1024x1366): desktop', async () => {
      setViewport(1024, 1366);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isDesktop).toBe(true);
    });
  });

  describe('iPad multitasking — narrow column stays mobile', () => {
    it('Slide Over (320x1024): mobile', async () => {
      setViewport(320, 1024);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isMobile).toBe(true);
    });

    it('Split View half (507x1024): mobile', async () => {
      setViewport(507, 1024);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isMobile).toBe(true);
    });
  });

  describe('threshold boundary', () => {
    it('exactly 600 on shorter side: desktop', async () => {
      setViewport(600, 1024);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isDesktop).toBe(true);
    });

    it('599 on shorter side: mobile', async () => {
      setViewport(599, 1024);
      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isMobile).toBe(true);
    });
  });

  describe('reactive updates via matchMedia', () => {
    it('media query change flips layout when crossing threshold', async () => {
      setViewport(400, 800);
      let mediaListener: MediaListener | null = null;
      Object.defineProperty(window, 'matchMedia', {
        writable: true,
        configurable: true,
        value: (query: string) => {
          const minW = /\(min-width:\s*(\d+)px\)/.exec(query)?.[1];
          const minH = /\(min-height:\s*(\d+)px\)/.exec(query)?.[1];
          const evaluate = () =>
            (!minW || window.innerWidth >= parseInt(minW, 10)) &&
            (!minH || window.innerHeight >= parseInt(minH, 10));
          return {
            get matches() { return evaluate(); },
            media: query,
            onchange: null,
            addListener: (cb: MediaListener) => { mediaListener = cb; },
            removeListener: () => { mediaListener = null; },
            addEventListener: (_: string, cb: MediaListener) => { mediaListener = cb; },
            removeEventListener: () => { mediaListener = null; },
            dispatchEvent: () => false,
          };
        },
      });

      const { useLayoutStore, initializeLayoutStore } = await loadStoreFresh();
      initializeLayoutStore();
      expect(useLayoutStore.getState().isMobile).toBe(true);

      Object.defineProperty(window, 'innerWidth', { value: 900, writable: true, configurable: true });
      Object.defineProperty(window, 'innerHeight', { value: 800, writable: true, configurable: true });
      if (mediaListener) (mediaListener as MediaListener)({ matches: true } as MediaQueryListEvent);
      expect(useLayoutStore.getState().isDesktop).toBe(true);
    });
  });
});
