/**
 * Vitest 全局测试配置
 *
 * 这个文件在每个测试文件执行前自动运行
 * 用于配置全局 mock 和测试环境
 */
import '@testing-library/jest-dom/vitest';
import { configure } from '@testing-library/dom';
import { vi, beforeEach, afterEach } from 'vitest';

// 配置 testing-library
// 1. 禁用 computedStyleSupportsPseudoElements 避免 jsdom 的 getComputedStyle 问题
// 2. 设置 defaultHidden 为 true，跳过 isInaccessible 检查
configure({
  computedStyleSupportsPseudoElements: false,
  defaultHidden: true,
});

// ==================== DOM API Mocks ====================

// Mock window.matchMedia (Material-UI 需要)
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// Mock ResizeObserver (Material-UI 需要)
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// Mock IntersectionObserver
global.IntersectionObserver = class IntersectionObserver {
  root = null;
  rootMargin = '';
  thresholds = [];
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() { return []; }
} as any;

// Mock scrollTo
window.scrollTo = vi.fn();

// Mock getComputedStyle (Material-UI 需要完整的 CSSStyleDeclaration)
// 使用 Proxy 来处理任意属性访问
const createMockComputedStyle = () => {
  const baseStyles: Record<string, string> = {
    visibility: 'visible',
    display: 'block',
    minHeight: '0px',
    width: '100px',
    height: '100px',
    opacity: '1',
    transform: 'none',
    transition: 'none',
    animation: 'none',
  };

  return new Proxy(baseStyles, {
    get(target, prop) {
      if (prop === 'getPropertyValue') {
        return (name: string) => target[name] || '';
      }
      if (typeof prop === 'string') {
        return target[prop] || '';
      }
      return undefined;
    },
  });
};

window.getComputedStyle = vi.fn().mockImplementation(() => createMockComputedStyle()) as any;

// ==================== Storage Mocks ====================

// 创建可重置的 localStorage mock
const createStorageMock = () => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] || null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      store = {};
    }),
    key: vi.fn((index: number) => Object.keys(store)[index] || null),
    get length() {
      return Object.keys(store).length;
    },
    _reset: () => {
      store = {};
    },
  };
};

const localStorageMock = createStorageMock();
const sessionStorageMock = createStorageMock();

Object.defineProperty(window, 'localStorage', { value: localStorageMock });
Object.defineProperty(window, 'sessionStorage', { value: sessionStorageMock });

// ==================== Navigator Mocks ====================

// Mock clipboard (安全地处理已存在的属性)
try {
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: vi.fn().mockResolvedValue(undefined),
      readText: vi.fn().mockResolvedValue(''),
    },
    writable: true,
    configurable: true,
  });
} catch {
  // clipboard 已存在，使用 vi.spyOn 方式 mock
  if (navigator.clipboard) {
    vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);
    vi.spyOn(navigator.clipboard, 'readText').mockResolvedValue('');
  }
}

try {
  Object.defineProperty(navigator, 'language', {
    value: 'zh-CN',
    writable: true,
    configurable: true,
  });
} catch {
  // 忽略错误
}

// ==================== Console 配置 ====================

// 在测试中静默某些 console 输出（可选）
const originalConsoleError = console.error;
const originalConsoleWarn = console.warn;

beforeEach(() => {
  // 重置 storage
  (localStorageMock as any)._reset();
  (sessionStorageMock as any)._reset();

  // 重置所有 mock
  vi.clearAllMocks();
});

afterEach(() => {
  // 清理
  vi.restoreAllMocks();
});

// 过滤已知的无害警告（如 React 的 act 警告）
console.error = (...args: any[]) => {
  const message = args[0]?.toString() || '';
  // 过滤 React act 警告
  if (message.includes('Warning: An update to') && message.includes('was not wrapped in act')) {
    return;
  }
  // 过滤 Material-UI 的 findDOMNode 警告
  if (message.includes('findDOMNode is deprecated')) {
    return;
  }
  originalConsoleError.apply(console, args);
};

console.warn = (...args: any[]) => {
  const message = args[0]?.toString() || '';
  // 过滤 React Router 的 future flag 警告
  if (message.includes('React Router Future Flag Warning')) {
    return;
  }
  originalConsoleWarn.apply(console, args);
};

// ==================== 全局测试工具 ====================

// 等待异步操作完成
export const waitForAsync = (ms = 0) =>
  new Promise(resolve => setTimeout(resolve, ms));

// 模拟网络延迟
export const simulateNetworkDelay = (ms = 100) =>
  new Promise(resolve => setTimeout(resolve, ms));
