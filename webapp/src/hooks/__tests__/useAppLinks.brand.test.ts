import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { brandConfig } from '../../brand';

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: vi.fn().mockResolvedValue({ code: -1, message: 'offline' }) },
}));
vi.mock('../../services/cache-store', () => ({
  cacheStore: { get: vi.fn(() => null), set: vi.fn() },
}));
vi.mock('react-i18next', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-i18next')>()),
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'zh-CN' } }),
}));

import { useAppLinks } from '../useAppLinks';

describe('useAppLinks brand fallback', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('falls back to brandConfig.baseURL when app config is unavailable', async () => {
    const { result } = renderHook(() => useAppLinks());
    await waitFor(() => {
      expect(result.current.links.privacyPolicyUrl).toBe(`${brandConfig.baseURL}/privacy`);
      expect(result.current.links.privacyPolicyUrl).not.toContain('https://kaitu.io');
    });
  });
});
