import { describe, it, expect, vi, beforeEach } from 'vitest';

// Stub `server-only` — it's a side-effect module that errors when imported in
// non-RSC contexts. Vitest runs in node, not RSC, so we have to neutralize it.
vi.mock('server-only', () => ({}));

vi.mock('next/headers', () => ({
  headers: vi.fn(),
}));

import { headers } from 'next/headers';
import { getRequestPathname } from '../request-pathname';

const mockedHeaders = headers as unknown as ReturnType<typeof vi.fn>;

function mockHeaderValue(value: string | null) {
  mockedHeaders.mockResolvedValue({
    get: (key: string) => (key === 'x-pathname' ? value : null),
  });
}

describe('getRequestPathname', () => {
  beforeEach(() => {
    mockedHeaders.mockReset();
  });

  it('returns pathname from x-pathname header', async () => {
    mockHeaderValue('/install');
    expect(await getRequestPathname()).toBe('/install');
  });

  it('normalizes "/" to empty string (homepage)', async () => {
    mockHeaderValue('/');
    expect(await getRequestPathname()).toBe('');
  });

  it('returns empty when header absent', async () => {
    mockHeaderValue(null);
    expect(await getRequestPathname()).toBe('');
  });

  it('preserves nested paths', async () => {
    mockHeaderValue('/k2/comparison');
    expect(await getRequestPathname()).toBe('/k2/comparison');
  });

  it('preserves paths with trailing segments', async () => {
    mockHeaderValue('/purchase');
    expect(await getRequestPathname()).toBe('/purchase');
  });
});
