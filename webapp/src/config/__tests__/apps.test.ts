import { describe, it, expect } from 'vitest';
import { getCurrentAppConfig, isFeatureEnabled } from '../apps';
import { brandConfig } from '../../brands';

// Brand-adaptive: every assertion is expressed against brandConfig, so this
// suite is green under K2_BRAND=kaitu and K2_BRAND=overleap alike.
describe('getCurrentAppConfig derives from brandConfig', () => {
  it('brand-divergent features mirror brandConfig.features', () => {
    const cfg = getCurrentAppConfig();
    expect(cfg.features.invite).toBe(brandConfig.features.invite);
    expect(cfg.features.discover).toBe(brandConfig.features.discover);
    expect(cfg.features.delegate).toBe(brandConfig.features.delegate);
    expect(cfg.features.retailer).toBe(brandConfig.features.retailer);
    expect(cfg.features.chatwoot).toBe(brandConfig.features.chatwoot);
    expect(cfg.features.privateNode).toBe(brandConfig.features.privateNode);
    expect(cfg.features.selfHostedTunnels).toBe(brandConfig.features.selfHostedTunnels);
  });

  it('appName and branding come from the brand', () => {
    const cfg = getCurrentAppConfig();
    expect(cfg.appName).toBe(brandConfig.productName);
    expect(cfg.branding.primaryColor).toBe(brandConfig.theme.dark.primary.main);
  });

  it('platform-static features are unchanged', () => {
    const cfg = getCurrentAppConfig();
    expect(cfg.features.proHistory).toBe(true);
    expect(cfg.features.appBypass).toBe(true);
    expect(cfg.features.proxyRule).toEqual({ visible: true, defaultValue: 'chnroute' });
    expect(isFeatureEnabled('proxyRule')).toBe(true);
  });
});
