import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { brandConfig } from '../../brands';
import { APP_UTM_SOURCE, appLinkCampaign, withAppAttribution } from '../site-link';

const site = new URL(brandConfig.baseURL);
const bare = site.hostname.replace(/^www\./, '');

describe('withAppAttribution', () => {
  const original = window._platform;
  beforeEach(() => {
    (window as any)._platform = { ...(original ?? {}), os: 'android' };
  });
  afterEach(() => {
    (window as any)._platform = original;
  });

  it('tags own-site links with source / os / campaign', () => {
    const out = new URL(withAppAttribution(`${brandConfig.baseURL}/install`, 'account'));
    expect(out.pathname).toBe('/install');
    expect(out.searchParams.get('utm_source')).toBe(APP_UTM_SOURCE);
    expect(out.searchParams.get('utm_medium')).toBe('android');
    expect(out.searchParams.get('utm_campaign')).toBe('account');
  });

  it('treats the bare domain and www as the same site, keeps query and hash', () => {
    const out = new URL(withAppAttribution(`https://${bare}/k2/?x=1#top`, 'tunnels'));
    expect(out.searchParams.get('utm_source')).toBe('app');
    expect(out.searchParams.get('x')).toBe('1');
    expect(out.hash).toBe('#top');
  });

  it('leaves third-party links untouched (payment pages, Stripe, Apple)', () => {
    for (const url of [
      'https://pay.arbella.group/pay/abc',
      'https://checkout.stripe.com/c/pay/cs_123',
      'https://apps.apple.com/account/subscriptions',
      `https://${bare}.evil.example/install`,
    ]) {
      expect(withAppAttribution(url, 'purchase')).toBe(url);
    }
  });

  it('does not override existing campaign tags, embedded pages, or API endpoints', () => {
    const tagged = `${brandConfig.baseURL}/purchase?utm_source=edm&utm_campaign=double11`;
    expect(withAppAttribution(tagged, 'home')).toBe(tagged);
    const embed = `${brandConfig.baseURL}/zh-CN/discovery?embed=true&theme=dark`;
    expect(withAppAttribution(embed, 'discover')).toBe(embed);
    const api = `${brandConfig.baseURL}/api/orders/u-1/pay?src=web`;
    expect(withAppAttribution(api, 'purchase')).toBe(api);
  });

  it('returns non-URLs unchanged', () => {
    expect(withAppAttribution('not a url', 'home')).toBe('not a url');
    expect(withAppAttribution('mailto:support@example.com', 'home')).toBe('mailto:support@example.com');
  });
});

describe('appLinkCampaign', () => {
  it('uses the first route segment', () => {
    expect(appLinkCampaign('/account')).toBe('account');
    expect(appLinkCampaign('/pro-histories/123')).toBe('pro-histories');
    expect(appLinkCampaign('/')).toBe('home');
    expect(appLinkCampaign('')).toBe('home');
    expect(appLinkCampaign('/Ab%20c')).toBe('ab20c');
  });
});
