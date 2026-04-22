/**
 * Viewport scaling helpers for main.tsx.
 *
 * The UI is designed for DESIGN_WIDTH px. Narrower windows scale down via
 * CSS `zoom` (not transform) so position:fixed children stay relative to
 * the viewport. Body width/height/zoom are mutated to match.
 *
 * Android Capacitor caveat: mutating body style during keyboard-triggered
 * resize dismisses the soft keyboard and closes MUI Dialogs (backdropClick).
 * So on Android only, height-only changes (with width unchanged, which is
 * the keyboard signature — orientation changes also vary width) skip body
 * mutation. Tauri desktop, iOS Capacitor, and web must always recompute so
 * user-initiated window resizes trigger reflow.
 */

export const DESIGN_WIDTH = 430;

export interface ViewportState {
  windowWidth: number;
  windowHeight: number;
}

export interface BodyStyle {
  width: string;
  height: string;
  zoom: string;
}

export interface ScaleDecision {
  skip: boolean;
  bodyStyle?: BodyStyle;
}

/**
 * Compute the next body-style update (or skip) given the current window
 * dimensions and the previously-seen ones.
 *
 * - Android keyboard event (width same, height changed) → skip
 * - Otherwise scale<1 → set explicit body width/height/zoom
 * - Otherwise → clear body style (free-flowing)
 */
export function computeScaleDecision(
  current: ViewportState,
  previous: ViewportState | null,
  isAndroidCapacitor: boolean,
): ScaleDecision {
  if (previous && isAndroidCapacitor) {
    const widthChanged = current.windowWidth !== previous.windowWidth;
    const heightChanged = current.windowHeight !== previous.windowHeight;
    if (!widthChanged && heightChanged) {
      return { skip: true };
    }
  }

  const scaleX = current.windowWidth / DESIGN_WIDTH;
  const scale = Math.min(scaleX, 1);

  if (scale < 1) {
    return {
      skip: false,
      bodyStyle: {
        width: `${DESIGN_WIDTH}px`,
        height: `${current.windowHeight / scale}px`,
        zoom: `${scale}`,
      },
    };
  }

  return {
    skip: false,
    bodyStyle: { width: '', height: '', zoom: '' },
  };
}

/**
 * True when running inside Capacitor's Android WebView.
 * Tauri desktop is explicitly excluded even if UA hints suggest otherwise.
 */
export function isAndroidCapacitorWebView(): boolean {
  if (typeof window === 'undefined' || typeof navigator === 'undefined') {
    return false;
  }
  if ((window as unknown as { __TAURI__?: unknown }).__TAURI__) {
    return false;
  }
  return /Android/i.test(navigator.userAgent);
}
