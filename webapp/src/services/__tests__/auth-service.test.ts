import { describe, it, expect, vi, beforeEach } from 'vitest';
import { authService } from '../auth-service';

vi.mock('../antiblock', () => ({
  resolveEntry: vi.fn(async () => 'https://api.example.com'),
}));

describe('authService.buildSubsUrl', () => {
  beforeEach(() => {
    vi.spyOn(authService, 'getCredentials').mockResolvedValue({ udid: 'UDID', token: 'TOK' });
  });

  it('builds k2subs URL with country query', async () => {
    const url = await authService.buildSubsUrl('jp');
    expect(url).toBe('k2subs://UDID:TOK@api.example.com/api/subs?country=jp');
  });

  it('omits country query when country is null', async () => {
    const url = await authService.buildSubsUrl(null);
    expect(url).toBe('k2subs://UDID:TOK@api.example.com/api/subs');
  });

  it('lowercases country code', async () => {
    const url = await authService.buildSubsUrl('JP');
    expect(url).toBe('k2subs://UDID:TOK@api.example.com/api/subs?country=jp');
  });
});
