import { useCallback } from 'react';
import { cloudApi } from '../api/cloud';

export function useShareLink() {
  const generateShareLink = useCallback(async (inviteCode: string): Promise<string> => {
    const resp = await cloudApi.createShareLink(inviteCode);
    const data = resp.data as { url: string };
    return data.url;
  }, []);

  return { generateShareLink };
}
