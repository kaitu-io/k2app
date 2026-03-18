const STORAGE_KEY = 'k2_entry_url';
export const DEFAULT_ENTRY = 'https://k2.52j.me';
export const DECRYPTION_KEY =
  '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';

// jsdelivr mirrors — raced simultaneously (Happy Eyeballs)
// Same repo path, different edge networks for redundancy in blocked regions
export const CDN_SOURCES = [
  'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://testingcf.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://gcore.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://cdn.jsdmirror.com/gh/kaitu-io/ui-theme@dist/config.js',
  'https://jsd.onmicrosoft.cn/gh/kaitu-io/ui-theme@dist/config.js',
];

// ---------------------------------------------------------------------------
// Base64 decoder (no atob usage — required by project rules)
// ---------------------------------------------------------------------------

function base64ToBytes(b64: string): Uint8Array {
  const chars =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  const lookup = new Uint8Array(256);
  for (let i = 0; i < chars.length; i++) {
    lookup[chars.charCodeAt(i)] = i;
  }

  const len = b64.length;
  const padLen = b64.endsWith('==') ? 2 : b64.endsWith('=') ? 1 : 0;
  const byteLen = (len * 3) / 4 - padLen;
  const bytes = new Uint8Array(byteLen);

  let j = 0;
  for (let i = 0; i < len; i += 4) {
    const a = lookup[b64.charCodeAt(i)]!;
    const b = lookup[b64.charCodeAt(i + 1)]!;
    const c = lookup[b64.charCodeAt(i + 2)]!;
    const d = lookup[b64.charCodeAt(i + 3)]!;
    bytes[j++] = (a << 2) | (b >> 4);
    if (j < byteLen) bytes[j++] = ((b & 0xf) << 4) | (c >> 2);
    if (j < byteLen) bytes[j++] = ((c & 0x3) << 6) | d;
  }
  return bytes;
}

// ---------------------------------------------------------------------------
// Hex decoder
// ---------------------------------------------------------------------------

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return bytes;
}

// ---------------------------------------------------------------------------
// AES-256-GCM decryption via Web Crypto API
// ---------------------------------------------------------------------------

export async function decrypt(
  encoded: string,
  keyHex: string,
): Promise<string | null> {
  try {
    const data = base64ToBytes(encoded);
    const iv = data.slice(0, 12);
    const ciphertext = data.slice(12);
    const rawKey = hexToBytes(keyHex);
    const key = await crypto.subtle.importKey(
      'raw',
      rawKey,
      'AES-GCM',
      false,
      ['decrypt'],
    );
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv },
      key,
      ciphertext,
    );
    return new TextDecoder().decode(plain);
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// JSONP loader — <script> tag injection (no CORS restrictions)
// Config script sets window.__k2ac = { v: 1, data: "<base64 ciphertext>" }
// ---------------------------------------------------------------------------

const JSONP_GLOBAL = '__k2ac';

interface AntiblockConfig {
  v: number;
  data: string;
}

function loadScript(url: string, timeoutMs = 5000): Promise<AntiblockConfig | null> {
  return new Promise((resolve) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const w = window as any;
    delete w[JSONP_GLOBAL];

    const script = document.createElement('script');
    const timer = setTimeout(() => {
      console.warn('[Antiblock] loadScript timeout:', url);
      script.remove();
      resolve(null);
    }, timeoutMs);

    script.onload = () => {
      clearTimeout(timer);
      const config = w[JSONP_GLOBAL] as AntiblockConfig | undefined;
      delete w[JSONP_GLOBAL];
      script.remove();
      if (!config) {
        console.warn('[Antiblock] loadScript: script loaded but no config found');
      }
      resolve(config ?? null);
    };
    script.onerror = (e) => {
      clearTimeout(timer);
      console.warn('[Antiblock] loadScript error:', url, e);
      script.remove();
      resolve(null);
    };
    script.src = url;
    document.head.appendChild(script);
  });
}

// ---------------------------------------------------------------------------
// Happy Eyeballs — race all CDN mirrors, first valid config wins
// ---------------------------------------------------------------------------

async function decryptConfig(config: AntiblockConfig): Promise<string | null> {
  if (!config || config.v !== 1 || typeof config.data !== 'string') return null;
  const plaintext = await decrypt(config.data, DECRYPTION_KEY);
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
    loadScript(url).then(async (config) => {
      if (!config) throw new Error(`no config from ${url}`);
      const entry = await decryptConfig(config);
      if (!entry) throw new Error(`decrypt failed from ${url}`);
      console.info('[Antiblock] resolved entry from:', url);
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
    console.info('[Antiblock] using cached entry:', cached);
    refreshEntryInBackground();
    return cached;
  }
  console.info('[Antiblock] no cache, fetching from CDN...');
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
