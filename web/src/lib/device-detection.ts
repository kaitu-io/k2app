// Device detection utilities for automatic download

export type DeviceType = 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'unknown';

export interface DeviceInfo {
  type: DeviceType;
  name: string;
  isMobile: boolean;
  isDesktop: boolean;
  userAgent: string;
}

/**
 * Detect the current device type based on user agent
 */
export function detectDevice(): DeviceInfo {
  if (typeof window === 'undefined') {
    return {
      type: 'unknown',
      name: 'Unknown Device',
      isMobile: false,
      isDesktop: false,
      userAgent: ''
    };
  }

  const userAgent = window.navigator.userAgent.toLowerCase();
  
  // Mobile detection first
  if (/iphone|ipad|ipod/.test(userAgent)) {
    return {
      type: 'ios',
      name: 'iPhone / iPad',
      isMobile: true,
      isDesktop: false,
      userAgent
    };
  }

  if (/android/.test(userAgent)) {
    return {
      type: 'android',
      name: 'Android Device',
      isMobile: true,
      isDesktop: false,
      userAgent
    };
  }

  // Desktop detection
  if (/windows|win32|win64|wow32|wow64/.test(userAgent)) {
    return {
      type: 'windows',
      name: 'Windows PC',
      isMobile: false,
      isDesktop: true,
      userAgent
    };
  }

  if (/macintosh|mac os x/.test(userAgent)) {
    return {
      type: 'macos',
      name: 'Mac',
      isMobile: false,
      isDesktop: true,
      userAgent
    };
  }

  if (/linux/.test(userAgent) && !/android/.test(userAgent)) {
    return {
      type: 'linux',
      name: 'Linux PC',
      isMobile: false,
      isDesktop: true,
      userAgent
    };
  }

  return {
    type: 'unknown',
    name: 'Unknown Device',
    isMobile: false,
    isDesktop: false,
    userAgent
  };
}

/**
 * Get the primary download link for the detected device
 */
export function getPrimaryDownloadLink(downloadLinks: Record<string, string>): string | null {
  const device = detectDevice();
  
  switch (device.type) {
    case 'windows':
      return downloadLinks.windows || null;
    case 'macos':
      return downloadLinks.macos || null;
    case 'ios':
      return downloadLinks.ios || null;
    case 'android':
      return downloadLinks.android || null;
    default:
      return null;
  }
}

/**
 * Check if the detected device has an available download
 */
export function hasAvailableDownload(downloadLinks: Record<string, string>): boolean {
  const primaryLink = getPrimaryDownloadLink(downloadLinks);
  return Boolean(primaryLink && primaryLink.trim() !== '');
}

/**
 * Get recommended alternatives for devices without available downloads
 */
export function getAlternativeDownloads(downloadLinks: Record<string, string>): Array<{type: DeviceType, name: string, link: string}> {
  const alternatives: Array<{type: DeviceType, name: string, link: string}> = [];
  
  if (downloadLinks.windows) {
    alternatives.push({
      type: 'windows',
      name: 'Windows PC',
      link: downloadLinks.windows
    });
  }
  
  if (downloadLinks.macos) {
    alternatives.push({
      type: 'macos',
      name: 'Mac',
      link: downloadLinks.macos
    });
  }

  return alternatives;
}

/**
 * Whether to show the macOS 11 supportability disclaimer on the install page.
 *
 * Policy: conservative — default ON, hide only when we are confident the user
 * is on macOS 12 or later. This guarantees macOS 11 users always see the
 * warning, at the cost of occasional needless display when version detection
 * is blocked.
 *
 * Detection matrix:
 *   Chromium (Chrome/Edge/Arc):  navigator.userAgentData.getHighEntropyValues — reliable
 *   Firefox (Intel Mac):         userAgent exposes true "Mac OS X 11.0" — reliable
 *   Firefox (Apple Silicon):     userAgent capped at "10.15" — falls through (show)
 *   Safari:                      userAgent always "10_15_7" post-Big Sur — falls through (show)
 *   Non-macOS / SSR:             irrelevant — caller only invokes on macOS panel
 *
 * Any unknown / errored path returns true, matching the conservative policy.
 */
export async function shouldShowMacOS11Notice(): Promise<boolean> {
  if (typeof window === 'undefined') return true;

  const ua = window.navigator.userAgent;
  if (!/Mac OS X|Macintosh/.test(ua)) return true;

  const uaData = (window.navigator as Navigator & {
    userAgentData?: { getHighEntropyValues?: (hints: string[]) => Promise<{ platformVersion?: string }> };
  }).userAgentData;
  if (uaData?.getHighEntropyValues) {
    try {
      const hints = await uaData.getHighEntropyValues(['platformVersion']);
      const major = parseInt((hints.platformVersion ?? '').split('.')[0], 10);
      if (!isNaN(major) && major >= 12) return false;
    } catch {
      // Client Hints rejected or unsupported — fall through to show.
    }
  }

  if (/Firefox\//.test(ua)) {
    const match = ua.match(/Mac OS X (\d+)[._](\d+)/);
    if (match) {
      const major = parseInt(match[1], 10);
      if (!isNaN(major) && major >= 12) return false;
    }
  }

  return true;
}

/**
 * Trigger automatic download for supported browsers
 */
export function triggerDownload(url: string, filename?: string): boolean {
  try {
    if (typeof window === 'undefined') return false;
    
    // Create a temporary anchor element
    const link = document.createElement('a');
    link.href = url;
    link.style.display = 'none';
    
    if (filename) {
      link.download = filename;
    }
    
    // Append to body, click, and remove
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    
    return true;
  } catch (error) {
    console.error('Download trigger failed:', error);
    return false;
  }
}

/**
 * Trigger auto-download via hidden iframe (not blocked by popup blockers).
 * Use this for programmatic downloads outside user gesture context (e.g. on page load).
 * The iframe loads the URL silently — browsers treat binary file responses as downloads.
 */
export function triggerAutoDownload(url: string): void {
  if (typeof window === 'undefined') return;
  const iframe = document.createElement('iframe');
  iframe.style.display = 'none';
  iframe.src = url;
  document.body.appendChild(iframe);
  // Clean up after 60s (download should have started by then)
  setTimeout(() => {
    try { document.body.removeChild(iframe); } catch { /* already removed */ }
  }, 60000);
}

/**
 * Open download in new tab as fallback
 */
export function openDownloadInNewTab(url: string): void {
  if (typeof window !== 'undefined') {
    window.open(url, '_blank', 'noopener,noreferrer');
  }
}