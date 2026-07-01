import { describe, it, expect, vi, beforeEach } from 'vitest';
import { mintGatewayCredential, discoverRouter } from '../private-node-service';

const postMock = vi.fn();
const getMock = vi.fn();
vi.mock('../cloud-api', () => ({
  cloudApi: { post: (...a: unknown[]) => postMock(...a), get: (...a: unknown[]) => getMock(...a) },
}));

describe('private-node-service router', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    postMock.mockResolvedValue({ code: 0, data: { url: 'k2subs://u:t@h/api/subs' } });
    getMock.mockResolvedValue({ code: 0, data: { candidates: [{ lanIP: '192.168.8.1', port: 1779 }] } });
  });

  it('mintGatewayCredential returns url', async () => {
    const url = await mintGatewayCredential();
    expect(url).toBe('k2subs://u:t@h/api/subs');
    expect(postMock).toHaveBeenCalledWith('/api/user/gateway-credential', {});
  });

  it('mintGatewayCredential returns empty string when data missing', async () => {
    postMock.mockResolvedValue({ code: 0 });
    const url = await mintGatewayCredential();
    expect(url).toBe('');
  });

  it('discoverRouter returns candidates', async () => {
    const c = await discoverRouter();
    expect(c).toHaveLength(1);
    expect(c[0].lanIP).toBe('192.168.8.1');
    expect(getMock).toHaveBeenCalledWith('/api/pair/discover');
  });

  it('discoverRouter returns empty array when data missing', async () => {
    getMock.mockResolvedValue({ code: 0 });
    const c = await discoverRouter();
    expect(c).toEqual([]);
  });
});
