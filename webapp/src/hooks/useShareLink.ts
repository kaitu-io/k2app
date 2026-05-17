import { useState, useCallback, useRef } from 'react';
import { cloudApi } from '../services/cloud-api';


interface ShareLinkCache {
  [cacheKey: string]: {  // Format: "{code}:{days}"
    link: string;
    expiresAt: number; // Timestamp in milliseconds
  };
}

interface ShareLinkData {
  code: string;
  shareLink: string;
  expiresAt: number; // Unix timestamp in seconds
}

/**
 * Hook for managing invite code share links with caching
 *
 * - Fetches short links from backend API with customizable expiration
 * - Caches links for 1 hour per invite code and expiration days
 * - Automatically clears expired cache
 */
export function useShareLink() {
  const [loading, setLoading] = useState(false);
  const cacheRef = useRef<ShareLinkCache>({});

  /**
   * Get share link for an invite code with specified expiration
   * @param inviteCode - The invite code
   * @param expiresInDays - Link expiration in days (default: 7)
   * @returns The share link or null if failed
   */
  const getShareLink = useCallback(async (
    inviteCode: string,
    expiresInDays: number = 7
  ): Promise<string | null> => {
    // Build cache key with expiration
    const cacheKey = `${inviteCode}:${expiresInDays}`;

    // Check cache first
    const cached = cacheRef.current[cacheKey];
    if (cached && Date.now() < cached.expiresAt) {
      console.log(`[useShareLink] Cache hit for ${cacheKey}`);
      return cached.link;
    }

    // Cache miss or expired, fetch from API
    setLoading(true);

    try {
      console.log(`[useShareLink] Fetching share link for ${cacheKey}`);
      const apiResponse = await cloudApi.get<ShareLinkData>(`/api/invite/my-codes/${inviteCode}/share-link?expiresInDays=${expiresInDays}`);

      console.log(`[useShareLink] API Response:`, JSON.stringify(apiResponse));

      if (apiResponse.code === 0 && apiResponse.data) {
        const shareLinkData = apiResponse.data;
        const shareLink = shareLinkData.shareLink;

        // Cache for 1 hour
        const cacheExpiresAt = Date.now() + (60 * 60 * 1000);
        cacheRef.current[cacheKey] = {
          link: shareLink,
          expiresAt: cacheExpiresAt,
        };

        console.log(`[useShareLink] Fetched and cached share link for ${cacheKey}: ${shareLink}`);
        return shareLink;
      } else {
        console.error(`[useShareLink] API error for ${cacheKey}:`, apiResponse.code, apiResponse.message);
        return null;
      }
    } catch (err) {
      console.error(`[useShareLink] Error fetching share link for ${cacheKey}:`, err);
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  return {
    getShareLink,
    loading,
  };
}
