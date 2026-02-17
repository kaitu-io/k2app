/**
 * Web Secure Storage Implementation
 *
 * 使用 Web Crypto API (AES-GCM) 对 localStorage 中的数据进行加密
 *
 * 特点：
 * - 数据在存储前使用 AES-256-GCM 加密
 * - 密钥由设备指纹派生（不硬编码）
 * - WebKit inspector 只能看到密文
 * - 零用户交互
 *
 * 安全性说明：
 * - 这不是"绝对安全"，但比明文 localStorage 安全得多
 * - 攻击者需要：1) 访问设备 2) 理解加密方案 3) 逆向密钥派生
 * - 适用于存储敏感配置，不适用于存储高价值密钥
 */

import type { ISecureStorage, StorageOptions } from '../types/kaitu-core';

// 存储条目结构
interface StorageEntry<T = any> {
  value: T;
  expiry?: number; // 过期时间戳
  createdAt: number;
}

// 存储前缀，避免与其他应用冲突
const STORAGE_PREFIX = '_k2_secure_';

// 缓存的加密密钥
let cachedKey: CryptoKey | null = null;

/**
 * 生成设备指纹（用于派生密钥）
 * 结合多个浏览器特征，在同一设备上保持稳定
 */
function generateDeviceFingerprint(): string {
  const components = [
    navigator.userAgent,
    navigator.language,
    screen.width.toString(),
    screen.height.toString(),
    screen.colorDepth.toString(),
    new Date().getTimezoneOffset().toString(),
    navigator.hardwareConcurrency?.toString() || 'unknown',
    // 可以添加更多特征
  ];

  // 添加一个固定的应用盐值
  components.push('kaitu-secure-storage-v1');

  return components.join('|');
}

/**
 * 从字符串派生 AES-256 密钥
 */
async function deriveKey(material: string): Promise<CryptoKey> {
  // 将字符串转换为字节
  const encoder = new TextEncoder();
  const materialBytes = encoder.encode(material);

  // 使用 SHA-256 哈希作为密钥材料
  const hashBuffer = await crypto.subtle.digest('SHA-256', materialBytes);

  // 导入为 AES-GCM 密钥
  return crypto.subtle.importKey('raw', hashBuffer, { name: 'AES-GCM' }, false, ['encrypt', 'decrypt']);
}

/**
 * 获取加密密钥（懒加载 + 缓存）
 */
async function getEncryptionKey(): Promise<CryptoKey> {
  if (cachedKey) {
    return cachedKey;
  }

  const fingerprint = generateDeviceFingerprint();
  cachedKey = await deriveKey(fingerprint);
  return cachedKey;
}

/**
 * 加密数据
 */
async function encrypt(data: string): Promise<string> {
  const key = await getEncryptionKey();
  const encoder = new TextEncoder();
  const dataBytes = encoder.encode(data);

  // 生成随机 IV（12 bytes for AES-GCM）
  const iv = crypto.getRandomValues(new Uint8Array(12));

  // 加密
  const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, dataBytes);

  // 组合 IV + 密文，使用 base64 编码
  const combined = new Uint8Array(iv.length + ciphertext.byteLength);
  combined.set(iv);
  combined.set(new Uint8Array(ciphertext), iv.length);

  // Convert to base64 without spread operator for better compatibility
  let binaryString = '';
  for (let i = 0; i < combined.length; i++) {
    binaryString += String.fromCharCode(combined[i]);
  }
  return btoa(binaryString);
}

/**
 * 解密数据
 */
async function decrypt(encryptedData: string): Promise<string> {
  try {
    const key = await getEncryptionKey();

    // 解码 base64 without Uint8Array.from for better compatibility
    const binaryString = atob(encryptedData);
    const combined = new Uint8Array(binaryString.length);
    for (let i = 0; i < binaryString.length; i++) {
      combined[i] = binaryString.charCodeAt(i);
    }

    // 分离 IV 和密文
    const iv = combined.slice(0, 12);
    const ciphertext = combined.slice(12);

    // 解密
    const decrypted = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ciphertext);

    const decoder = new TextDecoder();
    return decoder.decode(decrypted);
  } catch (error) {
    console.error('[SecureStorage] Decryption failed:', error);
    throw error;
  }
}

/**
 * 检查条目是否过期
 */
function isExpired(entry: StorageEntry): boolean {
  if (!entry.expiry) return false;
  return Date.now() > entry.expiry;
}

/**
 * Web Secure Storage 实现
 *
 * 使用 AES-256-GCM 加密数据后存储到 localStorage
 */
