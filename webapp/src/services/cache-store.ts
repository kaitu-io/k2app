/**
 * CacheStore - 通用缓存存储层
 *
 * 特性：
 * - 双层缓存：内存 + localStorage
 * - TTL 支持（可选）
 * - 类型安全
 * - 自动序列化/反序列化
 * - 缓存键命名空间
 */

export interface CacheEntry<T> {
  data: T;
  expireAt?: number; // undefined 表示永不过期
}

/** 缓存变更订阅回调。参数是发生变更的键。 */
export type CacheListener = (key: string) => void;

export interface CacheOptions {
  /** 缓存过期时间（秒），undefined 表示永不过期 */
  ttl?: number;
  /** 是否持久化到 localStorage，默认 true */
  persist?: boolean;
}

const CACHE_PREFIX = 'kaitu_cache:';

/**
 * 通用缓存存储类
 * 支持泛型，提供类型安全的缓存操作
 */
export class CacheStore {
  private memoryCache = new Map<string, CacheEntry<any>>();
  private listeners = new Map<string, Set<CacheListener>>();

  /**
   * 获取完整的缓存键
   */
  private getFullKey(key: string): string {
    return `${CACHE_PREFIX}${key}`;
  }

  /**
   * 订阅某个键的变更。set / delete / clear / clearExpired 影响到该键时回调。
   *
   * 存在的理由：缓存是全应用共享的单例，但读它的 hook（useUser 等）把值拷进各自的
   * useState。没有广播时，一个组件写入的新数据其余组件永远看不到——iOS 购买成功后
   * 授权日期不刷新就是这么来的（写入方是购买面板，显示方是父级与 affordance，
   * 三份 state 互不相干）。
   *
   * @returns 取消订阅函数（在 useEffect 里直接 return 它）
   */
  subscribe(key: string, listener: CacheListener): () => void {
    let set = this.listeners.get(key);
    if (!set) {
      set = new Set();
      this.listeners.set(key, set);
    }
    set.add(listener);
    return () => {
      const current = this.listeners.get(key);
      if (!current) return;
      current.delete(listener);
      if (current.size === 0) this.listeners.delete(key);
    };
  }

  /**
   * 通知某个键的订阅者。单个 listener 抛错不得影响其余 listener，也不得让写入方
   * 的 set/delete 失败——缓存写入是主流程，通知是副作用。
   */
  private notify(key: string): void {
    const set = this.listeners.get(key);
    if (!set || set.size === 0) return;
    // 快照迭代：listener 内部可能取消订阅，直接遍历 Set 会漏掉后续项。
    for (const listener of [...set]) {
      try {
        listener(key);
      } catch (e) {
        console.error('[CacheStore] listener threw for key:', key, e);
      }
    }
  }

  /**
   * 设置缓存
   */
  set<T>(key: string, data: T, options?: CacheOptions): void {
    const { ttl, persist = true } = options || {};

    const entry: CacheEntry<T> = {
      data,
      expireAt: ttl ? Date.now() + ttl * 1000 : undefined,
    };

    // 写入内存缓存
    this.memoryCache.set(key, entry);

    // 持久化到 localStorage
    if (persist) {
      try {
        const fullKey = this.getFullKey(key);
        localStorage.setItem(fullKey, JSON.stringify(entry));
      } catch (e) {
        console.warn('[CacheStore] Failed to persist cache:', key, e);
      }
    }

    // 内存已是新值，localStorage 尽力而为——通知放最后，订阅者读到的必然是新值。
    this.notify(key);
  }

