const STORAGE_KEY = 'k2_entry_url';
export const DEFAULT_ENTRY = 'https://w.app.52j.me';
export const CDN_SOURCES = [
  'https://cdn.jsdelivr.net/npm/unlock-it/config.js',
  'https://unpkg.com/unlock-it/config.js',
];
export const DECRYPTION_KEY = '';

export async function decrypt(
  _encoded: string,
  _keyHex: string,
): Promise<string | null> {
  return null;
}

export async function resolveEntry(): Promise<string> {
  const cached = localStorage.getItem(STORAGE_KEY);
  if (cached) {
    refreshEntryInBackground();
    return cached;
  }
  const entry = await fetchEntryFromCDN();
  return entry ?? DEFAULT_ENTRY;
}

async function fetchEntryFromCDN(): Promise<string | null> {
  for (const url of CDN_SOURCES) {
    try {
      const resp = await fetch(url);
      if (!resp.ok) continue;
      const text = await resp.text();
      // JSONP: extract config object
      const match = text.match(/\{[\s\S]*\}/);
      if (!match) continue;
      const config = JSON.parse(match[0]) as { entries?: string[] };
      if (config.entries && Array.isArray(config.entries)) {
        const entries = decodeEntries(config.entries);
        if (entries.length > 0) {
          localStorage.setItem(STORAGE_KEY, entries[0]!);
          return entries[0]!;
        }
      }
    } catch {
      continue;
    }
  }
  return null;
}

function decodeEntries(encoded: string[]): string[] {
  return encoded
    .map((e) => {
      try {
        return atob(e);
      } catch {
        return null;
      }
    })
    .filter((e): e is string => e !== null && e.startsWith('http'));
}

function refreshEntryInBackground(): void {
  fetchEntryFromCDN().catch(() => {});
}
