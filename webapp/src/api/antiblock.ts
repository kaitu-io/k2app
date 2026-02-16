const STORAGE_KEY = 'k2_entry_url';
export const DEFAULT_ENTRY = 'https://w.app.52j.me';
export const DECRYPTION_KEY =
  '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';
export const CDN_SOURCES = [
  'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/config.js',
  'https://cdn.statically.io/gh/kaitu-io/ui-theme/dist/config.js',
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
      rawKey.buffer as ArrayBuffer,
      'AES-GCM',
      false,
      ['decrypt'],
    );
    const plain = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: iv.buffer as ArrayBuffer },
      key,
      ciphertext.buffer as ArrayBuffer,
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

function loadScript(url: string, timeoutMs = 10000): Promise<AntiblockConfig | null> {
  return new Promise((resolve) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const w = window as any;
    delete w[JSONP_GLOBAL];

    const script = document.createElement('script');
    const timer = setTimeout(() => {
      script.remove();
      resolve(null);
    }, timeoutMs);

    script.onload = () => {
      clearTimeout(timer);
      const config = w[JSONP_GLOBAL] as AntiblockConfig | undefined;
      delete w[JSONP_GLOBAL];
      script.remove();
      resolve(config ?? null);
    };
    script.onerror = () => {
      clearTimeout(timer);
      script.remove();
      resolve(null);
    };
    script.src = url;
    document.head.appendChild(script);
  });
}

async function fetchEntryFromCDN(): Promise<string | null> {
  for (const url of CDN_SOURCES) {
    try {
      const config = await loadScript(url);
      if (!config || config.v !== 1 || typeof config.data !== 'string') continue;
      const plaintext = await decrypt(config.data, DECRYPTION_KEY);
      if (!plaintext) continue;
      const parsed = JSON.parse(plaintext) as { entries?: string[] };
      if (parsed.entries && parsed.entries.length > 0) {
        localStorage.setItem(STORAGE_KEY, parsed.entries[0]!);
        return parsed.entries[0]!;
      }
    } catch {
      continue;
    }
  }
  return null;
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
  return entry ?? DEFAULT_ENTRY;
}

function refreshEntryInBackground(): void {
  fetchEntryFromCDN().catch(() => {});
}