  /**
   * 获取缓存
   * @param key 缓存键
   * @param allowExpired 是否允许返回过期数据（用于 fallback 场景）
   * @returns 缓存数据，未找到或已过期（且不允许过期）返回 null
   */
  get<T>(key: string, allowExpired = false): T | null {
    // 优先从内存缓存读取
    let entry = this.memoryCache.get(key) as CacheEntry<T> | undefined;

    // 内存缓存未命中，尝试从 localStorage 读取
    if (!entry) {
      const fullKey = this.getFullKey(key);
      try {
        const stored = localStorage.getItem(fullKey);
        if (stored) {
          entry = JSON.parse(stored) as CacheEntry<T>;
          // 恢复到内存缓存
          this.memoryCache.set(key, entry);
        }
      } catch (e) {
        console.warn('[CacheStore] Failed to read cache from localStorage:', key, e);
        // 解析失败，删除损坏的缓存条目
        try {
          localStorage.removeItem(fullKey);
        } catch {
          // ignore cleanup error
        }
      }
    }

    if (!entry) {
      return null;
    }

    // 检查是否过期
    if (entry.expireAt && Date.now() > entry.expireAt) {
      if (!allowExpired) {
        return null;
      }
      // allowExpired=true 时返回过期数据（用于 fallback）
      console.debug('[CacheStore] Returning expired cache for fallback:', key);
    }

    return entry.data;
  }

  /**
   * 获取缓存条目（包含元数据）
   */
  getEntry<T>(key: string): CacheEntry<T> | null {
    let entry = this.memoryCache.get(key) as CacheEntry<T> | undefined;

    if (!entry) {
      const fullKey = this.getFullKey(key);
      try {
        const stored = localStorage.getItem(fullKey);
        if (stored) {
          entry = JSON.parse(stored) as CacheEntry<T>;
          this.memoryCache.set(key, entry);
        }
      } catch (e) {
        console.warn('[CacheStore] Failed to read cache entry:', key, e);
        // 解析失败，删除损坏的缓存条目
        try {
          localStorage.removeItem(fullKey);
        } catch {
          // ignore cleanup error
        }
      }
    }

    return entry || null;
  }

  /**
   * 检查缓存是否存在且未过期
   */
  has(key: string): boolean {
    return this.get(key) !== null;
  }

  /**
   * 检查缓存是否过期
   */
  isExpired(key: string): boolean {
    const entry = this.getEntry(key);
    if (!entry) return true;
    if (!entry.expireAt) return false;
    return Date.now() > entry.expireAt;
  }

  /**
   * 删除缓存
   */
  delete(key: string): void {
    this.memoryCache.delete(key);
    try {
      const fullKey = this.getFullKey(key);
      localStorage.removeItem(fullKey);
    } catch (e) {
      console.warn('[CacheStore] Failed to delete cache:', key, e);
    }
    this.notify(key);
  }

  /**
   * 清空所有缓存
   */
  clear(): void {
    this.memoryCache.clear();
    try {
      // 只清除带有缓存前缀的 localStorage 项
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key?.startsWith(CACHE_PREFIX)) {
          keysToRemove.push(key);
        }
      }
      keysToRemove.forEach(key => localStorage.removeItem(key));
    } catch (e) {
      console.warn('[CacheStore] Failed to clear cache:', e);
    }
    // 全清后每个订阅者持有的值都已失效（典型场景：登出）。按订阅键通知，
    // 而不是按被删的缓存键——某个键此刻恰好无缓存，不代表订阅者不需要知道。
    for (const key of [...this.listeners.keys()]) {
      this.notify(key);
    }
  }

  /**
   * 清除所有过期的缓存
   */
  clearExpired(): void {
    const now = Date.now();
    const evicted: string[] = [];

    // 清除内存缓存中过期的项
    for (const [key, entry] of this.memoryCache.entries()) {
      if (entry.expireAt && now > entry.expireAt) {
        this.memoryCache.delete(key);
        evicted.push(key);
      }
    }

    // 清除 localStorage 中过期的项
    try {
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const fullKey = localStorage.key(i);
        if (fullKey?.startsWith(CACHE_PREFIX)) {
          const stored = localStorage.getItem(fullKey);
          if (stored) {
            try {
              const entry = JSON.parse(stored) as CacheEntry<any>;
              if (entry.expireAt && now > entry.expireAt) {
                keysToRemove.push(fullKey);
              }
            } catch {
              // 解析失败的也删除
              keysToRemove.push(fullKey);
            }
          }
        }
      }
      keysToRemove.forEach(key => localStorage.removeItem(key));
    } catch (e) {
      console.warn('[CacheStore] Failed to clear expired cache:', e);
    }
    evicted.forEach(key => this.notify(key));
  }
}

// 导出单例
export const cacheStore = new CacheStore();
