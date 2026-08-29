/**
 * FeedbackButton — coordinate-space guards
 *
 * useDraggable emits DESIGN-space coordinates (the space #root is shrunk into
 * by main.tsx's CSS `zoom` on narrow viewports). The button must therefore be
 * portaled INTO #root; MUI's default <Portal> target (document.body) sits
 * outside the zoom and made the button overhang the right edge — clipped by
 * half on every viewport narrower than the 430px design width.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/FeedbackButton.test.tsx
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/react';

// ---------------------------------------------------------------------------
// Self-sufficient DOM stubs.
//
// setup.ts installs vi.fn()-backed getComputedStyle and localStorage; the
// global afterEach vi.restoreAllMocks() strips their implementations, so from
// the second test onward they return undefined. Both are replaced here with
// plain functions (invisible to the mock registry, hence never restored away).
// The getComputedStyle stub additionally answers the --app-zoom custom
// property, which the shared stub does not.
// ---------------------------------------------------------------------------
let appZoom = '1';

window.getComputedStyle = ((el: Element) => {
  const styles: Record<string, string> = {
    visibility: 'visible',
    display: 'block',
    minHeight: '0px',
    width: '100px',
    height: '100px',
    opacity: '1',
    transform: 'none',
    transition: 'none',
    animation: 'none',
    '--app-zoom': el === document.documentElement ? appZoom : '',
  };
  return new Proxy(styles, {
    get(target, prop) {
      if (prop === 'getPropertyValue') return (name: string) => target[name] ?? '';
      return typeof prop === 'string' ? (target[prop] ?? '') : undefined;
    },
  });
}) as unknown as typeof window.getComputedStyle;

const storage = new Map<string, string>();
Object.defineProperty(window, 'localStorage', {
  configurable: true,
  value: {
    getItem: (k: string) => storage.get(k) ?? null,
    setItem: (k: string, v: string) => void storage.set(k, String(v)),
    removeItem: (k: string) => void storage.delete(k),
    clear: () => storage.clear(),
    key: (i: number) => [...storage.keys()][i] ?? null,
    get length() {
      return storage.size;
    },
  },
});

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

const mockNavigate = vi.fn();
vi.mock('react-router-dom', () => ({
  useNavigate: () => mockNavigate,
}));

let layoutState = { sidebarWidth: 220, isDesktop: false };
vi.mock('../../stores', () => ({
  useLayout: () => layoutState,
}));

vi.mock('../../stores/feedback.store', () => ({
  useFeedbackStore: (selector: (s: { unreadCount: number }) => unknown) =>
    selector({ unreadCount: 0 }),
}));

import FeedbackButton from '../FeedbackButton';

const STORAGE_KEY = 'k2_feedback_btn_pos';
const DESIGN_WIDTH = 430;
const FAB_SIZE = 40; // MUI Fab size="small"
const EDGE_MARGIN = 8;

/** Mirrors main.tsx applyScale(): zoom on #root, mirrored to --app-zoom. */
function applyViewport(width: number, height: number, zoom: number) {
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true });
  Object.defineProperty(window, 'innerHeight', { value: height, configurable: true });
  appZoom = String(zoom);
}

function makeRoot(): HTMLElement {
  const rootEl = document.createElement('div');
  rootEl.id = 'root';
  document.body.appendChild(rootEl);
  return rootEl;
}

const getFab = () => document.querySelector('.MuiFab-root') as HTMLElement | null;

describe('FeedbackButton', () => {
  beforeEach(() => {
    storage.clear();
    layoutState = { sidebarWidth: 220, isDesktop: false };
    applyViewport(1024, 768, 1);
  });

  afterEach(() => {
    cleanup();
    document.body.replaceChildren();
  });

  describe('portal target', () => {
    it('mounts inside #root so it shares the viewport zoom', () => {
      const rootEl = makeRoot();
      render(<FeedbackButton />, { container: rootEl });

      const fab = getFab();
      expect(fab).not.toBeNull();
      // The guard: MUI's default body-mounted Portal would fail this, and a
      // body-mounted button escapes the zoom that its coordinates assume.
      expect(rootEl.contains(fab!)).toBe(true);
      expect(fab!.parentElement).not.toBe(document.body);
    });

    it('falls back to document.body when #root is absent', () => {
      render(<FeedbackButton />);

      const fab = getFab();
      expect(fab).not.toBeNull();
      expect(document.body.contains(fab!)).toBe(true);
    });
  });

  describe('design-space positioning', () => {
    it('does not overhang the physical right edge of a scaled-down viewport', () => {
      // The reported case: a 403px-wide Tauri window scaled to the design width.
      const zoom = 403 / DESIGN_WIDTH;
      applyViewport(403, 864, zoom);
      const rootEl = makeRoot();
      render(<FeedbackButton />, { container: rootEl });

      const fab = getFab()!;
      const left = parseFloat(fab.style.left);
      expect(left).toBeCloseTo(DESIGN_WIDTH - FAB_SIZE - EDGE_MARGIN, 6);

      // jsdom does not lay CSS `zoom` out, so derive the device-pixel edge the
      // way a browser would: coordinates inside #root are scaled by the zoom,
      // coordinates on <body> are not. Pre-fix (body-mounted) this was
      // 382 + 40 = 422 against a 403px viewport — half the button clipped off.
      const scale = rootEl.contains(fab) ? zoom : 1;
      expect((left + FAB_SIZE) * scale).toBeLessThanOrEqual(403);
    });

    it('does not overhang a narrow UNSCALED viewport (standalone browser)', () => {
      // main.tsx only calls setupViewportScaling() on Tauri / Capacitor native,
      // so a browser — including the webapp embedded in the Linux k2 binary —
      // runs unscaled at its real width. Flooring the logical viewport at the
      // 430px design width used to push the button 19px past a 403px edge.
      applyViewport(403, 864, 1);
      const rootEl = makeRoot();
      render(<FeedbackButton />, { container: rootEl });

      const left = parseFloat(getFab()!.style.left);
      expect(left).toBe(403 - FAB_SIZE - EDGE_MARGIN);
      expect(left + FAB_SIZE).toBeLessThanOrEqual(403);
    });

    it('derives the default Y from the logical (unzoomed) viewport height', () => {
      applyViewport(400, 800, 0.5);
      const rootEl = makeRoot();
      render(<FeedbackButton />, { container: rootEl });

      // Logical height is 800 / 0.5 = 1600 and the default sits at 65% of it.
      // Reading raw window.innerHeight (the pre-fix behaviour) yields 520.
      expect(parseFloat(getFab()!.style.top)).toBe(Math.round(1600 * 0.65));
    });

    it('restores a persisted side and clamps the stored Y', () => {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ y: 120, side: 'left' }));
      applyViewport(403, 864, 403 / DESIGN_WIDTH);
      const rootEl = makeRoot();
      render(<FeedbackButton />, { container: rootEl });

      const fab = getFab()!;
      // Mobile layout ⇒ no sidebar offset, so the left edge is the margin alone.
      expect(parseFloat(fab.style.left)).toBe(EDGE_MARGIN);
      expect(parseFloat(fab.style.top)).toBe(120);
    });
  });
});
