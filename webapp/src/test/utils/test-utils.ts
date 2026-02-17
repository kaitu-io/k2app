/**
 * 测试工具函数
 */
import { vi, beforeEach, afterEach, expect } from 'vitest';
import { act } from '@testing-library/react';

/**
 * 等待指定时间
 */
export const wait = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

/**
 * 等待下一个 tick（微任务）
 */
export const waitForNextTick = () => new Promise(resolve => process.nextTick(resolve));

/**
 * 等待所有 Promise 完成
 */
export const flushPromises = () => act(async () => {
  await wait(0);
});

/**
 * 模拟用户输入
 */
export async function typeText(
  element: HTMLInputElement | HTMLTextAreaElement,
  text: string
) {
  await act(async () => {
    element.focus();
    element.value = text;
    element.dispatchEvent(new Event('input', { bubbles: true }));
    element.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

/**
 * 模拟表单提交
 */
export async function submitForm(form: HTMLFormElement) {
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
}

/**
 * 模拟键盘事件
 */
export function pressKey(element: HTMLElement, key: string, options?: KeyboardEventInit) {
  element.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, ...options })
  );
  element.dispatchEvent(
    new KeyboardEvent('keyup', { key, bubbles: true, ...options })
  );
}

/**
 * 模拟点击事件
 */
export async function click(element: HTMLElement) {
  await act(async () => {
    element.click();
  });
}

/**
 * 创建 mock 定时器环境
 */
export function useFakeTimers() {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  return {
    advanceTimersByTime: (ms: number) => vi.advanceTimersByTime(ms),
    runAllTimers: () => vi.runAllTimers(),
    runOnlyPendingTimers: () => vi.runOnlyPendingTimers(),
  };
}

/**
 * 创建可控的 Promise（用于测试 loading 状态）
 */
export function createControllablePromise<T>() {
  let resolve: (value: T) => void;
  let reject: (error: Error) => void;

  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });

  return {
    promise,
    resolve: (value: T) => resolve(value),
    reject: (error: Error) => reject(error),
  };
}

/**
 * Mock fetch 请求
 */
export function mockFetch(response: any, options?: { delay?: number; status?: number }) {
  const { delay = 0, status = 200 } = options || {};

  return vi.fn().mockImplementation(() =>
    new Promise(resolve =>
      setTimeout(
        () =>
          resolve({
            ok: status >= 200 && status < 300,
            status,
            json: () => Promise.resolve(response),
            text: () => Promise.resolve(JSON.stringify(response)),
          }),
        delay
      )
    )
  );
}

/**
 * 断言函数被调用时的参数
 */
export function expectCalledWith(
  mockFn: ReturnType<typeof vi.fn>,
  ...args: any[]
) {
  expect(mockFn).toHaveBeenCalledWith(...args);
}

/**
 * 断言函数被调用的次数
 */
export function expectCalledTimes(
  mockFn: ReturnType<typeof vi.fn>,
  times: number
) {
  expect(mockFn).toHaveBeenCalledTimes(times);
}

/**
 * 创建带有默认值的 mock 函数
 */
export function createMockWithDefault<T>(defaultValue: T) {
  return vi.fn().mockReturnValue(defaultValue);
}

/**
 * 创建 async mock 函数
 */
export function createAsyncMock<T>(value: T, delay = 0) {
  return vi.fn().mockImplementation(
    () =>
      new Promise(resolve =>
        setTimeout(() => resolve(value), delay)
      )
  );
}

/**
 * 创建失败的 async mock
 */
export function createFailingAsyncMock(errorMessage: string, delay = 0) {
  return vi.fn().mockImplementation(
    () =>
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error(errorMessage)), delay)
      )
  );
}

/**
 * 生成随机测试 ID
 */
export function generateTestId(prefix = 'test') {
  return `${prefix}-${Math.random().toString(36).substr(2, 9)}`;
}

/**
 * 创建测试用的 DOM 事件
 */
export const createEvent = {
  click: (_target?: Element) => new MouseEvent('click', { bubbles: true }),
  change: (value: string) => {
    const event = new Event('change', { bubbles: true });
    Object.defineProperty(event, 'target', { value: { value } });
    return event;
  },
  input: (value: string) => {
    const event = new Event('input', { bubbles: true });
    Object.defineProperty(event, 'target', { value: { value } });
    return event;
  },
  submit: () => new Event('submit', { bubbles: true, cancelable: true }),
  focus: () => new FocusEvent('focus', { bubbles: true }),
  blur: () => new FocusEvent('blur', { bubbles: true }),
};
