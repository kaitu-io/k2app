/**
 * Simple delay utility - returns a promise that resolves after specified milliseconds
 * @param ms - Delay time in milliseconds
 */
export function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Execute a function after a delay, returns cancel function
 * @param fn - Function to execute
 * @param ms - Delay time in milliseconds
 * @returns Cancel function
 */
export function delayedExec(fn: () => void, ms: number): () => void {
  const timer = setTimeout(fn, ms);
  return () => clearTimeout(timer);
}

/**
 * Debounce function - delays execution until after wait milliseconds have elapsed
 * since the last time the debounced function was invoked
 * @param fn - Function to debounce
 * @param wait - Wait time in milliseconds
 */
export function debounce<T extends (...args: any[]) => any>(
  fn: T,
  wait: number
): ((...args: Parameters<T>) => void) & { cancel: () => void } {
  let timer: ReturnType<typeof setTimeout> | null = null;

  const debounced = (...args: Parameters<T>) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      fn(...args);
      timer = null;
    }, wait);
  };

  debounced.cancel = () => {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  };

  return debounced;
}

/**
 * Async debounce function - supports async functions
 * @param fn - Async function to debounce
 * @param wait - Wait time in milliseconds
 */
export function debounceAsync<T extends (...args: any[]) => Promise<any>>(
  fn: T,
  wait: number
): (...args: Parameters<T>) => Promise<ReturnType<T>> {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let lastReject: ((reason?: any) => void) | null = null;
  return (...args: Parameters<T>) => {
    if (timer) {
      clearTimeout(timer);
      if (lastReject) lastReject('debounced');
    }
    return new Promise<ReturnType<T>>((resolve, reject) => {
      lastReject = (reason) => {
        if (reason !== 'debounced') {
          reject(reason);
        } else {
          // Debounced calls resolve with undefined
          resolve(undefined as unknown as ReturnType<T>);
        }
      };
      timer = setTimeout(async () => {
        try {
          const result = await fn(...args);
          resolve(result);
        } catch (e) {
          reject(e);
        }
      }, wait);
    });
  };
}

/**
 * Throttle function - ensures function is called at most once per wait period
 * @param fn - Function to throttle
 * @param wait - Minimum time between calls in milliseconds
 */
export function throttle<T extends (...args: any[]) => any>(
  fn: T,
  wait: number
): (...args: Parameters<T>) => void {
  let lastCall = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;

  return (...args: Parameters<T>) => {
    const now = Date.now();
    const remaining = wait - (now - lastCall);

    if (remaining <= 0) {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
      lastCall = now;
      fn(...args);
    } else if (!timer) {
      timer = setTimeout(() => {
        lastCall = Date.now();
        timer = null;
        fn(...args);
      }, remaining);
    }
  };
}

/**
 * Focus an element after a delay - useful for Dialog/Modal focus management
 * Works around timing issues with animations and old WebViews
 * @param getElement - Function that returns the element to focus (called after delay)
 * @param ms - Delay time in milliseconds (default: 100ms)
 * @returns Cancel function
 */
export function delayedFocus(
  getElement: () => HTMLElement | null | undefined,
  ms: number = 100
): () => void {
  const timer = setTimeout(() => {
    const el = getElement();
    if (el && typeof el.focus === 'function') {
      el.focus();
    }
  }, ms);
  return () => clearTimeout(timer);
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 b";
  const k = 1024;
  const sizes = ["b", "K", "M", "G", "T"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}