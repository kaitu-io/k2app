/**
 * CacheStore 单元测试
 *
 * 运行方式：
 * 1. 安装依赖: yarn add -D vitest @testing-library/react jsdom
 * 2. 运行测试: yarn vitest run
 */

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { CacheStore, CacheEntry } from '../cache-store';

describe('CacheStore', () => {
  let store: CacheStore;
  let localStorageMock: { [key: string]: string };

  beforeEach(() => {
    // 重置 localStorage mock
    localStorageMock = {};

    vi.stubGlobal('localStorage', {
      getItem: vi.fn((key: string) => localStorageMock[key] || null),
      setItem: vi.fn((key: string, value: string) => {
        localStorageMock[key] = value;
      }),
      removeItem: vi.fn((key: string) => {
        delete localStorageMock[key];
      }),
      clear: vi.fn(() => {
        localStorageMock = {};
      }),
      key: vi.fn((index: number) => Object.keys(localStorageMock)[index] || null),
      get length() {
        return Object.keys(localStorageMock).length;
      },
    });

    store = new CacheStore();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('set/get 基本操作', () => {
    it('应该能够设置和获取缓存数据', () => {
      const data = { name: 'test', value: 123 };
      store.set('testKey', data);

      const result = store.get<typeof data>('testKey');
      expect(result).toEqual(data);
    });

    it('应该在设置缓存时持久化到 localStorage', () => {
      const data = { name: 'test' };
      store.set('testKey', data);

      expect(localStorage.setItem).toHaveBeenCalledWith(
        'kaitu_cache:testKey',
        expect.any(String)
      );
    });

    it('persist=false 时不应该写入 localStorage', () => {
      const data = { name: 'test' };
      store.set('testKey', data, { persist: false });

      expect(localStorage.setItem).not.toHaveBeenCalled();
    });

    it('获取不存在的缓存应该返回 null', () => {
      const result = store.get('nonExistentKey');
      expect(result).toBeNull();
    });
  });

  describe('TTL 过期机制', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('未过期的缓存应该能正常获取', () => {
      store.set('testKey', 'testValue', { ttl: 60 }); // 60秒 TTL

      vi.advanceTimersByTime(30 * 1000); // 前进30秒

      const result = store.get('testKey');
      expect(result).toBe('testValue');
    });

    it('过期的缓存默认返回 null', () => {
      store.set('testKey', 'testValue', { ttl: 60 }); // 60秒 TTL

      vi.advanceTimersByTime(61 * 1000); // 前进61秒

      const result = store.get('testKey');
      expect(result).toBeNull();
    });

    it('allowExpired=true 时应该返回过期数据', () => {
      store.set('testKey', 'testValue', { ttl: 60 }); // 60秒 TTL

      vi.advanceTimersByTime(61 * 1000); // 前进61秒

      const result = store.get('testKey', true);
      expect(result).toBe('testValue');
    });

    it('没有设置 TTL 的缓存永不过期', () => {
      store.set('testKey', 'testValue'); // 无 TTL

      vi.advanceTimersByTime(365 * 24 * 60 * 60 * 1000); // 前进1年

      const result = store.get('testKey');
      expect(result).toBe('testValue');
    });
  });

  describe('localStorage 回退读取', () => {
    it('内存缓存未命中时应该从 localStorage 读取', () => {
      // 直接写入 localStorage（模拟应用重启后的场景）
      const entry: CacheEntry<string> = {
        data: 'persistedValue',
        expireAt: Date.now() + 60000,
      };
      localStorageMock['kaitu_cache:testKey'] = JSON.stringify(entry);

      // 新建 store（模拟应用重启）
      const newStore = new CacheStore();
      const result = newStore.get<string>('testKey');

      expect(result).toBe('persistedValue');
    });

    it('localStorage 解析失败时应该返回 null 并清理损坏数据', () => {
      localStorageMock['kaitu_cache:testKey'] = 'invalid json {{{';

      const result = store.get('testKey');

      expect(result).toBeNull();
      expect(localStorage.removeItem).toHaveBeenCalledWith('kaitu_cache:testKey');
    });
  });

  describe('delete 操作', () => {
    it('应该同时删除内存缓存和 localStorage', () => {
      store.set('testKey', 'testValue');
      store.delete('testKey');

      expect(store.get('testKey')).toBeNull();
      expect(localStorage.removeItem).toHaveBeenCalledWith('kaitu_cache:testKey');
    });
  });

  describe('clear 操作', () => {
    it('应该只清除带有 kaitu_cache: 前缀的项', () => {
      localStorageMock['kaitu_cache:key1'] = 'value1';
      localStorageMock['kaitu_cache:key2'] = 'value2';
      localStorageMock['other_key'] = 'should_not_be_deleted';

      store.clear();

      expect(localStorage.removeItem).toHaveBeenCalledWith('kaitu_cache:key1');
      expect(localStorage.removeItem).toHaveBeenCalledWith('kaitu_cache:key2');
      // other_key 不应该被删除，检查调用次数
      expect(localStorage.removeItem).toHaveBeenCalledTimes(2);
    });
  });

  describe('clearExpired 操作', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('应该清除过期的缓存条目', () => {
      store.set('expiredKey', 'value1', { ttl: 30 });
      store.set('validKey', 'value2', { ttl: 120 });

      vi.advanceTimersByTime(60 * 1000); // 前进60秒

      store.clearExpired();

      expect(store.get('expiredKey', true)).toBeNull(); // 即使 allowExpired 也应该返回 null（因为被清除了）
      expect(store.get('validKey')).toBe('value2');
    });
  });

  describe('isExpired 操作', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('未过期的缓存返回 false', () => {
      store.set('testKey', 'value', { ttl: 60 });
      expect(store.isExpired('testKey')).toBe(false);
    });

    it('过期的缓存返回 true', () => {
      store.set('testKey', 'value', { ttl: 60 });
      vi.advanceTimersByTime(61 * 1000);
      expect(store.isExpired('testKey')).toBe(true);
    });

    it('不存在的缓存返回 true', () => {
      expect(store.isExpired('nonExistentKey')).toBe(true);
    });

    it('无 TTL 的缓存返回 false', () => {
      store.set('testKey', 'value');
      vi.advanceTimersByTime(365 * 24 * 60 * 60 * 1000); // 1年后
      expect(store.isExpired('testKey')).toBe(false);
    });
  });

  describe('localStorage 异常处理', () => {
    it('localStorage.setItem 抛出异常时不应影响内存缓存', () => {
      vi.mocked(localStorage.setItem).mockImplementation(() => {
        throw new Error('QuotaExceededError');
      });

      // 不应该抛出异常
      expect(() => store.set('testKey', 'value')).not.toThrow();

      // 内存缓存应该正常工作
      expect(store.get('testKey')).toBe('value');
    });

    it('localStorage.getItem 抛出异常时应该返回 null', () => {
      vi.mocked(localStorage.getItem).mockImplementation(() => {
        throw new Error('SecurityError');
      });

      const result = store.get('testKey');
      expect(result).toBeNull();
    });
  });
});
