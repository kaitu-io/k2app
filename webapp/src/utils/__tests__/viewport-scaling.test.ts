import { describe, it, expect, afterEach } from 'vitest';
import {
  computeScaleDecision,
  isAndroidCapacitorWebView,
  DESIGN_WIDTH,
} from '../viewport-scaling';

describe('computeScaleDecision', () => {
  describe('Android Capacitor (keyboard event protection)', () => {
    it('skips body mutation on large height shrink (soft keyboard show: 900 -> 500, 400px shrink)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 400, windowHeight: 500 },
        { windowWidth: 400, windowHeight: 900 },
        true,
      );
      expect(decision.skip).toBe(true);
      expect(decision.bodyStyle).toBeUndefined();
    });

    it('skips on typical numeric-keyboard shrink (797 -> 530, 267px)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 393, windowHeight: 530 },
        { windowWidth: 393, windowHeight: 797 },
        true,
      );
      expect(decision.skip).toBe(true);
    });

    it('APPLIES on small height shrink (edge-to-edge inset settle: 844 -> 797, 47px)', () => {
      // This is the bug: WebView reports innerHeight=844 before edge-to-edge
      // plugin applies bottom system-bar inset, then resizes to 797. The old
      // guard treated this as keyboard and skipped, leaving body.height
      // stuck at 844/scale and BottomNav clipped below the visible viewport.
      const decision = computeScaleDecision(
        { windowWidth: 393, windowHeight: 797 },
        { windowWidth: 393, windowHeight: 844 },
        true,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle).toBeDefined();
    });

    it('APPLIES on height grow (keyboard dismiss / system-bar reveal: 500 -> 797)', () => {
      // Grows are never keyboard-showing — always apply so body re-syncs.
      const decision = computeScaleDecision(
        { windowWidth: 393, windowHeight: 797 },
        { windowWidth: 393, windowHeight: 500 },
        true,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle).toBeDefined();
    });

    it('APPLIES at exactly the threshold boundary (150px shrink: 797 -> 647)', () => {
      // 150px boundary belongs to non-keyboard side (system bars max ~100px,
      // keyboard min ~200px — 150 is the safe middle that excludes both).
      const decision = computeScaleDecision(
        { windowWidth: 393, windowHeight: 647 },
        { windowWidth: 393, windowHeight: 797 },
        true,
      );
      expect(decision.skip).toBe(false);
    });

    it('skips just past the threshold (151px shrink)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 393, windowHeight: 646 },
        { windowWidth: 393, windowHeight: 797 },
        true,
      );
      expect(decision.skip).toBe(true);
    });

    it('applies scale when width changes (orientation/window resize)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 800, windowHeight: 400 },
        { windowWidth: 400, windowHeight: 800 },
        true,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle).toBeDefined();
    });

    it('applies scale on initial call (no previous state)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 400, windowHeight: 900 },
        null,
        true,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle).toBeDefined();
    });
  });

  describe('Tauri desktop / iOS / web (the bug we fixed)', () => {
    it('applies scale when only height changes on small-screen MacBook Air (scale<1)', () => {
      // Simulates adjust_window_size() output on 768p Air: 293x652
      // User drags bottom edge: height 652 -> 700
      const decision = computeScaleDecision(
        { windowWidth: 293, windowHeight: 700 },
        { windowWidth: 293, windowHeight: 652 },
        false,
      );
      expect(decision.skip).toBe(false);
      const scale = 293 / DESIGN_WIDTH;
      expect(decision.bodyStyle).toEqual({
        width: `${DESIGN_WIDTH}px`,
        height: `${700 / scale}px`,
        zoom: `${scale}`,
      });
    });

    it('clears body style when window is at or above design width', () => {
      const decision = computeScaleDecision(
        { windowWidth: 430, windowHeight: 956 },
        { windowWidth: 430, windowHeight: 800 },
        false,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle).toEqual({ width: '', height: '', zoom: '' });
    });

    it('clears body style when wider than design width (max-clamped)', () => {
      const decision = computeScaleDecision(
        { windowWidth: 480, windowHeight: 900 },
        null,
        false,
      );
      expect(decision.bodyStyle).toEqual({ width: '', height: '', zoom: '' });
    });

    it('applies scale when both dimensions change', () => {
      const decision = computeScaleDecision(
        { windowWidth: 350, windowHeight: 700 },
        { windowWidth: 293, windowHeight: 600 },
        false,
      );
      expect(decision.skip).toBe(false);
      expect(decision.bodyStyle?.zoom).toBe(`${350 / DESIGN_WIDTH}`);
    });
  });

  it('applies scale on first call with null previous', () => {
    const decision = computeScaleDecision(
      { windowWidth: 430, windowHeight: 956 },
      null,
      false,
    );
    expect(decision.skip).toBe(false);
    expect(decision.bodyStyle).toEqual({ width: '', height: '', zoom: '' });
  });
});

describe('isAndroidCapacitorWebView', () => {
  const originalUA = navigator.userAgent;

  afterEach(() => {
    Object.defineProperty(navigator, 'userAgent', {
      value: originalUA,
      configurable: true,
    });
    delete (window as unknown as { __TAURI__?: unknown }).__TAURI__;
  });

  it('returns true for Android UA without Tauri', () => {
    Object.defineProperty(navigator, 'userAgent', {
      value: 'Mozilla/5.0 (Linux; Android 13; Pixel 6) AppleWebKit/537.36',
      configurable: true,
    });
    expect(isAndroidCapacitorWebView()).toBe(true);
  });

  it('returns false when __TAURI__ is present even if UA says Android', () => {
    Object.defineProperty(navigator, 'userAgent', {
      value: 'Mozilla/5.0 (Linux; Android 13) WebKit Tauri',
      configurable: true,
    });
    (window as unknown as { __TAURI__?: unknown }).__TAURI__ = {};
    expect(isAndroidCapacitorWebView()).toBe(false);
  });

  it('returns false for non-Android UA', () => {
    Object.defineProperty(navigator, 'userAgent', {
      value: 'Mozilla/5.0 (Macintosh; Intel Mac OS X) AppleWebKit/537.36',
      configurable: true,
    });
    expect(isAndroidCapacitorWebView()).toBe(false);
  });
});
