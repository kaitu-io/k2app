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
      name: 'iOS Device',
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
 * Open download in new tab as fallback
 */
export function openDownloadInNewTab(url: string): void {
  if (typeof window !== 'undefined') {
    window.open(url, '_blank', 'noopener,noreferrer');
  }
}