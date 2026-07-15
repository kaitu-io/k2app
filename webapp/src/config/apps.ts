/**
 * Application Configuration
 *
 * This file defines the configuration for the Kaitu application including:
 * - Branding (colors, logos, app name)
 * - Features (enable/disable specific routes and functionality)
 * - API endpoints
 * - Bundle identifiers
 */

import { brandConfig, getBrandId } from '../brand';

export interface AppConfig {
  /** Unique app identifier */
  appId: string;

  /** Display name of the application */
  appName: string;

  /** Feature flags to enable/disable specific functionality */
  features: {
    /** Invite functionality */
    invite?: boolean;
    /** Retailer (分销商) UI surfaces — brand-gated */
    retailer?: boolean;
    /** Discovery/explore page */
    discover?: boolean;
    /** Delegate payer setup */
    delegate?: boolean;
    /** Pro history */
    proHistory?: boolean;
    /** Feedback page */
    feedback?: boolean;
    /** Device install guide */
    deviceInstall?: boolean;
    /** Android USB install via ADB */
    androidInstall?: boolean;
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
    /** App bypass (per-app VPN exclusion) */
    appBypass?: boolean;
    /** Private (dedicated) node management page */
    privateNode?: boolean;
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
 * App config = platform-static features + brand-divergent features.
 * Brand-divergent gates come from brandConfig.features (single source of
 * truth) — never fork on brand id inside components.
 */
const APP_CONFIG: AppConfig = {
  appId: 'io.kaitu.desktop',
  appName: brandConfig.productName,
  features: {
    // brand-divergent (from brand registry)
    invite: brandConfig.features.invite,
    retailer: brandConfig.features.retailer,
    discover: brandConfig.features.discover,
    delegate: brandConfig.features.delegate,
    chatwoot: brandConfig.features.chatwoot,
    privateNode: brandConfig.features.privateNode,
    // platform-static (same for both brands)
    proHistory: true,
    feedback: true,
    deviceInstall: true,
    androidInstall: true,
    updateLoginEmail: true,
    bridgeTest: true,
    proxyRule: {
      visible: true,
      defaultValue: 'chnroute',
    },
    appBypass: true,
  },
  branding: {
    primaryColor: brandConfig.theme.dark.primary.main,
    secondaryColor: brandConfig.theme.dark.secondary.main,
    logo: '/favicon.png',
    favicon: '/favicon.png',
  },
  // Entry URL is shared k2 infrastructure (brand-neutral), not brand surface.
  apiEndpoint: import.meta.env.VITE_KAITU_ENTRY_URL || 'https://k2.52j.me',
};

export const getCurrentAppConfig = (): AppConfig => APP_CONFIG;

export const getCurrentAppId = (): string => getBrandId();

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