export const webSecureStorage: ISecureStorage = {
  async get<T = any>(key: string): Promise<T | null> {
    try {
      const storageKey = STORAGE_PREFIX + key;
      const encrypted = localStorage.getItem(storageKey);

      if (!encrypted) {
        return null;
      }

      // 解密
      const decrypted = await decrypt(encrypted);
      const entry: StorageEntry<T> = JSON.parse(decrypted);

      // 检查是否过期
      if (isExpired(entry)) {
        localStorage.removeItem(storageKey);
        return null;
      }

      return entry.value;
    } catch (error) {
      console.error('[SecureStorage] Get error:', error);
      // 如果解密失败（数据损坏），删除该键
      localStorage.removeItem(STORAGE_PREFIX + key);
      return null;
    }
  },

  async set<T = any>(key: string, value: T, options?: StorageOptions): Promise<void> {
    try {
      const storageKey = STORAGE_PREFIX + key;
      const entry: StorageEntry<T> = {
        value,
        createdAt: Date.now(),
      };

      if (options?.ttl) {
        entry.expiry = Date.now() + options.ttl;
      }

      // 序列化并加密
      const serialized = JSON.stringify(entry);
      const encrypted = await encrypt(serialized);

      localStorage.setItem(storageKey, encrypted);
    } catch (error) {
      console.error('[SecureStorage] Set error:', error);
      throw error;
    }
  },

  async remove(key: string): Promise<void> {
    const storageKey = STORAGE_PREFIX + key;
    localStorage.removeItem(storageKey);
  },

  async has(key: string): Promise<boolean> {
    try {
      const value = await this.get(key);
      return value !== null;
    } catch {
      return false;
    }
  },

  async clear(): Promise<void> {
    // 只清除带有我们前缀的键
    const keysToRemove: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && key.startsWith(STORAGE_PREFIX)) {
        keysToRemove.push(key);
      }
    }
    keysToRemove.forEach((key) => localStorage.removeItem(key));
  },

  async keys(): Promise<string[]> {
    const result: string[] = [];

    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key && key.startsWith(STORAGE_PREFIX)) {
        // 返回不带前缀的键名
        const cleanKey = key.slice(STORAGE_PREFIX.length);

        // 检查是否过期
        try {
          const value = await this.get(cleanKey);
          if (value !== null) {
            result.push(cleanKey);
          }
        } catch {
          // 解密失败，跳过
        }
      }
    }

    return result;
  },
};

/**
 * 创建自定义前缀的 secure storage 实例
 * 用于隔离不同模块的存储
 */
export function createSecureStorage(customPrefix: string): ISecureStorage {
  const prefix = `_k2_${customPrefix}_`;

  return {
    async get<T = any>(key: string): Promise<T | null> {
      try {
        const storageKey = prefix + key;
        const encrypted = localStorage.getItem(storageKey);

        if (!encrypted) {
          return null;
        }

        const decrypted = await decrypt(encrypted);
        const entry: StorageEntry<T> = JSON.parse(decrypted);

        if (isExpired(entry)) {
          localStorage.removeItem(storageKey);
          return null;
        }

        return entry.value;
      } catch (error) {
        console.error('[SecureStorage] Get error:', error);
        localStorage.removeItem(prefix + key);
        return null;
      }
    },

    async set<T = any>(key: string, value: T, options?: StorageOptions): Promise<void> {
      const storageKey = prefix + key;
      const entry: StorageEntry<T> = {
        value,
        createdAt: Date.now(),
      };

      if (options?.ttl) {
        entry.expiry = Date.now() + options.ttl;
      }

      const serialized = JSON.stringify(entry);
      const encrypted = await encrypt(serialized);
      localStorage.setItem(storageKey, encrypted);
    },

    async remove(key: string): Promise<void> {
      localStorage.removeItem(prefix + key);
    },

    async has(key: string): Promise<boolean> {
      const value = await this.get(key);
      return value !== null;
    },

    async clear(): Promise<void> {
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const storageKey = localStorage.key(i);
        if (storageKey && storageKey.startsWith(prefix)) {
          keysToRemove.push(storageKey);
        }
      }
      keysToRemove.forEach((k) => localStorage.removeItem(k));
    },

    async keys(): Promise<string[]> {
      const result: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const storageKey = localStorage.key(i);
        if (storageKey && storageKey.startsWith(prefix)) {
          const cleanKey = storageKey.slice(prefix.length);
          try {
            const value = await this.get(cleanKey);
            if (value !== null) {
              result.push(cleanKey);
            }
          } catch {
            // skip
          }
        }
      }
      return result;
    },
  };
}
