/**
 * Application Configuration
 *
 * This file defines the configuration for the Kaitu application including:
 * - Branding (colors, logos, app name)
 * - Features (enable/disable specific routes and functionality)
 * - API endpoints
 * - Bundle identifiers
 */

export interface AppConfig {
  /** Unique app identifier */
  appId: string;

  /** Display name of the application */
  appName: string;

  /** Feature flags to enable/disable specific functionality */
  features: {
    /** Invite functionality */
    invite?: boolean;
    /** Discovery/explore page */
    discover?: boolean;
    /** Member management */
    memberManagement?: boolean;
    /** Pro history */
    proHistory?: boolean;
    /** Feedback page */
    feedback?: boolean;
    /** Device install guide */
    deviceInstall?: boolean;
    /** Update login email */
    updateLoginEmail?: boolean;
    /** Bridge test page (development only) */
    bridgeTest?: boolean;
    /** Proxy rule configuration */
    proxyRule?: {
      /** Whether to show proxy rule selector */
      visible: boolean;
      /** Default proxy rule mode */
      defaultValue: 'global' | 'chnroute' | 'gfwlist';
    };
    /** Chatwoot chat widget */
    chatwoot?: boolean;
  };

  /** Branding configuration */
  branding: {
    /** Primary theme color (hex) */
    primaryColor: string;
    /** Secondary theme color (hex) */
    secondaryColor?: string;
    /** Logo asset path */
    logo: string;
    /** Favicon path */
    favicon?: string;
  };

  /** API endpoint configuration */
  apiEndpoint: string;
}

/**
 * Kaitu application configuration
 */
const KAITU_CONFIG: AppConfig = {
  appId: 'io.kaitu.desktop',
  appName: 'Kaitu',
  features: {
    invite: true,
    discover: true,
    memberManagement: true,
    proHistory: true,
    feedback: true,
    deviceInstall: true,
    updateLoginEmail: true,
    bridgeTest: true,
    proxyRule: {
      visible: true,              // Show proxy rule selector
      defaultValue: 'chnroute',   // Default to chnroute mode
    },
    chatwoot: true,               // Enable Chatwoot chat widget
  },
  branding: {
    primaryColor: '#1976d2',
    secondaryColor: '#dc004e',
    logo: '/assets/kaitu-logo.png',
    favicon: '/assets/favicon.ico',
  },
  apiEndpoint: 'https://k2.52j.me',
};

/**
 * Get the current application configuration
 * @returns Current app configuration (always Kaitu)
 */
export const getCurrentAppConfig = (): AppConfig => {
  return KAITU_CONFIG;
};

/**
 * Get current app ID
 * @returns Current app identifier (always 'kaitu')
 */
export const getCurrentAppId = (): string => {
  return 'kaitu';
};

/**
 * Check if a specific feature is enabled in the current app
 * @param feature Feature key to check
 * @returns Whether the feature is enabled
 */
export const isFeatureEnabled = (feature: keyof AppConfig['features']): boolean => {
  const config = getCurrentAppConfig();
  const featureValue = config.features[feature];

  // Handle special case for proxyRule which is an object
  if (feature === 'proxyRule' && typeof featureValue === 'object' && featureValue !== null) {
    return featureValue.visible;
  }

  return featureValue === true;
};
