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
     * Validates the target origin for postMessage security
     * Only allows trusted origins to prevent security vulnerabilities
     */
    const getValidatedTargetOrigin = (): string | null => {
      // Get the parent window's origin if available
      try {
        // Check if we have a valid parent window
        if (!window.parent || window.parent === window) {
          return null;
        }

        // For security, we validate against known safe origins
        // In embed mode, we typically expect parent to be from same origin or trusted domains
        const currentOrigin = window.location.origin;

        // For desktop apps using file:// or custom protocols, we might need to be more lenient
        // but we should still validate the context
        const isFileProtocol = window.location.protocol === 'file:';
        const isLocalhost = window.location.hostname === 'localhost' ||
                           window.location.hostname === '127.0.0.1';

        // Allow localhost and file protocol for development and desktop app contexts
        if (isFileProtocol || isLocalhost) {
          return currentOrigin;
        }

        // For production, only allow same origin unless specifically configured
        return currentOrigin;
      } catch (error) {
        console.warn('[useEmbedMode] Failed to determine target origin:', error);
        return null;
      }
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

        // Get validated target origin for secure postMessage
        const targetOrigin = getValidatedTargetOrigin();

        // Send message to parent window (desktop app) with proper origin validation
        if (window.parent && window.parent !== window && targetOrigin) {
          try {
            window.parent.postMessage({
              type: 'external-link',
              url: sanitizedUrl,
              timestamp: Date.now() // Add timestamp for message freshness validation
            }, targetOrigin);
            console.log('[useEmbedMode] Sent external link to parent with origin validation:', sanitizedUrl);
          } catch (error) {
            console.error('[useEmbedMode] Failed to send message to parent:', error);
            // Fallback: open in current window if message fails
            window.open(sanitizedUrl, '_blank', 'noopener,noreferrer');
          }
        } else {
          // Fallback: open in new tab if no parent window or invalid origin
          if (!targetOrigin) {
            console.warn('[useEmbedMode] No valid target origin, opening in new tab');
          }
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