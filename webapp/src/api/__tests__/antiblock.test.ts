import { describe, it, expect, vi, beforeEach } from 'vitest';
import { resolveEntry } from '../antiblock';

describe('resolveEntry', () => {
  let mockFetch: ReturnType<typeof vi.fn>;
  let mockLocalStorage: {
    getItem: ReturnType<typeof vi.fn>;
    setItem: ReturnType<typeof vi.fn>;
    removeItem: ReturnType<typeof vi.fn>;
  };

  beforeEach(() => {
    mockFetch = vi.fn();
    vi.stubGlobal('fetch', mockFetch);
    mockLocalStorage = {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    };
    vi.stubGlobal('localStorage', mockLocalStorage);
  });

  it('returns cached entry from localStorage', async () => {
    mockLocalStorage.getItem.mockReturnValue('https://cached.example.com');
    // Mock fetch for background refresh (should not block)
    mockFetch.mockRejectedValue(new Error('network error'));

    const entry = await resolveEntry();
    expect(entry).toBe('https://cached.example.com');
  });

  it('fetches from CDN when no cache', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);

    const encoded = btoa('https://fresh.example.com');
    mockFetch.mockResolvedValueOnce({
      ok: true,
      text: () =>
        Promise.resolve(`callback(${JSON.stringify({ entries: [encoded] })})`),
    });

    const entry = await resolveEntry();
    expect(entry).toBe('https://fresh.example.com');
    expect(mockLocalStorage.setItem).toHaveBeenCalledWith('k2_entry_url', 'https://fresh.example.com');
  });

  it('falls back to default when CDN fails', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);
    mockFetch.mockRejectedValue(new Error('network error'));

    const entry = await resolveEntry();
    expect(entry).toBe('https://w.app.52j.me');
  });

  it('tries second CDN source if first fails', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);

    // First CDN fails
    mockFetch.mockRejectedValueOnce(new Error('fail'));

    // Second CDN succeeds
    const encoded = btoa('https://second-cdn.example.com');
    mockFetch.mockResolvedValueOnce({
      ok: true,
      text: () =>
        Promise.resolve(`callback(${JSON.stringify({ entries: [encoded] })})`),
    });

    const entry = await resolveEntry();
    expect(entry).toBe('https://second-cdn.example.com');
  });

  it('skips non-http entries after decoding', async () => {
    mockLocalStorage.getItem.mockReturnValue(null);

    const invalidEntry = btoa('ftp://invalid.example.com');
    const validEntry = btoa('https://valid.example.com');
    mockFetch.mockResolvedValueOnce({
      ok: true,
      text: () =>
        Promise.resolve(`callback(${JSON.stringify({ entries: [invalidEntry, validEntry] })})`),
    });

    const entry = await resolveEntry();
    expect(entry).toBe('https://valid.example.com');
  });
});
