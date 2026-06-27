// ---------------------------------------------------------------------------
// Shared low-level crypto + JSONP primitives for antiblock modules.
// No atob usage — required by project rules.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Base64 decoder (no atob usage — required by project rules)
// ---------------------------------------------------------------------------

export function base64ToBytes(b64: string): Uint8Array {
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

export function hexToBytes(hex: string): Uint8Array {
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
// Generalized: parameterizes the JSONP global name so different modules can
// use distinct window globals without coupling.
// ---------------------------------------------------------------------------

export interface JsonpConfig {
  v: number;
  data: string;
}

export function loadJsonp(
  url: string,
  globalName: string,
  timeoutMs = 5000,
): Promise<JsonpConfig | null> {
  return new Promise((resolve) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const w = window as any;
    delete w[globalName];

    const script = document.createElement('script');
    const timer = setTimeout(() => {
      console.warn('[Antiblock] loadJsonp timeout:', url);
      script.remove();
      resolve(null);
    }, timeoutMs);

    script.onload = () => {
      clearTimeout(timer);
      const config = w[globalName] as JsonpConfig | undefined;
      delete w[globalName];
      script.remove();
      if (!config) {
        console.warn('[Antiblock] loadJsonp: script loaded but no config found');
      }
      resolve(config ?? null);
    };
    script.onerror = (e) => {
      clearTimeout(timer);
      console.warn('[Antiblock] loadJsonp error:', url, e);
      script.remove();
      resolve(null);
    };
    script.src = url;
    document.head.appendChild(script);
  });
}
