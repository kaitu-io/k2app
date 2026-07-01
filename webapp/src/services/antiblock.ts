import { decrypt as _decrypt, loadJsonp } from './antiblock-crypto';

const STORAGE_KEY = 'k2_entry_url';
export const DEFAULT_ENTRY = 'https://k2.52j.me';
export const DECRYPTION_KEY =
  '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';

// Re-export decrypt so existing importers that pull it from antiblock.ts
// continue to work without any change to their import paths.
export { decrypt } from './antiblock-crypto';

// jsdelivr mirrors — raced simultaneously (Happy Eyeballs)
// Same repo path, different edge networks for redundancy in blocked regions
export const CDN_SOURCES = [
  'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://testingcf.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://gcore.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://cdn.jsdmirror.com/gh/kaitu-io/ui-theme@dist/config.js',
  'https://cdn.jsdmirror.cn/gh/kaitu-io/ui-theme@dist/config.js',
  'https://jsd.onmicrosoft.cn/gh/kaitu-io/ui-theme@dist/config.js',
];

// ---------------------------------------------------------------------------
// JSONP global name used by the CDN config script
// ---------------------------------------------------------------------------

const JSONP_GLOBAL = '__k2ac';

interface AntiblockConfig {
  v: number;
  data: string;
}

// ---------------------------------------------------------------------------
// Happy Eyeballs — race all CDN mirrors, first valid config wins
// ---------------------------------------------------------------------------

async function decryptConfig(config: AntiblockConfig): Promise<string | null> {
  if (!config || config.v !== 1 || typeof config.data !== 'string') return null;
  const plaintext = await _decrypt(config.data, DECRYPTION_KEY);
  if (!plaintext) return null;
  const parsed = JSON.parse(plaintext) as { entries?: string[] };
  if (parsed.entries && parsed.entries.length > 0) return parsed.entries[0]!;
  return null;
}

// Promise.any polyfill — target is ES2020, Promise.any requires ES2021
function promiseAny<T>(promises: Promise<T>[]): Promise<T> {
  return new Promise((resolve, reject) => {
    let remaining = promises.length;
    if (remaining === 0) {
      reject(new Error('All promises were rejected'));
      return;
    }
    promises.forEach((p) => {
      p.then(resolve, () => {
        if (--remaining === 0) reject(new Error('All promises were rejected'));
      });
    });
  });
}

async function fetchEntryFromCDN(): Promise<string | null> {
  const candidates = CDN_SOURCES.map((url) =>
    loadJsonp(url, JSONP_GLOBAL).then(async (config) => {
      if (!config) throw new Error(`no config from ${url}`);
      const entry = await decryptConfig(config);
      if (!entry) throw new Error(`decrypt failed from ${url}`);
      return entry;
    }),
  );

  if (candidates.length === 0) return null;

  try {
    const entry = await promiseAny(candidates);
    localStorage.setItem(STORAGE_KEY, entry);
    return entry;
  } catch {
    console.warn('[Antiblock] all CDN sources failed');
    return null;
  }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export async function resolveEntry(): Promise<string> {
  const cached = localStorage.getItem(STORAGE_KEY);
  if (cached) {
    refreshEntryInBackground();
    return cached;
  }
  const entry = await fetchEntryFromCDN();
  const result = entry ?? DEFAULT_ENTRY;
  if (!entry) {
    console.warn('[Antiblock] CDN failed, using default:', DEFAULT_ENTRY);
  }
  return result;
}

function refreshEntryInBackground(): void {
  fetchEntryFromCDN().catch(() => {});
}
