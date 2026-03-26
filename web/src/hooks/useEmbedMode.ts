"use client";

import { useState, useEffect } from 'react';
import { useSearchParams } from 'next/navigation';

export interface EmbedModeConfig {
  isEmbedded: boolean;
  showNavigation: boolean;
  showFooter: boolean;
  compactLayout: boolean;
  authToken: string | null;
  embedTheme: 'auto' | 'light' | 'dark' | null;
}

/**
 * Hook to detect and configure embed mode for pages
 * Checks for ?embed=true parameter or #embed hash
 * Extracts auth_token for seamless authentication in embed mode
 * Extracts theme parameter (auto, light, dark) for embed theme control
 * Sets up external link handling for embedded mode
 */
export function useEmbedMode(): EmbedModeConfig {
  const searchParams = useSearchParams();
  const [isEmbedded, setIsEmbedded] = useState(false);
  const [authToken, setAuthToken] = useState<string | null>(null);
  const [embedTheme, setEmbedTheme] = useState<'auto' | 'light' | 'dark' | null>(null);

  useEffect(() => {
    // Check for embed parameter in URL
    const embed = searchParams.get('embed');
    const hash = window.location.hash;
    
    // Check for ?embed=true or #embed
    if (embed === 'true' || hash === '#embed') {
      setIsEmbedded(true);
      
      // Extract theme parameter for embed mode
      const theme = searchParams.get('theme');
      if (theme && ['auto', 'light', 'dark'].includes(theme)) {
        setEmbedTheme(theme as 'auto' | 'light' | 'dark');
        console.log('[useEmbedMode] Theme parameter extracted:', theme);
      } else if (theme) {
        console.warn('[useEmbedMode] Invalid theme parameter, falling back to auto:', theme);
        setEmbedTheme('auto');
      } else {
        setEmbedTheme('auto'); // Default to auto if no theme parameter
      }
      
      // Extract auth token from URL parameters for seamless authentication
      const token = searchParams.get('auth_token');
      console.log('[useEmbedMode] URL parameters check:');
      console.log('[useEmbedMode] - embed:', embed);
      console.log('[useEmbedMode] - theme:', theme);
      console.log('[useEmbedMode] - auth_token present:', !!token);
      console.log('[useEmbedMode] - auth_token length:', token?.length || 0);
      
      if (token && token.length > 0) {
        setAuthToken(token);
        console.log('[useEmbedMode] Auth token extracted from URL parameters successfully');
        console.log('[useEmbedMode] Token preview:', token.substring(0, 50) + '...');
        
        // Store token in localStorage for API requests
        try {
          localStorage.setItem('embed_auth_token', token);
          console.log('[useEmbedMode] Token stored in localStorage as embed_auth_token');
          
          // Verify storage
          const storedToken = localStorage.getItem('embed_auth_token');
          console.log('[useEmbedMode] Storage verification:', storedToken === token ? 'SUCCESS' : 'FAILED');
        } catch (error) {
          console.warn('[useEmbedMode] Failed to store auth token:', error);
        }
      } else {
        console.log('[useEmbedMode] No auth_token found in URL parameters');
      }
    }
  }, [searchParams]);

  // Add external link handler for embedded mode
  useEffect(() => {
    // Only activate in embedded mode
    if (!isEmbedded) return;

    /**
     * In embed mode, the parent window (desktop app / dev server) is always
     * on a different origin (tauri://localhost, http://localhost:5173, etc.).
     * We cannot know the parent's origin from within the iframe, so we use '*'.
     * Security is handled on the receiving side which validates event.origin.
     */
    const getTargetOrigin = (): string => {
      return '*';
    };

    const handleLinkClick = (e: Event) => {
      const target = e.target as HTMLElement;
      const link = target.closest('a');

      // Validate the link and its URL
      if (!link?.href) return;

      // Sanitize the URL to prevent XSS
      let sanitizedUrl: string;
      try {
        const url = new URL(link.href);
        // Only allow http and https protocols for security
        if (!['http:', 'https:'].includes(url.protocol)) {
          console.warn('[useEmbedMode] Blocked non-HTTP protocol:', url.protocol);
          return;
        }

        // Additional security: block suspicious domains that could be used for attacks
        const suspiciousDomains = ['javascript', 'data', 'vbscript', 'about'];
        const hostname = url.hostname.toLowerCase();
        if (suspiciousDomains.some(domain => hostname.includes(domain))) {
          console.warn('[useEmbedMode] Blocked suspicious domain:', hostname);
          return;
        }

        sanitizedUrl = url.toString();
      } catch {
        console.warn('[useEmbedMode] Invalid URL format:', link.href);
        return;
      }

      // Check if it's an external link (with target="_blank" or different origin)
      const isExternal = link.target === '_blank' ||
                        link.href.startsWith('http://') ||
                        link.href.startsWith('https://');

      if (link && isExternal) {
        e.preventDefault();
        e.stopPropagation();

        const targetOrigin = getTargetOrigin();

        // Send message to parent window (desktop app)
        if (window.parent && window.parent !== window) {
          try {
            window.parent.postMessage({
              type: 'external-link',
              url: sanitizedUrl,
              timestamp: Date.now()
            }, targetOrigin);
            console.log('[useEmbedMode] Sent external link to parent:', sanitizedUrl);
          } catch (error) {
            console.error('[useEmbedMode] Failed to send message to parent:', error);
            window.open(sanitizedUrl, '_blank', 'noopener,noreferrer');
          }
        } else {
          window.open(sanitizedUrl, '_blank', 'noopener,noreferrer');
        }
      }
    };

    // Add event listener
    document.addEventListener('click', handleLinkClick, true);

    // Cleanup
    return () => {
      document.removeEventListener('click', handleLinkClick, true);
    };
  }, [isEmbedded]);

  // Disable right-click context menu in embedded mode for security
  useEffect(() => {
    if (!isEmbedded) return;

    const handleContextMenu = (e: MouseEvent) => {
      e.preventDefault();
      return false;
    };

    // Add event listener
    document.addEventListener('contextmenu', handleContextMenu);
    console.log('[useEmbedMode] Context menu disabled for embed mode');

    // Cleanup
    return () => {
      document.removeEventListener('contextmenu', handleContextMenu);
      console.log('[useEmbedMode] Context menu handler removed');
    };
  }, [isEmbedded]);

  return {
    isEmbedded,
    showNavigation: !isEmbedded,
    showFooter: !isEmbedded,
    compactLayout: isEmbedded,
    authToken,
    embedTheme
  };
}