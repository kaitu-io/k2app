import { useState, useCallback, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { k2api } from '../services/k2api';


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
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
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
    setError(null);

    try {
      console.log(`[useShareLink] Fetching share link for ${cacheKey}`);
      const apiResponse = await k2api().exec<ShareLinkData>('api_request', {
        method: 'GET',
        path: `/api/invite/my-codes/${inviteCode}/share-link?expiresInDays=${expiresInDays}`,
      });

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
        setLoading(false);
        return shareLink;
      } else {
        console.error(`[useShareLink] API error for ${cacheKey}:`, apiResponse.code, apiResponse.message);
        setError(t('invite:invite.getShareLinkFailed'));
        setLoading(false);
        return null;
      }
    } catch (err) {
      console.error(`[useShareLink] Error fetching share link for ${cacheKey}:`, err);
      setError(t('invite:invite.getShareLinkFailed'));
      setLoading(false);
      return null;
    }
  }, []);

  /**
   * Clear cache for a specific invite code
   */
  const clearCache = useCallback((inviteCode: string) => {
    delete cacheRef.current[inviteCode];
    console.log(`[useShareLink] Cleared cache for code: ${inviteCode}`);
  }, []);

  /**
   * Clear all cached share links
   */
  const clearAllCache = useCallback(() => {
    cacheRef.current = {};
    console.log('[useShareLink] Cleared all cache');
  }, []);

  return {
    getShareLink,
    clearCache,
    clearAllCache,
    loading,
    error,
  };
}
